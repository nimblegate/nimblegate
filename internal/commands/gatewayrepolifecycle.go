// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/kits"
)

// repoLifecycleHandlers owns the /policy/repo/{add,archive,restore,scan-*}
// routes - the gateway-side repo lifecycle. Each handler delegates the
// state-change to internal/gateway and records exactly one structured event in
// _events.jsonl. All routes require POST + CSRF + --allow-edits (wired in
// gatewaydashboard.go under `if *allowEdits`). policyRoot and reposRoot point
// at the gateway's per-repo config root and bare-repos root respectively;
// selfExe is the absolute path baked into post-receive hooks and used to shell
// out for rescans.
type repoLifecycleHandlers struct {
	policyRoot string
	reposRoot  string
	selfExe    string
	token      string
}

// normalizeProtectedRefs maps operator-friendly inputs to the full ref names
// that gateway.isGatedRef matches against. A bare token (no `/`) is treated
// as a branch name and prefixed with `refs/heads/`; anything starting with
// `refs/` is passed through unchanged (so operators can still write
// `refs/tags/v*` for tag patterns). Empty / whitespace tokens are dropped.
//
// Background: the form pre-fills `refs/heads/main` so most operators won't
// hit this path. But if someone types `main` (or copies a branch list from
// `git branch`), the gate would have silently never fired. This is the
// "every dashboard-registered repo ships ungated" foot-gun T5 surfaced.
func normalizeProtectedRefs(in []string) []string {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "refs/") {
			out = append(out, t)
			continue
		}
		out = append(out, "refs/heads/"+t)
	}
	return out
}

