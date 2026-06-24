// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"nimblegate/internal/gateway"
)

// makePubkey returns a valid ssh-ed25519 authorized_keys line with the given
// comment. Generated at test time so tests don't carry a private key on disk.
func makePubkey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh.NewPublicKey: %v", err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	return line
}

func TestSshKeys_listKeysOnMissingFile(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	keys, err := h.listKeys()
	if err != nil {
		t.Fatalf("listKeys on missing file: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(keys))
	}
}

func TestSshKeys_addKey_valid(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	line := makePubkey(t, "alice@laptop")
	k, err := h.addKey(line)
	if err != nil {
		t.Fatalf("addKey: %v", err)
	}
	if k.Type != "ssh-ed25519" {
		t.Errorf("Type: got %q want ssh-ed25519", k.Type)
	}
	if k.Comment != "alice@laptop" {
		t.Errorf("Comment: got %q want alice@laptop", k.Comment)
	}
	if !strings.HasPrefix(k.Fingerprint, "SHA256:") {
		t.Errorf("Fingerprint: want SHA256:… prefix, got %q", k.Fingerprint)
	}
	// File written with correct content + mode.
	data, err := os.ReadFile(h.keysPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), line) {
		t.Errorf("file missing the added line:\n%s", string(data))
	}
	st, err := os.Stat(h.keysPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("authorized_keys perm = %v, want 0600", st.Mode().Perm())
	}
}

// Default (single-tenant): a key is written as a plain HARDENED line -
// `restrict <key>`, NO forced command. The git user's login shell is git-shell,
// which routes the `~/` path and caps the session to git verbs; `restrict` adds
// no-pty/no-forwarding. (A forced command here would be rejected by git-shell.)
func TestSshKeys_addKey_unscopedWritesPlainRestrictLine(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{
		keysPath:   filepath.Join(dir, "authorized_keys"),
		exe:        "/usr/local/bin/nimblegate",
		policyRoot: "/srv/gateway/cfg",
		reposRoot:  "/srv/gateway/repos",
	}
	k, err := h.addKey(makePubkey(t, "alice@laptop"))
	if err != nil {
		t.Fatalf("addKey: %v", err)
	}
	if !strings.HasPrefix(k.Raw, "restrict ") {
		t.Errorf("unscoped key should be a plain restricted line: %q", k.Raw)
	}
	if strings.Contains(k.Raw, "command=") {
		t.Errorf("unscoped key must NOT carry a forced command (git-shell rejects it): %q", k.Raw)
	}
	if strings.Contains(k.Raw, "--scoped") {
		t.Errorf("unscoped key must NOT carry --scoped: %q", k.Raw)
	}
	if !strings.Contains(k.Raw, "ssh-") {
		t.Errorf("the key material must follow restrict: %q", k.Raw)
	}
}

// Pasted options are dropped - the gateway controls the restrictions (`restrict`),
// not the operator's pasted prefix. Critically a pasted `command="…"` must NOT
// survive (an operator can't inject an alternate forced command via the paste).
func TestSshKeys_addKey_overridesPastedOptions(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{
		keysPath:   filepath.Join(dir, "authorized_keys"),
		exe:        "/usr/local/bin/nimblegate",
		policyRoot: "/srv/gateway/cfg",
		reposRoot:  "/srv/gateway/repos",
	}
	line := `command="evil",no-pty ` + makePubkey(t, "ops@host")
	k, err := h.addKey(line)
	if err != nil {
		t.Fatalf("addKey: %v", err)
	}
	if !strings.HasPrefix(k.Raw, "restrict ") {
		t.Errorf("pasted options must be replaced by restrict: %q", k.Raw)
	}
	if strings.Contains(k.Raw, "evil") {
		t.Errorf("a pasted forced command must not survive: %q", k.Raw)
	}
}

