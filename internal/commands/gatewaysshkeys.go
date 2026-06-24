// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"

	"golang.org/x/crypto/ssh"
)

// sshKey is a parsed SSH public key ready for the dashboard.
type sshKey struct {
	Type        string // "ssh-ed25519", "ssh-rsa", etc.
	Fingerprint string // SHA256:… form
	Comment     string // human-friendly trailing label (or "")
	Raw         string // original authorized_keys line (preserves options)
}

// sshKeyHandlers serves the /ssh-keys page + its write endpoints.
//
// Concurrency: a single mutex serializes all writes so concurrent add/delete
// don't tear the authorized_keys file. Reads are fine without locking because
// os.ReadFile() is atomic at the syscall layer for small files.
type sshKeyHandlers struct {
	keysPath string // path to the authorized_keys file (volume-mounted)
	token    string // CSRF token (empty when --allow-edits is off)
	mu       sync.Mutex

	// Scoped access: when scoped is true, each added key is written with a
	// forced command (`command="<exe> gateway shell --key <fp> …",restrict`) so
	// sshd routes it through the gateway shell, which enforces the per-key ACL.
	// exe/policyRoot/reposRoot parameterize that forced command.
	scoped     bool
	exe        string
	policyRoot string
	reposRoot  string
}

// forcedCommandLine builds the scoped-access authorized_keys line: a forced
// command pinned to the key's fingerprint, plus restrict, in front of the
// canonical key. sshd runs ONLY this command and passes the client's git
// command in $SSH_ORIGINAL_COMMAND; gateway shell then enforces the ACL.
func forcedCommandLine(exe, policyRoot, reposRoot, fingerprint, canonicalKey string, scoped bool) string {
	cmd := fmt.Sprintf("%s gateway shell --key %s --policy-root %s --repos-root %s",
		exe, fingerprint, policyRoot, reposRoot)
	if scoped {
		// Only scoped mode enforces the per-key ACL; without --scoped the shell
		// routes the request (so the clean ssh:// URL resolves) but allows any
		// authorized key - the single-tenant default.
		cmd += " --scoped"
	}
	return fmt.Sprintf("command=%q,restrict %s", cmd, canonicalKey)
}

// listKeys parses every line of authorized_keys. Malformed lines are silently
// skipped (the file may have been hand-edited; we never refuse to render).
func (h *sshKeyHandlers) listKeys() ([]sshKey, error) {
	data, err := os.ReadFile(h.keysPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var keys []sshKey
	rest := data
	for len(bytes.TrimSpace(rest)) > 0 {
		pk, comment, _, rem, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			// Skip this line, advance to next newline. Hand-edited files may
			// have malformed entries we don't want to crash on.
			if idx := bytes.IndexByte(rest, '\n'); idx >= 0 {
				rest = rest[idx+1:]
				continue
			}
			break
		}
		// The parsed "rem" tells us how much of "rest" was consumed; the rest
		// minus rem is the original line (with trailing newline).
		consumed := len(rest) - len(rem)
		raw := strings.TrimRight(string(rest[:consumed]), "\r\n")
		keys = append(keys, sshKey{
			Type:        pk.Type(),
			Fingerprint: ssh.FingerprintSHA256(pk),
			Comment:     comment,
			Raw:         raw,
		})
		rest = rem
	}
	return keys, nil
}

// addKey validates and appends a new pubkey line. Returns the parsed sshKey on
// success or an error suitable for display to the user (NOT a 500).
func (h *sshKeyHandlers) addKey(line string) (sshKey, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	line = strings.TrimSpace(line)
	if line == "" {
		return sshKey{}, errors.New("paste a public key line (e.g. starting with ssh-ed25519 …)")
	}
	pk, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return sshKey{}, fmt.Errorf("not a valid SSH public key: %v", err)
	}
	fp := ssh.FingerprintSHA256(pk)

	// We control the line's restrictions; any pasted options are dropped.
	_ = options
	canonical := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pk)))
	if comment != "" {
		canonical += " " + comment
	}
	if h.scoped {
		// Scoped (multi-tenant) mode: a forced command routes every request through
		// `gateway shell`, which enforces the per-key ACL. This REQUIRES the git
		// user's login shell to be a normal shell (e.g. /bin/sh) so sshd can run the
		// forced command - git-shell rejects it. So scoped mode is an advanced
		// non-container deployment, not the default.
		line = forcedCommandLine(h.exe, h.policyRoot, h.reposRoot, fp, canonical, h.scoped)
	} else {
		// Default (single-tenant): a plain key hardened with `restrict` - the
		// canonical OpenSSH shorthand for no-port-forwarding + no-agent-forwarding +
		// no-X11-forwarding + no-pty + no-user-rc (and future restrictions). The git
		// user's login shell is git-shell, which already caps the session to git
		// transfer verbs and routes the `~/<repo>.git` path; `restrict` closes the
		// SSH-channel vectors git-shell doesn't (forwarding/pty). The clean
		// `ssh://host/repo.git` URL is intentionally NOT supported - push via the
		// `ssh://host:2222/~/<repo>.git` path form (see README / getting-started).
		line = "restrict " + canonical
	}

	// Duplicate check: keys are unique by fingerprint, not by comment.
	existing, _ := h.listKeys()
	for _, k := range existing {
		if k.Fingerprint == fp {
			return sshKey{}, fmt.Errorf("key already authorized (%s)", fp)
		}
	}

	if err := h.ensureFile(); err != nil {
		return sshKey{}, err
	}
	f, err := os.OpenFile(h.keysPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return sshKey{}, err
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		return sshKey{}, err
	}
	return sshKey{Type: pk.Type(), Fingerprint: fp, Comment: comment, Raw: line}, nil
}