// redirectAfterAction sends the operator to `target` after a successful
// mutation. htmx submits (HX-Request: true) get an HX-Redirect header which
// triggers a real browser navigation - without this, htmx with no hx-target
// follows the 303 and swaps the destination page's HTML INTO the submitting
// form's container, inlining the whole site into a tiny corner of the form.
// Plain HTTP clients (curl, manual testing, JS-off) ignore the header and
// follow the standard 303 Location.
func redirectAfterAction(w http.ResponseWriter, r *http.Request, target string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// add registers a new repo: AddRepo creates lib + activation symlinks, then
// we write the starter [frames] enabled list, then log the event. Redirects
// to /policy?repo=<name> so the operator lands on the new repo's policy page.
func (h repoLifecycleHandlers) add(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) || name == "_repos" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	upstream := r.FormValue("upstream")
	// C: this gateway relays over HTTPS; with no ssh client it cannot relay to an
	// ssh:// upstream at all (it would fail silently after each push). Fail fast.
	if gateway.IsSSHUpstream(upstream) {
		if _, err := exec.LookPath("ssh"); err != nil {
			http.Error(w, "This gateway can't relay to an ssh:// upstream - no SSH client is installed. Use an https:// URL with a Personal Access Token.", http.StatusBadRequest)
			return
		}
	}
	// B: refuse a second repo pointing at an already-registered upstream - almost
	// always a mistake (two gateway repos relaying to the same real remote).
	if upstream != "" {
		for _, existing := range listGatewayRepos(h.policyRoot) {
			if p, err := (gateway.FilePolicyStore{Root: h.policyRoot}).Load(existing); err == nil && p.UpstreamURL == upstream {
				http.Error(w, fmt.Sprintf("That upstream is already registered as %q - edit or remove that one instead of adding a second repo for the same remote.", existing), http.StatusConflict)
				return
			}
		}
	}
	kitName := r.FormValue("kit")
	if kitName == "" {
		kitName = "core"
	}
	securityStrict := r.FormValue("security_strict") == "1"
	opts := gateway.AddOptions{
		Name:          name,
		UpstreamURL:   upstream,
		ProtectedRefs: normalizeProtectedRefs(strings.Fields(r.FormValue("protected_refs"))),
		Enabled:       r.FormValue("enabled") == "1",
		Observe:       r.FormValue("observe") == "1",
		PolicyRoot:    h.policyRoot,
		ReposRoot:     h.reposRoot,
		SelfExe:       h.selfExe,
	}
	if err := gateway.AddRepo(opts); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			// An archived repo keeps its _repos/<name> lib dir, so the name stays
			// reserved and AddRepo reports "already exists" - but Restore/Delete,
			// not "pick another name", is the fix. Point the operator at them.
			if gateway.IsArchivedRepo(h.policyRoot, name) {
				http.Error(w, fmt.Sprintf("A repo named %q is archived - Restore it, or Delete it permanently, from the Archived panel on the Repos page to reuse the name.", name), http.StatusConflict)
				return
			}
			http.Error(w, fmt.Sprintf("A repo named %q is already registered - pick a different name, or change that repo's upstream/refs via Edit repo settings on its Policy page.", name), http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Apply starter kit and optional security-strict to the new repo's per-repo config.
	if err := applyStarterKit(h.policyRoot, name, kitName, securityStrict); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Upstream credential (optional). Stored mode 0600 at
	// <policyRoot>/<repo>/credential. Never logged or surfaced in responses.
	cred := r.FormValue("upstream_credential")
	credSet := false
	if cred != "" {
		if err := (gateway.FileCredentialStore{Root: h.policyRoot}).Save(name, cred); err != nil {
			http.Error(w, "credential save failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		credSet = true
	}
	// Mirror the upstream's existing history into the gateway bare so an
	// already-populated repo is immediately clone-able from the gateway - no
	// manual server-side seeding. No-op for a new/empty upstream. A failure
	// (unreachable upstream, http upstream without a credential yet) does NOT
	// fail registration: SeedAtRegistration drops a marker that surfaces a
	// one-click "Sync from upstream" on /repos.
	seededRefs := 0
	if opts.UpstreamURL != "" {
		res, _ := gateway.SeedAtRegistration(h.policyRoot, h.reposRoot, name, opts.UpstreamURL, cred)
		seededRefs = res.Refs
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "add",
		Repo:  name,
		OK:    true,
		Payload: map[string]any{
			"upstream":        opts.UpstreamURL,
			"protected_refs":  opts.ProtectedRefs,
			"enabled":         opts.Enabled,
			"observe":         opts.Observe,
			"kit":             kitName,
			"security_strict": securityStrict,
			"credential_set":  credSet,
			"seeded_refs":     seededRefs,
		},
	})
	redirectAfterAction(w, r, "/policy?repo="+name+"&registered=1")
}

// credential rotates or first-time-sets the upstream push credential for an
// already-registered repo. Mirrors the add handler's credential write but
// without touching policy/frames/upstream URL. The credential value is read
// from the form and never echoed back, logged, or included in event payloads.
func (h repoLifecycleHandlers) credential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("repo")
	if !validRepoName(name) || name == "_repos" {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	// Confirm the repo actually exists before writing a credential file for it
	// (avoids creating a credential dir for a typo'd repo name).
	if _, err := os.Stat(filepath.Join(h.policyRoot, name, "gateway.toml")); err != nil {
		http.Error(w, "unknown repo", http.StatusBadRequest)
		return
	}
	cred := r.FormValue("upstream_credential")
	if cred == "" {
		http.Error(w, "credential required (clear-credential is not yet supported via the dashboard; remove the credential file manually if needed)", http.StatusBadRequest)
		return
	}
	if err := (gateway.FileCredentialStore{Root: h.policyRoot}).Save(name, cred); err != nil {
		http.Error(w, "credential save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "credential-update",
		Repo:  name,
		OK:    true,
		Payload: map[string]any{
			"credential_set": true,
		},
	})
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// settings updates a registered repo's upstream URL + protected refs in place -
// the two fields that otherwise forced a delete+re-add. It reuses the add
// handler's guards (HTTPS-only/ssh-client check; dup-upstream excluding this
// repo) then Load→mutate→Save, so the notification rail and every other
// gateway.toml field round-trip untouched. Name + credential are out of scope
// (rename = new repo; credential has its own rotate form).
func (h repoLifecycleHandlers) settings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("repo")
	if !validRepoName(name) || name == "_repos" {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	store := gateway.FilePolicyStore{Root: h.policyRoot}
	p, err := store.Load(name)
	if err != nil {
		http.Error(w, "unknown repo", http.StatusBadRequest)
		return
	}
	upstream := r.FormValue("upstream")
	// Same HTTPS-only guard as registration (C): no ssh client → no ssh:// relay.
	if gateway.IsSSHUpstream(upstream) {
		if _, err := exec.LookPath("ssh"); err != nil {
			http.Error(w, "This gateway can't relay to an ssh:// upstream - no SSH client is installed. Use an https:// URL with a Personal Access Token.", http.StatusBadRequest)
			return
		}
	}
	// Dup-upstream guard (B), excluding this repo (re-saving its own URL is fine).
	if upstream != "" {
		for _, existing := range listGatewayRepos(h.policyRoot) {
			if existing == name {
				continue
			}
			if ep, err := store.Load(existing); err == nil && ep.UpstreamURL == upstream {
				http.Error(w, fmt.Sprintf("That upstream is already registered as %q - edit or remove that one instead.", existing), http.StatusConflict)
				return
			}
		}
	}
	p.UpstreamURL = upstream
	p.ProtectedRefs = normalizeProtectedRefs(strings.Fields(r.FormValue("protected_refs")))
	if err := store.Save(p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "settings-update", Repo: name, OK: true,
		Payload: map[string]any{"upstream": upstream, "protected_refs": p.ProtectedRefs},
	})
	redirectAfterAction(w, r, "/repos")
}

// groups replaces the repo's [frames].enabled list with exactly the groups
// ticked in the form (set semantics, not patch). Auto-submitted by the
// /policy page on any checkbox change.
func (h repoLifecycleHandlers) groups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("repo")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	groups := r.Form["group"]
	for _, g := range groups {
		if !validGroupName(g) {
			http.Error(w, "invalid group: "+g, http.StatusBadRequest)
			return
		}
	}
	fp, _ := gateway.LoadFramePolicy(h.policyRoot, name)
	fp.Enabled = groups
	if err := fp.Save(h.policyRoot, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "set-groups",
		Repo:  name,
		OK:    true,
		Payload: map[string]any{
			"groups": groups,
		},
	})
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// validGroupName accepts the conventional @name form used by the scan / frame
// system. Conservative pattern keeps a stray POST from injecting weird data
// into the [frames].enabled list.
func validGroupName(g string) bool {
	if len(g) < 2 || g[0] != '@' {
		return false
	}
	for _, c := range g[1:] {
		if !(c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

// archive removes both activation symlinks (files in _repos/ untouched), logs
// the event, and regenerates _archived.md from the events log.
// repair runs a connection-check auto-repair operation for one repo. The
// operator clicks "Repair" on a per-issue row in the /repos page; the
// handler delegates to Skeleton.Repair (which is responsible for refusing
// unknown operations) and redirects back. Logged as a "repo-connection-repair"
// event so the audit trail captures what was reseeded and when.
func (h repoLifecycleHandlers) repair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("repo")
	if !validRepoName(name) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	op := r.FormValue("operation")
	sk := gateway.Skeleton{PolicyRoot: h.policyRoot, ReposRoot: h.reposRoot}
	if err := sk.Repair(name, op); err != nil {
		http.Error(w, "repair failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "repo-connection-repair", Repo: name, OK: true,
		Payload: map[string]any{"operation": op},
	})
	redirectAfterAction(w, r, "/repos")
}

// observe flips a registered repo between enforce (rejects on a would-block)
// and observe/advisory (records the would-block but relays anyway), preserving
// the rest of the policy. The /repos page renders a single toggle button that
// posts the desired target state in the `observe` field. Redirects back to
// /repos so the badge + button update.
func (h repoLifecycleHandlers) observe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	observe := r.FormValue("observe") == "1"
	if err := (gateway.FilePolicyStore{Root: h.policyRoot}).SetObserve(name, observe); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "set-observe", Repo: name, OK: true,
		Payload: map[string]any{"observe": observe},
	})
	redirectAfterAction(w, r, "/repos")
}