// In scoped-access mode, a key is written with a forced command pinned to its
// fingerprint (so sshd routes it through `gateway shell`, which enforces the
// per-key ACL) plus restrict. This overrides any pasted options - scoping is
// mandatory.
func TestSshKeys_addKey_scopedWritesForcedCommand(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{
		keysPath:   filepath.Join(dir, "authorized_keys"),
		scoped:     true,
		exe:        "/usr/local/bin/nimblegate",
		policyRoot: "/srv/gateway/cfg",
		reposRoot:  "/srv/gateway/repos",
	}
	k, err := h.addKey(makePubkey(t, "alice@laptop"))
	if err != nil {
		t.Fatalf("addKey: %v", err)
	}
	if !strings.HasPrefix(k.Raw, `command="`) {
		t.Errorf("scoped key should start with a forced command: %q", k.Raw)
	}
	if !strings.Contains(k.Raw, "gateway shell --key "+k.Fingerprint) {
		t.Errorf("forced command must pin the key's fingerprint: %q", k.Raw)
	}
	if !strings.Contains(k.Raw, "--policy-root /srv/gateway/cfg") || !strings.Contains(k.Raw, "--repos-root /srv/gateway/repos") {
		t.Errorf("forced command must carry the roots: %q", k.Raw)
	}
	if !strings.Contains(k.Raw, ",restrict ") {
		t.Errorf("scoped key must also be restricted: %q", k.Raw)
	}
	if !strings.Contains(k.Raw, "--scoped") {
		t.Errorf("scoped key's forced command must carry --scoped (ACL enforcement): %q", k.Raw)
	}
	b, _ := os.ReadFile(h.keysPath)
	if !strings.Contains(string(b), "gateway shell --key SHA256:") {
		t.Errorf("authorized_keys missing the forced command:\n%s", b)
	}
}

func TestSshKeys_addKey_rejectsInvalid(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	cases := []struct {
		name, input string
	}{
		{"empty", ""},
		{"whitespace", "   \n\t  "},
		{"garbage", "this is not a pubkey"},
		{"missing-base64", "ssh-ed25519"},
		{"bad-base64", "ssh-ed25519 !!!notbase64!!! alice@host"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := h.addKey(c.input); err == nil {
				t.Fatalf("addKey(%q): expected error, got nil", c.input)
			}
		})
	}
	if _, err := os.Stat(h.keysPath); err == nil {
		t.Errorf("file should not exist after only-invalid attempts")
	}
}

func TestSshKeys_addKey_rejectsDuplicate(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	line := makePubkey(t, "first")
	if _, err := h.addKey(line); err != nil {
		t.Fatalf("first addKey: %v", err)
	}
	_, err := h.addKey(line)
	if err == nil {
		t.Fatalf("second addKey of same key: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already authorized") {
		t.Errorf("error message should name the duplicate; got %q", err.Error())
	}
}

func TestSshKeys_listKeys_roundTrip(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	lines := []string{
		makePubkey(t, "alice"),
		makePubkey(t, "bob"),
		makePubkey(t, ""), // no comment
	}
	for _, l := range lines {
		if _, err := h.addKey(l); err != nil {
			t.Fatalf("addKey: %v", err)
		}
	}
	keys, err := h.listKeys()
	if err != nil {
		t.Fatalf("listKeys: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("listKeys count = %d, want 3", len(keys))
	}
	if keys[0].Comment != "alice" || keys[1].Comment != "bob" || keys[2].Comment != "" {
		t.Errorf("comments not preserved: %v / %v / %v", keys[0].Comment, keys[1].Comment, keys[2].Comment)
	}
}

func TestSshKeys_removeKey_byFingerprint(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	line1 := makePubkey(t, "keep-me")
	line2 := makePubkey(t, "remove-me")
	k1, _ := h.addKey(line1)
	k2, _ := h.addKey(line2)

	removed, err := h.removeKey(k2.Fingerprint)
	if err != nil {
		t.Fatalf("removeKey: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}
	keys, _ := h.listKeys()
	if len(keys) != 1 {
		t.Fatalf("after remove: %d keys, want 1", len(keys))
	}
	if keys[0].Fingerprint != k1.Fingerprint {
		t.Errorf("wrong key remained: %q want %q", keys[0].Fingerprint, k1.Fingerprint)
	}
}

func TestSshKeys_removeKey_notFound(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys")}
	_, _ = h.addKey(makePubkey(t, "alice"))
	removed, err := h.removeKey("SHA256:nonexistent")
	if err != nil {
		t.Fatalf("removeKey: %v", err)
	}
	if removed {
		t.Fatal("expected removed=false for nonexistent fingerprint")
	}
	keys, _ := h.listKeys()
	if len(keys) != 1 {
		t.Errorf("count changed; key list disturbed by failed remove")
	}
}

func TestSshKeys_HTTP_list_emptyState(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys"), token: "tok"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ssh-keys", nil)
	h.list(rec, req, true, dir)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, body)
	}
	if !strings.Contains(body, "No SSH keys authorized yet") {
		t.Errorf("empty-state message missing from body")
	}
	if !strings.Contains(body, "Paste a public key") {
		t.Errorf("add form should render when allowEdits=true")
	}
	// forms must carry the htmx CSRF header - a plain <form action=...> POST
	// would 403 in a real browser because csrfOK reads from X-CSRF-Token only.
	if !strings.Contains(body, `hx-post="/ssh-keys/add"`) {
		t.Errorf("add form must use hx-post for CSRF header injection")
	}
	if !strings.Contains(body, `hx-headers='{"X-CSRF-Token":"tok"}'`) {
		t.Errorf("add form must inject X-CSRF-Token via hx-headers")
	}
}

func TestSshKeys_HTTP_list_readOnlyHidesForm(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys"), token: ""}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ssh-keys", nil)
	h.list(rec, req, false, dir)

	body := rec.Body.String()
	if strings.Contains(body, `name="pubkey"`) {
		t.Errorf("add form leaked into read-only render")
	}
	if !strings.Contains(body, "--allow-edits") {
		t.Errorf("read-only mode should hint at --allow-edits to enable management")
	}
}