// removeKey rewrites authorized_keys without the line matching the given
// fingerprint. Returns true if a key was removed.
func (h *sshKeyHandlers) removeKey(fingerprint string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return false, errors.New("fingerprint required")
	}
	data, err := os.ReadFile(h.keysPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	var out bytes.Buffer
	removed := false
	rest := data
	for len(rest) > 0 {
		idx := bytes.IndexByte(rest, '\n')
		var line []byte
		if idx >= 0 {
			line = rest[:idx+1]
			rest = rest[idx+1:]
		} else {
			line = rest
			rest = nil
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			out.Write(line)
			continue
		}
		pk, _, _, _, err := ssh.ParseAuthorizedKey(trimmed)
		if err != nil {
			// Preserve malformed lines verbatim.
			out.Write(line)
			continue
		}
		if ssh.FingerprintSHA256(pk) == fingerprint {
			removed = true
			continue // drop this line
		}
		out.Write(line)
	}
	if !removed {
		return false, nil
	}
	// Atomic replace: write to a tmp file then rename.
	dir := filepath.Dir(h.keysPath)
	tmp, err := os.CreateTemp(dir, ".authorized_keys.*")
	if err != nil {
		return false, err
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return false, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return false, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return false, err
	}
	if err := os.Rename(tmp.Name(), h.keysPath); err != nil {
		_ = os.Remove(tmp.Name())
		return false, err
	}
	return true, nil
}