func (h repoLifecycleHandlers) archive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{
		Name: name, PolicyRoot: h.policyRoot, ReposRoot: h.reposRoot,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "archive", Repo: name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(h.policyRoot)
	// Stay on /repos (where the Archive button lives) and surface a notice naming
	// the repo, instead of jumping to /policy - archiving is a repos-page action.
	redirectAfterAction(w, r, "/repos?archived="+name)
}

// restore re-creates both activation symlinks, logs the event, and refreshes
// _archived.md so the restore row lands in the table.
func (h repoLifecycleHandlers) restore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	if err := gateway.RestoreRepo(gateway.RestoreOptions{
		Name: name, PolicyRoot: h.policyRoot, ReposRoot: h.reposRoot,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "restore", Repo: name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(h.policyRoot)
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// delete PERMANENTLY removes an archived repo's whole footprint (bare repo +
// policy/credential dir) via gateway.DeleteRepo, freeing the name + upstream for
// re-registration. The button is rendered only on archived rows, so the flow is
// the deliberate two-step Archive → Delete; this handler also refuses to delete
// a still-active repo (archive it first) so a stray POST can't wipe a live repo.
// Logged as a "delete" event; the upstream and other repos are untouched.
func (h repoLifecycleHandlers) delete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) || name == "_repos" {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	// Only archived repos are deletable from the dashboard: an active repo must be
	// archived first (the symlink-still-present case). Keeps the destructive path
	// behind the explicit two-step the UI presents.
	if _, err := os.Lstat(filepath.Join(h.policyRoot, name)); err == nil {
		http.Error(w, fmt.Sprintf("%q is still active - Archive it first, then Delete permanently.", name), http.StatusConflict)
		return
	}
	if err := gateway.DeleteRepo(gateway.DeleteOptions{
		Name: name, PolicyRoot: h.policyRoot, ReposRoot: h.reposRoot,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "delete", Repo: name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(h.policyRoot)
	redirectAfterAction(w, r, "/repos")
}

// scanApply merges the recommended frame groups (set union) into the repo's
// existing [frames] enabled list, marks the recommendation dismissed, and
// logs the event with the applied groups in the payload.
func (h repoLifecycleHandlers) scanApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	recPath := filepath.Join(h.policyRoot, name, "scan-recommendation.json")
	data, err := os.ReadFile(recPath)
	if err != nil {
		http.Error(w, "no recommendation: "+err.Error(), http.StatusNotFound)
		return
	}
	var rec map[string]any
	if err := json.Unmarshal(data, &rec); err != nil {
		http.Error(w, "parse rec: "+err.Error(), http.StatusInternalServerError)
		return
	}
	recommended := extractRecommendedGroupNames(rec)
	merged, err := mergeFramePolicyGroups(h.policyRoot, name, recommended)
	if err != nil {
		http.Error(w, "merge: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rec["dismissed"] = true
	if out, err := json.Marshal(rec); err == nil {
		_ = os.WriteFile(recPath, out, 0o644)
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "scan-apply",
		Repo:  name,
		OK:    true,
		Payload: map[string]any{
			"applied_groups": recommended,
			"merged_into":    merged,
		},
	})
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// scanDismiss flips dismissed:true on the recommendation file. Does not touch
// the frame policy.
func (h repoLifecycleHandlers) scanDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	recPath := filepath.Join(h.policyRoot, name, "scan-recommendation.json")
	data, err := os.ReadFile(recPath)
	if err != nil {
		http.Error(w, "no recommendation: "+err.Error(), http.StatusNotFound)
		return
	}
	var rec map[string]any
	if err := json.Unmarshal(data, &rec); err != nil {
		http.Error(w, "parse rec: "+err.Error(), http.StatusInternalServerError)
		return
	}
	rec["dismissed"] = true
	if out, err := json.Marshal(rec); err == nil {
		_ = os.WriteFile(recPath, out, 0o644)
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "scan-dismiss", Repo: name, OK: true,
	})
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// scanRescan deletes the recommendation file and re-runs the first-push scan
// synchronously. Logs the event on success.
func (h repoLifecycleHandlers) scanRescan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	name := r.FormValue("name")
	if !validRepoName(name) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	recPath := filepath.Join(h.policyRoot, name, "scan-recommendation.json")
	_ = os.Remove(recPath)
	bare := filepath.Join(h.reposRoot, name+".git")
	if err := gateway.ScanFirstPush(bare, name, h.policyRoot, h.selfExe); err != nil {
		http.Error(w, "scan: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "scan-rescan", Repo: name, OK: true,
	})
	redirectAfterAction(w, r, "/policy?repo="+name)
}

// extractRecommendedGroupNames pulls .recommended_groups[].name from the
// generic decoded map. Tolerates the absent field (returns empty slice).
func extractRecommendedGroupNames(rec map[string]any) []string {
	raw, ok := rec["recommended_groups"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, g := range raw {
		m, ok := g.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := m["name"].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return out
}

// mergeFramePolicyGroups reads the repo's [frames] enabled list, set-unions
// `add` into it, and writes the result back via the existing atomic TOML
// writer (gateway.FramePolicy.Save). Returns the merged list.
func mergeFramePolicyGroups(policyRoot, name string, add []string) ([]string, error) {
	fp, err := gateway.LoadFramePolicy(policyRoot, name)
	if err != nil {
		return nil, err
	}
	have := make(map[string]bool, len(fp.Enabled))
	out := make([]string, 0, len(fp.Enabled)+len(add))
	for _, g := range fp.Enabled {
		if !have[g] {
			have[g] = true
			out = append(out, g)
		}
	}
	for _, g := range add {
		if !have[g] {
			have[g] = true
			out = append(out, g)
		}
	}
	fp.Enabled = out
	if err := fp.Save(policyRoot, name); err != nil {
		return nil, err
	}
	return out, nil
}

// applyStarterKit writes the kit's frames (and optionally security-strict frames)
// into <policyRoot>/<repo>/appframes.toml and records the applied kit names there.
// Called once at registration time from the add handler.
func applyStarterKit(policyRoot, repo, kitName string, securityStrict bool) error {
	ks, err := kits.LoadStdlib()
	if err != nil {
		return err
	}
	k, ok := ks.Get(kitName)
	if !ok {
		return nil // unknown kit name is a no-op; caller already validated
	}
	cfgPath := filepath.Join(policyRoot, repo, "appframes.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return err
	}
	// Build initial TOML content if file doesn't exist yet.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		data = []byte("[frames]\nenabled = []\n")
	}
	doc := string(data)
	for _, id := range k.Frames {
		doc, _, err = rewriteEnabledList(doc, id, true)
		if err != nil {
			return err
		}
	}
	if securityStrict {
		sk, ok := ks.Get("security-strict")
		if ok {
			for _, id := range sk.Frames {
				doc, _, err = rewriteEnabledList(doc, id, true)
				if err != nil {
					return err
				}
			}
		}
	}
	if err := atomicWriteFile(cfgPath, []byte(doc)); err != nil {
		return err
	}
	if err := addAppliedKit(cfgPath, kitName); err != nil {
		return err
	}
	if securityStrict {
		if err := addAppliedKit(cfgPath, "security-strict"); err != nil {
			return err
		}
	}
	return nil
}