func TestSshKeys_HTTP_add_rejectsMissingCSRF(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys"), token: "secret"}

	form := url.Values{}
	form.Set("pubkey", makePubkey(t, "alice"))
	req := httptest.NewRequest(http.MethodPost, "/ssh-keys/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// no X-CSRF-Token header
	rec := httptest.NewRecorder()
	h.add(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	// And no file was created.
	if _, err := os.Stat(h.keysPath); err == nil {
		t.Errorf("authorized_keys created despite CSRF failure")
	}
}

func TestSshKeys_HTTP_add_invalidShowsInlineError(t *testing.T) {
	dir := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(dir, "authorized_keys"), token: "secret"}
	form := url.Values{}
	form.Set("pubkey", "not a key")
	req := httptest.NewRequest(http.MethodPost, "/ssh-keys/add", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "secret")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.add(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 (redirect with err)", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/ssh-keys?err=") {
		t.Errorf("Location = %q, want /ssh-keys?err=…", loc)
	}
	if !strings.Contains(loc, "not%20a%20valid%20SSH%20public%20key") && !strings.Contains(loc, "not a valid SSH public key") {
		t.Errorf("Location should carry the validation error: %q", loc)
	}
}

// grant/revoke handlers write/remove the per-repo ACL and enforce CSRF.
func TestSshKeys_grantThenRevokeHandlers(t *testing.T) {
	policyRoot := t.TempDir()
	h := &sshKeyHandlers{keysPath: filepath.Join(t.TempDir(), "authorized_keys"), token: "tok", scoped: true, policyRoot: policyRoot}
	acl := gateway.AccessStore{PolicyRoot: policyRoot}

	post := func(handler func(http.ResponseWriter, *http.Request), path string, form url.Values, csrf bool) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if csrf {
			req.Header.Set("X-CSRF-Token", "tok")
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec.Code
	}

	// CSRF enforced
	if code := post(h.grant, "/ssh-keys/grant", url.Values{"repo": {"demo"}, "fingerprint": {"SHA256:abc"}}, false); code != http.StatusForbidden {
		t.Fatalf("grant without CSRF should be 403, got %d", code)
	}
	// grant writes the ACL
	post(h.grant, "/ssh-keys/grant", url.Values{"repo": {"demo"}, "fingerprint": {"SHA256:abc"}, "access": {"write"}}, true)
	if ok, _ := acl.Allows("demo", "SHA256:abc", true); !ok {
		t.Error("grant handler should have written a write grant")
	}
	// revoke removes it
	post(h.revoke, "/ssh-keys/revoke", url.Values{"repo": {"demo"}, "fingerprint": {"SHA256:abc"}}, true)
	if ok, _ := acl.Allows("demo", "SHA256:abc", false); ok {
		t.Error("revoke handler should have removed the grant")
	}
}

// The scoped page renders the access section + grant form (hx-post for CSRF).
func TestSshKeys_scopedPageShowsGrantUI(t *testing.T) {
	policyRoot := t.TempDir()
	if err := (gateway.FilePolicyStore{Root: policyRoot}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "file:///u"}); err != nil {
		t.Fatal(err)
	}
	h := &sshKeyHandlers{keysPath: filepath.Join(t.TempDir(), "authorized_keys"), token: "tok", scoped: true, policyRoot: policyRoot}
	rec := httptest.NewRecorder()
	h.list(rec, httptest.NewRequest(http.MethodGet, "/ssh-keys", nil), true, policyRoot)
	body := rec.Body.String()
	if !strings.Contains(body, "Repo access (scoped)") {
		t.Error("scoped page should show the access section")
	}
	if !strings.Contains(body, `hx-post="/ssh-keys/grant"`) {
		t.Error("scoped page should have the grant form via hx-post")
	}
}
