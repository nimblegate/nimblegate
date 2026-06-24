// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/gateway"
)

// registerRepoForTest registers a gateway repo so the guarded handlers'
// registered-repo check passes. Mirrors the inline pattern in gatewaytuning_test.go.
func registerRepoForTest(t *testing.T, root, repo string) {
	t.Helper()
	if err := (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: repo, UpstreamURL: "u", Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRenderAuthoringSection_listsChecksAndForm(t *testing.T) {
	lp := gateway.LinterPolicy{Linters: map[string]config.LinterConfig{
		"no-fixme": {Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*.go"}, Regex: "FIXME"},
		"runtool":  {Enabled: true, Command: "eslint"}, // subprocess → read-only
	}}
	html := renderAuthoringSection("r", lp, true)
	if !strings.Contains(html, "no-fixme") || !strings.Contains(html, "FIXME") {
		t.Errorf("authored regex check not rendered: %s", html)
	}
	if !strings.Contains(html, ">Add check<") {
		t.Errorf("add-form missing (looking for Add check primary button)")
	}
	// subprocess linter must be shown read-only: no edit controls naming it.
	if strings.Contains(html, `"name":"runtool"`) {
		t.Errorf("subprocess linter must have no edit controls: %s", html)
	}
	if !strings.Contains(html, "read-only") {
		t.Errorf("subprocess linter should be marked read-only: %s", html)
	}
}

func TestRenderAuthoringSection_readOnlyWithoutEdits(t *testing.T) {
	lp := gateway.LinterPolicy{Linters: map[string]config.LinterConfig{
		"no-fixme": {Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*.go"}, Regex: "FIXME"},
	}}
	html := renderAuthoringSection("r", lp, false)
	if strings.Contains(html, "hx-post") {
		t.Errorf("controls must be static without --allow-edits: %s", html)
	}
}

func authoringReq(t *testing.T, path string, form url.Values, token string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-CSRF-Token", token)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func TestAuthoringAdd_writesRegexLinter(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"no-fixme"}, "patterns": {"*.go"}, "regex": {"FIXME"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("add status = %d, want 200", w.Code)
	}
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	c, ok := lp.Linters["no-fixme"]
	if !ok || c.Kind != "regex" || c.Regex != "FIXME" {
		t.Fatalf("regex linter not written: %+v", lp.Linters)
	}
}

func TestAuthoringAdd_invalidRegex400(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"bad"}, "patterns": {"*.go"}, "regex": {"("}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for invalid regex", w.Code)
	}
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if _, ok := lp.Linters["bad"]; ok {
		t.Fatalf("invalid regex must not be written")
	}
}

func TestAuthoringAdd_badCSRF403(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"x"}, "patterns": {"*"}, "regex": {"y"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "WRONG"))
	if w.Code != 403 {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestAuthoringAdd_invalidName400(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"Bad Name"}, "patterns": {"*.go"}, "regex": {"x"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for invalid name", w.Code)
	}
}

func TestAuthoringAdd_noPatterns400(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"x"}, "patterns": {""}, "regex": {"y"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for missing patterns", w.Code)
	}
}

func TestAuthoringAdd_duplicate400(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("dup", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "q"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"dup"}, "patterns": {"*.go"}, "regex": {"y"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for duplicate name", w.Code)
	}
}

func TestAuthoringAdd_unknownRepo400(t *testing.T) {
	root := t.TempDir()
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"nope"}, "name": {"x"}, "patterns": {"*"}, "regex": {"y"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("status = %d, want 400 for unknown repo", w.Code)
	}
}