// ensureFile creates the authorized_keys file (mode 0600) and its parent dir
// (mode 0755) if they don't already exist.
func (h *sshKeyHandlers) ensureFile() error {
	if _, err := os.Stat(h.keysPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.keysPath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(h.keysPath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// --- HTTP handlers ---

// list is GET /ssh-keys. Available regardless of --allow-edits; when edits are
// off, the page renders the key list without the add form / delete buttons.
func (h *sshKeyHandlers) list(w http.ResponseWriter, r *http.Request, allowEdits bool, policyRoot string) {
	if r.URL.Path != "/ssh-keys" {
		http.NotFound(w, r)
		return
	}
	keys, err := h.listKeys()
	if err != nil {
		http.Error(w, "could not read authorized_keys: "+err.Error(), http.StatusInternalServerError)
		return
	}
	errMsg := strings.TrimSpace(r.URL.Query().Get("err"))
	data := sshKeysPageData{
		Keys:       keys,
		AllowEdits: allowEdits,
		Scoped:     h.scoped,
		CSRFToken:  h.token,
		Error:      errMsg,
		Chrome:     buildChrome("ssh-keys", "", policyRoot),
	}
	if h.scoped {
		data.Repos = listGatewayRepos(h.policyRoot)
		acl := gateway.AccessStore{PolicyRoot: h.policyRoot}
		for _, repo := range data.Repos {
			al, _ := acl.Load(repo)
			for _, g := range al.Grants {
				data.Grants = append(data.Grants, grantRow{KeyFP: g.Fingerprint, KeyComment: g.Comment, Repo: repo, Access: g.Access})
			}
		}
	}
	renderSshKeysPage(w, data)
}

// add is POST /ssh-keys/add. On parse/validation error, redirects back to
// /ssh-keys?err=<msg> so the page re-renders with the error inline (NOT a 500).
func (h *sshKeyHandlers) add(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	line := r.FormValue("pubkey")
	if _, err := h.addKey(line); err != nil {
		// Surface validation error to the user by round-tripping through the
		// list page's `err` query string; the renderer shows it inline.
		http.Redirect(w, r, "/ssh-keys?err="+escapeQuery(err.Error()), http.StatusSeeOther)
		return
	}
	_ = gateway.AppendEvent(gatewayCfgRootFromKeysPath(h.keysPath), gateway.Event{
		Event: "ssh-key-add",
		OK:    true,
	})
	redirectAfterAction(w, r, "/ssh-keys")
}

// delete is POST /ssh-keys/delete. fingerprint=SHA256:… in the form body.
func (h *sshKeyHandlers) delete(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	fp := r.FormValue("fingerprint")
	if _, err := h.removeKey(fp); err != nil {
		http.Redirect(w, r, "/ssh-keys?err="+escapeQuery(err.Error()), http.StatusSeeOther)
		return
	}
	_ = gateway.AppendEvent(gatewayCfgRootFromKeysPath(h.keysPath), gateway.Event{
		Event: "ssh-key-remove",
		OK:    true,
	})
	redirectAfterAction(w, r, "/ssh-keys")
}

// grant is POST /ssh-keys/grant - authorize a key (fingerprint) on a repo at
// read/write. Scoped-access only; writes the per-repo ACL.
func (h *sshKeyHandlers) grant(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo, fp, access := r.FormValue("repo"), r.FormValue("fingerprint"), r.FormValue("access")
	if access == "" {
		access = "write"
	}
	if repo == "" || fp == "" {
		http.Redirect(w, r, "/ssh-keys?err="+escapeQuery("grant needs a repo and a key"), http.StatusSeeOther)
		return
	}
	if err := (gateway.AccessStore{PolicyRoot: h.policyRoot}).Grant(repo, fp, access, ""); err != nil {
		http.Redirect(w, r, "/ssh-keys?err="+escapeQuery(err.Error()), http.StatusSeeOther)
		return
	}
	redirectAfterAction(w, r, "/ssh-keys")
}

// revoke is POST /ssh-keys/revoke - remove a key's grant on a repo.
func (h *sshKeyHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo, fp := r.FormValue("repo"), r.FormValue("fingerprint")
	if err := (gateway.AccessStore{PolicyRoot: h.policyRoot}).Revoke(repo, fp); err != nil {
		http.Redirect(w, r, "/ssh-keys?err="+escapeQuery(err.Error()), http.StatusSeeOther)
		return
	}
	redirectAfterAction(w, r, "/ssh-keys")
}

// escapeQuery is a small wrapper that hides the url.QueryEscape import while
// keeping the redirect URLs human-readable in logs.
func escapeQuery(s string) string {
	// Replace just the characters that would break the query string. The full
	// url.QueryEscape would over-encode; this keeps error messages readable.
	r := strings.NewReplacer(
		"&", "%26",
		"#", "%23",
		"?", "%3F",
		"\n", " ",
	)
	return r.Replace(s)
}

// gatewayCfgRootFromKeysPath is best-effort: the event log lives at the gateway
// policy root. When keys live at /srv/gateway/ssh/authorized_keys the policy
// root is typically /srv/gateway/cfg. Resolve sibling cfg/ dir; on container
// deploys this is correct, on others the event simply gets dropped (the helper
// returns "" and AppendEvent treats empty root as a no-op).
func gatewayCfgRootFromKeysPath(keysPath string) string {
	// /srv/gateway/ssh/authorized_keys → /srv/gateway/cfg
	ssh := filepath.Dir(keysPath) // /srv/gateway/ssh
	parent := filepath.Dir(ssh)   // /srv/gateway
	cfg := filepath.Join(parent, "cfg")
	if st, err := os.Stat(cfg); err == nil && st.IsDir() {
		return cfg
	}
	return ""
}

// --- Rendering ---

// grantRow is one (key → repo) authorization, flattened for the page.
type grantRow struct {
	KeyFP      string
	KeyComment string
	Repo       string
	Access     string
}

type sshKeysPageData struct {
	Keys       []sshKey
	AllowEdits bool
	Scoped     bool       // scoped-access on → show the grant/revoke UI
	Repos      []string   // registered repos, for the grant picker
	Grants     []grantRow // current (key → repo) grants
	CSRFToken  string
	Error      string
	Chrome     chromeData
}

func renderSshKeysPage(w http.ResponseWriter, d sshKeysPageData) {
	var body strings.Builder
	if err := sshKeysContentTmpl.ExecuteTemplate(&body, "content", d); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{
		Title:     "ssh keys : gateway",
		CSRFToken: d.CSRFToken,
		Chrome:    d.Chrome,
		Content:   template.HTML(body.String()),
	})
}

var sshKeysContentTmpl = func() *template.Template {
	t := template.New("sshkeys").Funcs(template.FuncMap{"icon": gwicons.HTML})
	template.Must(t.New("content").Parse(`
<div class="gw-sshkeys">
  <h2 class="gw-pagehead">SSH keys</h2>
  <p class="gw-pagedesc">
    Keys authorized to push to the gateway via <code>git@&lt;host&gt;:&lt;repo&gt;.git</code>. Stored on the persistent ssh volume; sshd reads this file directly so revocation is instant.
  </p>

  {{if .Error}}<div class="gw-sshkey-error" style="margin:0 0 14px;padding:9px 12px;background:var(--gw-error-bg);border:1px solid var(--gw-block-border);color:var(--gw-error-text);border-radius:6px;font-size:13px">{{icon "warn"}} {{.Error}}</div>{{end}}

  {{if .Keys}}
  <table class="fr" style="margin:0 0 18px;width:100%;border-collapse:collapse">
    <thead><tr style="border-bottom:1px solid var(--gw-border);color:var(--gw-text-muted);text-align:left;font-size:12px">
      <th style="padding:6px 8px">Type</th>
      <th style="padding:6px 8px">Fingerprint (SHA256)</th>
      <th style="padding:6px 8px">Comment</th>
      {{if .AllowEdits}}<th style="padding:6px 8px;width:80px"></th>{{end}}
    </tr></thead>
    <tbody>
    {{range .Keys}}
      <tr style="border-bottom:1px solid var(--gw-border-subtle)">
        <td style="padding:8px;font-family:ui-monospace,monospace;font-size:12px;color:var(--gw-text-soft)">{{.Type}}</td>
        <td style="padding:8px;font-family:ui-monospace,monospace;font-size:12px;color:var(--gw-text-soft);word-break:break-all">{{.Fingerprint}}</td>
        <td style="padding:8px;font-size:13px;color:var(--gw-text-soft)">{{if .Comment}}{{.Comment}}{{else}}<span class="sub" style="color:var(--gw-text-fainter)">-</span>{{end}}</td>
        {{if $.AllowEdits}}
        <td style="padding:8px">
          <form hx-post="/ssh-keys/delete" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded" hx-confirm="Remove this key? Devs using it will lose push access." style="display:inline">
            <input type="hidden" name="fingerprint" value="{{.Fingerprint}}">
            <button type="submit" style="background:none;border:1px solid var(--gw-danger-border);color:var(--gw-danger-text);padding:4px 10px;border-radius:4px;cursor:pointer;font:inherit;font-size:12px">Remove</button>
          </form>
        </td>
        {{end}}
      </tr>
    {{end}}
    </tbody>
  </table>
  {{else}}
  <div style="margin:0 0 18px;padding:18px;background:var(--gw-bg-panel);border:1px dashed var(--gw-border);color:var(--gw-text-muted);border-radius:6px;font-size:13px">
    No SSH keys authorized yet. {{if .AllowEdits}}Paste a public key below to start accepting git pushes.{{else}}Restart the dashboard with <code>--allow-edits</code> to add keys here.{{end}}
  </div>
  {{end}}

  {{if .AllowEdits}}
  <details class="frame gw-add-key" open style="margin:0 0 18px;padding:14px;background:var(--gw-bg-panel);border:1px solid var(--gw-border);border-radius:6px">
    <summary style="cursor:pointer;color:var(--gw-text);font-weight:600">+ Add a public key</summary>
    <form hx-post="/ssh-keys/add" hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded" hx-target="body" hx-swap="outerHTML" style="margin-top:12px">
      <label style="display:block;color:var(--gw-text-soft);font-size:13px;margin-bottom:6px">
        Paste a single SSH public key line (e.g. <code>ssh-ed25519 AAAA… name@host</code>):
      </label>
      <textarea name="pubkey" rows="3" required style="width:100%;background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:4px;padding:8px;font:inherit;font-family:ui-monospace,monospace;font-size:12px;box-sizing:border-box" placeholder="ssh-ed25519 AAAAC3Nz… alice@laptop"></textarea>
      <div style="margin-top:10px"><button type="submit" style="background:var(--gw-ok-bg);color:var(--gw-text);border:1px solid var(--gw-ok-border);padding:7px 14px;border-radius:4px;cursor:pointer;font:inherit">Authorize key</button></div>
      <p class="sub" style="color:var(--gw-text-fainter);font-size:12px;margin:8px 0 0">The key is stored in plain text on the ssh volume; sshd reads it on every connection attempt.</p>
      <details style="margin-top:14px;font-size:12px;color:var(--gw-text-muted);border-top:1px dashed var(--gw-border);padding-top:10px"><summary style="cursor:pointer;color:var(--gw-text-soft)">Don&#39;t have an SSH key yet? Run these on your machine.</summary><pre style="background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:4px;padding:10px;margin:8px 0 4px;font-size:12px;line-height:1.45;overflow-x:auto"><code>ssh-keygen -t ed25519 -C &quot;you@example.com&quot;
cat ~/.ssh/id_ed25519.pub</code></pre><p style="margin:6px 0 0">Press Enter at each prompt to accept the defaults; pick a passphrase or leave blank. Paste the <code>cat</code> output above. The private half (<code>~/.ssh/id_ed25519</code>, no <code>.pub</code>) stays on your machine. Never paste it anywhere.</p></details>
    </form>
  </details>
  {{end}}

  {{if .Scoped}}
  <div class="gw-scoped-access" style="margin:18px 0 0;padding:14px;background:var(--gw-bg-panel);border:1px solid var(--gw-border);border-radius:6px">
    <h3 style="margin:0 0 6px;color:var(--gw-text)">Repo access (scoped)</h3>
    <p class="sub" style="color:var(--gw-text-muted);font-size:13px;margin:0 0 12px">Scoped access is on. Each key may reach only the repos granted here. A key with no grant can reach nothing.</p>
    {{if .Grants}}
    <table class="fr" style="width:100%;border-collapse:collapse;margin:0 0 14px">
      <thead><tr style="border-bottom:1px solid var(--gw-border);color:var(--gw-text-muted);text-align:left;font-size:12px">
        <th style="padding:6px 8px">Repo</th><th style="padding:6px 8px">Key</th><th style="padding:6px 8px">Access</th>{{if .AllowEdits}}<th style="padding:6px 8px;width:80px"></th>{{end}}
      </tr></thead>
      <tbody>
      {{range .Grants}}
        <tr style="border-bottom:1px solid var(--gw-border-subtle)">
          <td style="padding:8px;font-size:13px;color:var(--gw-text-soft)">{{.Repo}}</td>
          <td style="padding:8px;font-family:ui-monospace,monospace;font-size:12px;color:var(--gw-text-soft);word-break:break-all">{{if .KeyComment}}{{.KeyComment}} {{end}}{{.KeyFP}}</td>
          <td style="padding:8px;font-size:13px;color:var(--gw-text-soft)">{{.Access}}</td>
          {{if $.AllowEdits}}<td style="padding:8px">
            <form hx-post="/ssh-keys/revoke" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded" style="display:inline">
              <input type="hidden" name="repo" value="{{.Repo}}"><input type="hidden" name="fingerprint" value="{{.KeyFP}}">
              <button type="submit" style="background:none;border:1px solid var(--gw-danger-border);color:var(--gw-danger-text);padding:4px 10px;border-radius:4px;cursor:pointer;font:inherit;font-size:12px">Revoke</button>
            </form>
          </td>{{end}}
        </tr>
      {{end}}
      </tbody>
    </table>
    {{else}}<p class="sub" style="color:var(--gw-text-fainter);font-size:13px;margin:0 0 12px">No grants yet. No key can reach any repo. Grant access below.</p>{{end}}
    {{if .AllowEdits}}
    <form hx-post="/ssh-keys/grant" hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded" style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end">
      <label style="font-size:12px;color:var(--gw-text-soft)">Key<br><select name="fingerprint" required style="background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:4px;padding:6px;font:inherit;font-size:12px">{{range .Keys}}<option value="{{.Fingerprint}}">{{if .Comment}}{{.Comment}} - {{end}}{{.Fingerprint}}</option>{{end}}</select></label>
      <label style="font-size:12px;color:var(--gw-text-soft)">Repo<br><select name="repo" required style="background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:4px;padding:6px;font:inherit;font-size:12px">{{range .Repos}}<option value="{{.}}">{{.}}</option>{{end}}</select></label>
      <label style="font-size:12px;color:var(--gw-text-soft)">Access<br><select name="access" style="background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:4px;padding:6px;font:inherit;font-size:12px"><option value="write">write (push+fetch)</option><option value="read">read (fetch)</option></select></label>
      <button type="submit" style="background:var(--gw-ok-bg);color:var(--gw-text);border:1px solid var(--gw-ok-border);padding:7px 14px;border-radius:4px;cursor:pointer;font:inherit">Grant</button>
    </form>
    {{end}}
  </div>
  {{end}}
</div>
`))
	return t
}()