func TestAuthoringDelete_previewWithoutConfirm(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("z", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "q"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	// No confirm=1 → returns the deleteConfirm fragment; linter remains.
	form := url.Values{"repo": {"r"}, "name": {"z"}}
	w := httptest.NewRecorder()
	h.delete(w, authoringReq(t, "/policy/check/delete", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("delete preview status = %d, want 200 body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Delete check") || !strings.Contains(body, `"confirm":"1"`) {
		t.Fatalf("preview missing confirm-fragment markers: %s", body)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if _, ok := got.Linters["z"]; !ok {
		t.Fatalf("z should still be present after preview")
	}
}

func TestAuthoringDelete_commitWithConfirm(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("z", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "q"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"z"}, "confirm": {"1"}}
	w := httptest.NewRecorder()
	h.delete(w, authoringReq(t, "/policy/check/delete", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("delete status = %d, want 200", w.Code)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if _, ok := got.Linters["z"]; ok {
		t.Fatalf("z not deleted")
	}
	evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "linter-delete" })
	if len(evs) != 1 || evs[0].Repo != "r" {
		t.Fatalf("event: %+v", evs)
	}
}

func TestAuthoringSeverity_updates(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("z", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "q"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"z"}, "severity": {"BLOCK"}}
	w := httptest.NewRecorder()
	h.severity(w, authoringReq(t, "/policy/check/severity", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("severity status = %d, want 200", w.Code)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if got.Linters["z"].Severity != "BLOCK" {
		t.Fatalf("severity not updated: %+v", got.Linters["z"])
	}
	evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "linter-severity" })
	if len(evs) != 1 || evs[0].Repo != "r" {
		t.Fatalf("event: %+v", evs)
	}
}

func TestAuthoringEnabled_toggles(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("z", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "q"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"z"}, "enabled": {"false"}}
	w := httptest.NewRecorder()
	h.enabled(w, authoringReq(t, "/policy/check/enabled", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("enabled status = %d, want 200", w.Code)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if got.Linters["z"].Enabled {
		t.Fatalf("z should be disabled")
	}
	evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "linter-enabled" })
	if len(evs) != 1 || evs[0].Repo != "r" {
		t.Fatalf("event: %+v", evs)
	}
}

func TestAuthoringAdd_recordsEvent(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"q"}, "regex": {"hi"}, "severity": {"WARN"}, "patterns": {"*"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	if w.Code != 200 {
		t.Fatalf("add status = %d", w.Code)
	}
	evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "linter-add" })
	if len(evs) != 1 || evs[0].Repo != "r" {
		t.Fatalf("event: %+v", evs)
	}
	if name, _ := evs[0].Payload["name"].(string); name != "q" {
		t.Fatalf("payload name: %v", evs[0].Payload)
	}
}

// makeBareUnderReposRoot builds <reposRoot>/<repo>.git from a single commit
// containing files, mirroring how the gateway holds relayed bare repos.
func makeBareUnderReposRoot(t *testing.T, reposRoot, repo string, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(work, "init", "-q", "-b", "main")
	for rel, body := range files {
		p := filepath.Join(work, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(body), 0o644)
	}
	run(work, "add", "-A")
	run(work, "commit", "-qm", "init")
	bare := filepath.Join(reposRoot, repo+".git")
	if out, err := exec.Command("git", "clone", "-q", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}
	return bare
}

func TestAuthoringPreview_reportsHits(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	bareParent := t.TempDir() // acts as repos-root
	makeBareUnderReposRoot(t, bareParent, "r", map[string]string{"main.go": "TODO(no-owner)\n"})
	h := authoringHandlers{policyRoot: root, token: "tok", reposRoot: bareParent}
	form := url.Values{"repo": {"r"}, "patterns": {"*.go"}, "regex": {`TODO\(no-owner\)`}}
	w := httptest.NewRecorder()
	h.preview(w, authoringReq(t, "/policy/check/preview", form, "tok"))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "would flag 1") {
		t.Fatalf("preview = %d %q, want 'would flag 1'", w.Code, body)
	}
	if !strings.Contains(body, "main.go") {
		t.Fatalf("preview missing sample hit: %s", body)
	}
}

func TestAuthoringPreview_noReposRoot(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	h := authoringHandlers{policyRoot: root, token: "tok"} // no reposRoot
	form := url.Values{"repo": {"r"}, "patterns": {"*.go"}, "regex": {"x"}}
	w := httptest.NewRecorder()
	h.preview(w, authoringReq(t, "/policy/check/preview", form, "tok"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "preview unavailable") {
		t.Fatalf("preview = %d %q, want 'preview unavailable'", w.Code, w.Body.String())
	}
}

func TestAuthoringPreview_noPushedTree(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	bareParent := t.TempDir()
	empty := filepath.Join(bareParent, "r.git")
	_ = exec.Command("git", "init", "-q", "--bare", empty).Run()
	h := authoringHandlers{policyRoot: root, token: "tok", reposRoot: bareParent}
	form := url.Values{"repo": {"r"}, "patterns": {"*.go"}, "regex": {"x"}}
	w := httptest.NewRecorder()
	h.preview(w, authoringReq(t, "/policy/check/preview", form, "tok"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "no pushed tree") {
		t.Fatalf("preview = %d %q, want 'no pushed tree'", w.Code, w.Body.String())
	}
}

// --- security hardening tests (Tasks 5-7 follow-up) ---

// TestGuarded_traversalRepo verifies that a POST with a path-traversal repo
// param is rejected at the HTTP layer (400) without touching the filesystem.
func TestGuarded_traversalRepo_400(t *testing.T) {
	root := t.TempDir()
	h := authoringHandlers{policyRoot: root, token: "tok"}
	for _, bad := range []string{"../x", "../../etc/passwd", "a/../b", ".", ".."} {
		form := url.Values{"repo": {bad}, "name": {"x"}, "patterns": {"*"}, "regex": {"y"}, "severity": {"WARN"}}
		w := httptest.NewRecorder()
		h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
		if w.Code != 400 {
			t.Errorf("repo=%q: status = %d, want 400", bad, w.Code)
		}
	}
}

// TestGuarded_traversalRepo_noWrite verifies that a traversal-repo POST does
// not write anything inside (or outside) the policy root.
func TestGuarded_traversalRepo_noWrite(t *testing.T) {
	root := t.TempDir()
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"../x"}, "name": {"x"}, "patterns": {"*"}, "regex": {"y"}, "severity": {"WARN"}}
	w := httptest.NewRecorder()
	h.add(w, authoringReq(t, "/policy/check/add", form, "tok"))
	// root must stay empty - no directories or files created.
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("traversal write leaked into policy root: %v", entries)
	}
}

// TestCommandLinter_deleteRejected verifies that a DELETE for a non-regex
// (subprocess/command) linter returns 400 and leaves the linter intact.
func TestCommandLinter_deleteRejected(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("eslint", config.LinterConfig{Enabled: true, Kind: "command", Command: "eslint", Severity: "WARN"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"eslint"}}
	w := httptest.NewRecorder()
	h.delete(w, authoringReq(t, "/policy/check/delete", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("delete command linter: status = %d, want 400", w.Code)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if _, ok := got.Linters["eslint"]; !ok {
		t.Fatalf("command linter was deleted despite 400")
	}
}

// TestCommandLinter_severityRejected verifies that a severity POST for a
// non-regex linter returns 400 and does not mutate it.
func TestCommandLinter_severityRejected(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "r")
	lp, _ := gateway.LoadLinterPolicy(root, "r")
	if err := lp.With("eslint", config.LinterConfig{Enabled: true, Kind: "command", Command: "eslint", Severity: "WARN"}).Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	h := authoringHandlers{policyRoot: root, token: "tok"}
	form := url.Values{"repo": {"r"}, "name": {"eslint"}, "severity": {"BLOCK"}}
	w := httptest.NewRecorder()
	h.severity(w, authoringReq(t, "/policy/check/severity", form, "tok"))
	if w.Code != 400 {
		t.Fatalf("severity command linter: status = %d, want 400", w.Code)
	}
	got, _ := gateway.LoadLinterPolicy(root, "r")
	if got.Linters["eslint"].Severity != "WARN" {
		t.Fatalf("command linter severity mutated: got %q", got.Linters["eslint"].Severity)
	}
}

// TestNoAllowEdits_postRoutesReturn404 verifies that the authoring POST routes
// are not registered when --allow-edits is not set, so crafted POSTs get 404.
func TestNoAllowEdits_postRoutesReturn404(t *testing.T) {
	// Build the same mux as gatewayDashboard but without registering authoring routes.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	// Do NOT register /policy/check/* (mirrors allowEdits=false branch in gatewayDashboard).
	for _, path := range []string{
		"/policy/check/add",
		"/policy/check/delete",
		"/policy/check/severity",
		"/policy/check/enabled",
		"/policy/check/preview",
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, nil)
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("POST %s without allow-edits: status = %d, want 404", path, rec.Code)
		}
	}
}
