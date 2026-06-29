// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// rlPost is a small POST helper mirroring postWL/postAuth: it always sets the
// urlencoded Content-Type and an X-CSRF-Token header (the handlers read CSRF
// from the header, not the form).
func rlPost(t *testing.T, target string, form url.Values, csrf string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	return req
}

// seedActiveRepo creates a fully-registered (lib + activation symlinks + bare
// repo with hooks) repo via gateway.AddRepo. selfExe is /bin/true so the
// hook scripts contain a sane path; tests don't invoke the hooks.
func seedActiveRepo(t *testing.T, policyRoot, reposRoot, name string) {
	t.Helper()
	if err := gateway.AddRepo(gateway.AddOptions{
		Name:        name,
		UpstreamURL: "http://example.test/" + name + ".git",
		Enabled:     true,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     "/bin/true",
	}); err != nil {
		t.Fatalf("seedActiveRepo: %v", err)
	}
}

func TestRepoLifecycle_addHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{
		"name":           {"newrepo"},
		"upstream":       {"http://example.test/newrepo.git"},
		"protected_refs": {"main release/*"},
		"enabled":        {"1"},
		"kit":            {"core"},
	}
	req := rlPost(t, "/policy/repo/add", form, "tok")
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "newrepo", "gateway.toml")); err != nil {
		t.Fatalf("gateway.toml: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(policyRoot, "newrepo")); err != nil {
		t.Fatalf("policy symlink: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(reposRoot, "newrepo.git")); err != nil {
		t.Fatalf("bare symlink: %v", err)
	}
	// Starter kit frames written to per-repo appframes.toml.
	fp, err := gateway.LoadFramePolicy(policyRoot, "newrepo")
	if err != nil {
		t.Fatalf("load frame policy: %v", err)
	}
	if len(fp.Enabled) != 15 {
		t.Fatalf("core kit should enable 15 frames; got %d: %v", len(fp.Enabled), fp.Enabled)
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "add" })
	if len(events) != 1 || events[0].Repo != "newrepo" {
		t.Fatalf("event: %+v", events)
	}
}

// dashboard add-repo normalizes operator-friendly ref names to the full
// refs/heads/<name> form that gateway.isGatedRef matches. Without this, a
// repo registered with the bare-`main` input would ship UNGATED - the gate
// installs the hook but every ref evaluates as un-protected and no frames
// run. Surfaced during the 2026-06-02 T5 dry-run.
func TestRepoLifecycle_addHandler_normalizesProtectedRefs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"bareBranch", "main", []string{"refs/heads/main"}},
		{"multipleBare", "main develop", []string{"refs/heads/main", "refs/heads/develop"}},
		{"fullRef", "refs/heads/main", []string{"refs/heads/main"}},
		{"defaultGateAll", "refs/heads/*", []string{"refs/heads/*"}},
		{"tagRef", "refs/tags/v*", []string{"refs/tags/v*"}},
		{"mixed", "main refs/tags/v*", []string{"refs/heads/main", "refs/tags/v*"}},
		{"whitespace", "  main   release/*  ", []string{"refs/heads/main", "refs/heads/release/*"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeProtectedRefs(strings.Fields(tc.in))
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestRepoLifecycle_addHandler_savedPolicyHasNormalizedRefs(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{
		"name":           {"normrepo"},
		"upstream":       {"http://example.test/normrepo.git"},
		"protected_refs": {"main"}, // bare branch - the foot-gun
		"enabled":        {"1"},
	}
	w := httptest.NewRecorder()
	h.add(w, rlPost(t, "/policy/repo/add", form, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	// The saved gateway.toml must carry the full ref name so isGatedRef matches.
	store := gateway.FilePolicyStore{Root: policyRoot}
	p, err := store.Load("normrepo")
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	if len(p.ProtectedRefs) != 1 || p.ProtectedRefs[0] != "refs/heads/main" {
		t.Fatalf("ProtectedRefs not normalized: got %v, want [refs/heads/main]", p.ProtectedRefs)
	}
}

func TestRepoLifecycle_addHandler_rejectsBadCSRF(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	req := rlPost(t, "/policy/repo/add", url.Values{"name": {"x"}}, "wrong")
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestRepoLifecycle_addHandler_rejectsNonPost(t *testing.T) {
	h := repoLifecycleHandlers{token: "tok"}
	req := httptest.NewRequest("GET", "/policy/repo/add", nil)
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", w.Code)
	}
}

func TestRepoLifecycle_observeHandler_flipsPolicy(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	store := gateway.FilePolicyStore{Root: policyRoot}
	if err := store.Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", ProtectedRefs: []string{"refs/heads/main"}, Enabled: true, Observe: false}); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	// enforce -> observe
	w := httptest.NewRecorder()
	h.observe(w, rlPost(t, "/policy/repo/observe", url.Values{"name": {"demo"}, "observe": {"1"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if p, _ := store.Load("demo"); !p.Observe {
		t.Error("observe handler did not set observe=true")
	}

	// observe -> enforce (and prove it doesn't clobber the rest)
	w = httptest.NewRecorder()
	h.observe(w, rlPost(t, "/policy/repo/observe", url.Values{"name": {"demo"}, "observe": {"0"}}, "tok"))
	p, _ := store.Load("demo")
	if p.Observe {
		t.Error("observe handler did not set observe=false")
	}
	if p.UpstreamURL != "u" || !p.Enabled || len(p.ProtectedRefs) != 1 {
		t.Errorf("observe handler clobbered other policy fields: %+v", p)
	}
}

func TestRepoLifecycle_observeHandler_rejectsBadCSRF(t *testing.T) {
	h := repoLifecycleHandlers{policyRoot: t.TempDir(), token: "tok"}
	w := httptest.NewRecorder()
	h.observe(w, rlPost(t, "/policy/repo/observe", url.Values{"name": {"demo"}, "observe": {"1"}}, "wrong"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestRepoLifecycle_addHandler_rejectsBadName(t *testing.T) {
	tmp := t.TempDir()
	h := repoLifecycleHandlers{policyRoot: filepath.Join(tmp, "p"), reposRoot: filepath.Join(tmp, "r"), token: "tok"}
	req := rlPost(t, "/policy/repo/add", url.Values{"name": {"../evil"}}, "tok")
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", w.Code)
	}
}

func TestRepoLifecycle_archiveHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.archive(w, rlPost(t, "/policy/repo/archive", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	// Both activation symlinks gone.
	if _, err := os.Lstat(filepath.Join(policyRoot, "foo")); err == nil {
		t.Fatal("policy symlink should be removed")
	}
	if _, err := os.Lstat(filepath.Join(reposRoot, "foo.git")); err == nil {
		t.Fatal("bare symlink should be removed")
	}
	// Event + _archived.md present with the row.
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "archive" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("event: %+v", events)
	}
	data, err := os.ReadFile(filepath.Join(policyRoot, "_archived.md"))
	if err != nil {
		t.Fatalf("_archived.md: %v", err)
	}
	if !strings.Contains(string(data), "foo") || !strings.Contains(string(data), "archive") {
		t.Fatalf("_archived.md missing row: %s", data)
	}
}

func TestRepoLifecycle_archiveHandler_rejectsBadCSRF(t *testing.T) {
	h := repoLifecycleHandlers{token: "tok"}
	w := httptest.NewRecorder()
	h.archive(w, rlPost(t, "/policy/repo/archive", url.Values{"name": {"foo"}}, "nope"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestRepoLifecycle_restoreHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Archive it first to set the precondition for restore.
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.restore(w, rlPost(t, "/policy/repo/restore", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Lstat(filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatal("policy symlink should be restored")
	}
	if _, err := os.Lstat(filepath.Join(reposRoot, "foo.git")); err != nil {
		t.Fatal("bare symlink should be restored")
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "restore" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("event: %+v", events)
	}
}

func TestRepoLifecycle_scanApplyHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Pre-seed [frames] enabled with @tier-1.
	fp := gateway.FramePolicy{Enabled: []string{"@tier-1"}, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, "foo"); err != nil {
		t.Fatal(err)
	}
	// Recommendation file recommends @tier-1 + @web.
	rec := map[string]any{
		"scanned_at": "2026-05-30T12:00:00Z",
		"tree_ref":   "HEAD",
		"recommended_groups": []any{
			map[string]any{"name": "@tier-1", "always": true, "would_flag": 0},
			map[string]any{"name": "@web", "always": false, "would_flag": 3},
		},
		"dismissed": false,
	}
	data, _ := json.Marshal(rec)
	recPath := filepath.Join(policyRoot, "foo", "scan-recommendation.json")
	if err := os.WriteFile(recPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.scanApply(w, rlPost(t, "/policy/repo/scan-apply", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	// Union: @tier-1 + @web, in that order.
	fp2, err := gateway.LoadFramePolicy(policyRoot, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp2.Enabled) != 2 || fp2.Enabled[0] != "@tier-1" || fp2.Enabled[1] != "@web" {
		t.Fatalf("merged enabled: %+v", fp2.Enabled)
	}
	// Recommendation marked dismissed.
	raw, _ := os.ReadFile(recPath)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["dismissed"] != true {
		t.Fatalf("rec not dismissed: %+v", got)
	}
	// Event recorded with applied_groups payload.
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "scan-apply" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("event: %+v", events)
	}
	applied, ok := events[0].Payload["applied_groups"].([]any)
	if !ok || len(applied) != 2 {
		t.Fatalf("applied_groups payload: %+v", events[0].Payload)
	}
}

func TestRepoLifecycle_scanDismissHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Pre-seed frames enabled so we can assert it's unchanged after dismiss.
	fp := gateway.FramePolicy{Enabled: []string{"@tier-1"}, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, "foo"); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{
		"recommended_groups": []any{
			map[string]any{"name": "@web", "always": false, "would_flag": 1},
		},
		"dismissed": false,
	}
	data, _ := json.Marshal(rec)
	recPath := filepath.Join(policyRoot, "foo", "scan-recommendation.json")
	if err := os.WriteFile(recPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.scanDismiss(w, rlPost(t, "/policy/repo/scan-dismiss", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	raw, _ := os.ReadFile(recPath)
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if got["dismissed"] != true {
		t.Fatalf("rec not dismissed: %+v", got)
	}
	// Enabled groups unchanged.
	fp2, _ := gateway.LoadFramePolicy(policyRoot, "foo")
	if len(fp2.Enabled) != 1 || fp2.Enabled[0] != "@tier-1" {
		t.Fatalf("dismiss should not touch enabled: %+v", fp2.Enabled)
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "scan-dismiss" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("event: %+v", events)
	}
}

func TestRepoLifecycle_scanRescanHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Seed a commit in the bare so git archive HEAD has something to extract.
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "x"},
		// Push to the activation symlink (which resolves to _repos/foo.git).
		{"push", "-q", filepath.Join(reposRoot, "foo.git"), "HEAD:refs/heads/main"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Pre-seed a stale rec file we expect to be overwritten.
	recPath := filepath.Join(policyRoot, "foo", "scan-recommendation.json")
	if err := os.WriteFile(recPath, []byte(`{"stale":true,"dismissed":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Fake selfExe: shell script that prints a known JSON.
	freshJSON := `{"scanned_at":"2026-05-30T13:00:00Z","tree_ref":"HEAD","recommended_groups":[{"name":"@tier-1","always":true,"would_flag":0}],"dismissed":false}`
	fakeExe := filepath.Join(tmp, "fake-scan")
	script := "#!/bin/sh\ncat <<'EOF'\n" + freshJSON + "\nEOF\n"
	if err := os.WriteFile(fakeExe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: fakeExe, token: "tok"}
	w := httptest.NewRecorder()
	h.scanRescan(w, rlPost(t, "/policy/repo/scan-rescan", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	// Rec overwritten with fresh content (no longer "stale").
	raw, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatalf("rec: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse rec: %v: %s", err, raw)
	}
	if _, isStale := got["stale"]; isStale {
		t.Fatalf("rescan must overwrite stale file: %s", raw)
	}
	if got["dismissed"] != false {
		t.Fatalf("rescan should produce dismissed:false, got %+v", got)
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "scan-rescan" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("event: %+v", events)
	}
}

func TestRepoLifecycle_groupsHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Seed @tier-1 alone.
	if err := (gateway.FramePolicy{Enabled: []string{"@tier-1"}, Severity: map[string]string{}}).Save(policyRoot, "foo"); err != nil {
		t.Fatal(err)
	}

	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{
		"repo":  {"foo"},
		"group": {"@tier-1", "@web", "@cf-pages"},
	}
	req := rlPost(t, "/policy/repo/groups", form, "tok")
	w := httptest.NewRecorder()
	h.groups(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}

	got, err := gateway.LoadFramePolicy(policyRoot, "foo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Enabled) != 3 || got.Enabled[0] != "@tier-1" || got.Enabled[1] != "@web" || got.Enabled[2] != "@cf-pages" {
		t.Fatalf("enabled: %v", got.Enabled)
	}

	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "set-groups" })
	if len(evs) != 1 || evs[0].Repo != "foo" {
		t.Fatalf("event: %+v", evs)
	}
}

func TestRepoLifecycle_groupsHandler_unticksAll(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	_ = (gateway.FramePolicy{Enabled: []string{"@tier-1", "@web"}, Severity: map[string]string{}}).Save(policyRoot, "foo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	// No group entries → enabled list becomes empty.
	form := url.Values{"repo": {"foo"}}
	req := rlPost(t, "/policy/repo/groups", form, "tok")
	w := httptest.NewRecorder()
	h.groups(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: %d", w.Code)
	}
	got, _ := gateway.LoadFramePolicy(policyRoot, "foo")
	if len(got.Enabled) != 0 {
		t.Fatalf("want empty enabled, got %v", got.Enabled)
	}
}

func TestRepoLifecycle_groupsHandler_rejectsInvalidGroup(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{"repo": {"foo"}, "group": {"no-at-sign"}}
	req := rlPost(t, "/policy/repo/groups", form, "tok")
	w := httptest.NewRecorder()
	h.groups(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
}

// TestRepoLifecycle_htmxRedirect confirms that requests carrying HX-Request: true
// receive a 200 + HX-Redirect header (triggering a real browser navigation in
// htmx) instead of a 303. Without this, htmx with no hx-target follows the
// 303 and swaps the destination page's HTML into the submitting form's
// container - inlining the whole site into a tiny corner of the form.
func TestRepoLifecycle_htmxRedirect(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	form := url.Values{"name": {"foo"}}
	req := rlPost(t, "/policy/repo/archive", form, "tok")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.archive(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200 got %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("HX-Redirect"); got != "/repos?archived=foo" {
		t.Fatalf("HX-Redirect: want /repos?archived=foo got %q", got)
	}
}

// --- T3 - upstream credential via dashboard ---

// readEventsBytes returns the raw _events.jsonl contents (for asserting the
// credential never lands in the audit log).
func readEventsBytes(t *testing.T, policyRoot string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(policyRoot, "_events.jsonl"))
	if err != nil {
		t.Fatalf("read _events.jsonl: %v", err)
	}
	return b
}

func TestRepoLifecycle_addHandler_withCredential(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	const secret = "ghp_supersecretdeploytoken12345"
	form := url.Values{
		"name":                {"credrepo"},
		"upstream":            {"https://github.com/example/credrepo.git"},
		"upstream_credential": {secret},
		"protected_refs":      {"main"},
		"enabled":             {"1"},
		"group":               {"@tier-1"},
	}
	req := rlPost(t, "/policy/repo/add", form, "tok")
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}

	credPath := filepath.Join(policyRoot, "credrepo", "credential")
	got, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read credential file: %v", err)
	}
	if string(got) != secret {
		t.Errorf("credential content = %q, want %q", string(got), secret)
	}
	st, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("credential perms = %v, want 0600", st.Mode().Perm())
	}
	// The whole point: secret must NOT appear in the audit log.
	if events := readEventsBytes(t, policyRoot); strings.Contains(string(events), secret) {
		t.Errorf("credential string leaked into _events.jsonl:\n%s", string(events))
	}
	// The flag indicating a credential WAS set should appear (truthy signal
	// without leaking the secret).
	if !strings.Contains(string(readEventsBytes(t, policyRoot)), `"credential_set":true`) {
		t.Errorf("credential_set:true marker missing from event payload")
	}
}

func TestRepoLifecycle_addHandler_noCredential(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	form := url.Values{
		"name":     {"nocred"},
		"upstream": {"file:///srv/upstream/nocred.git"},
		"enabled":  {"1"},
		"group":    {"@tier-1"},
	}
	req := rlPost(t, "/policy/repo/add", form, "tok")
	w := httptest.NewRecorder()
	h.add(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "nocred", "credential")); err == nil {
		t.Errorf("credential file should not exist when upstream_credential is empty")
	}
	if !strings.Contains(string(readEventsBytes(t, policyRoot)), `"credential_set":false`) {
		t.Errorf("credential_set:false marker missing from event payload")
	}
}

func TestRepoLifecycle_credentialHandler_rotate(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "rotrepo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	// First write
	const oldSecret = "old-token-v1"
	_ = (gateway.FileCredentialStore{Root: policyRoot}).Save("rotrepo", oldSecret)

	const newSecret = "new-rotated-token-v2"
	form := url.Values{
		"repo":                {"rotrepo"},
		"upstream_credential": {newSecret},
	}
	req := rlPost(t, "/policy/repo/credential", form, "tok")
	w := httptest.NewRecorder()
	h.credential(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}

	got, err := os.ReadFile(filepath.Join(policyRoot, "rotrepo", "credential"))
	if err != nil {
		t.Fatalf("read credential: %v", err)
	}
	if string(got) != newSecret {
		t.Errorf("post-rotation credential = %q, want %q", string(got), newSecret)
	}
	if strings.Contains(string(got), oldSecret) {
		t.Error("old secret still present after rotation")
	}
	events := readEventsBytes(t, policyRoot)
	if strings.Contains(string(events), oldSecret) || strings.Contains(string(events), newSecret) {
		t.Errorf("credential leaked into _events.jsonl after rotation:\n%s", string(events))
	}
	if !strings.Contains(string(events), `"event":"credential-update"`) {
		t.Errorf(`expected credential-update event in audit log; got:\n%s`, string(events))
	}
}

func TestRepoLifecycle_credentialHandler_emptyRejected(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "emptycred")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	form := url.Values{
		"repo":                {"emptycred"},
		"upstream_credential": {""},
	}
	req := rlPost(t, "/policy/repo/credential", form, "tok")
	w := httptest.NewRecorder()
	h.credential(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "credential required") {
		t.Errorf("error message should mention credential is required; got %q", w.Body.String())
	}
}

func TestRepoLifecycle_credentialHandler_unknownRepoRejected(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	_ = os.MkdirAll(policyRoot, 0o755)
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: tmp, selfExe: "/bin/true", token: "tok"}

	form := url.Values{
		"repo":                {"doesnotexist"},
		"upstream_credential": {"any"},
	}
	req := rlPost(t, "/policy/repo/credential", form, "tok")
	w := httptest.NewRecorder()
	h.credential(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "unknown repo") {
		t.Errorf("error should say 'unknown repo'; got %q", w.Body.String())
	}
}

func TestRepoLifecycle_credentialHandler_csrfMissing(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "csrftest")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	form := url.Values{
		"repo":                {"csrftest"},
		"upstream_credential": {"any"},
	}
	req := rlPost(t, "/policy/repo/credential", form, "") // no token
	w := httptest.NewRecorder()
	h.credential(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: want 403, got %d", w.Code)
	}
}

// Sanity check that decoded JSON event payloads carry the secret-free marker
// but never the secret. Parallel to the string-grep check above; gives the
// failure a clearer error path if the layout changes.
func TestRepoLifecycle_credential_payloadDecodes(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "decodetest")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}

	const secret = "should-never-appear-anywhere-but-the-credential-file"
	form := url.Values{"repo": {"decodetest"}, "upstream_credential": {secret}}
	req := rlPost(t, "/policy/repo/credential", form, "tok")
	w := httptest.NewRecorder()
	h.credential(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d", w.Code)
	}

	// Decode each event line and inspect payload by structure.
	for _, line := range strings.Split(strings.TrimSpace(string(readEventsBytes(t, policyRoot))), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Event   string         `json:"event"`
			Repo    string         `json:"repo"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("decode event line %q: %v", line, err)
		}
		for k, v := range ev.Payload {
			if s, ok := v.(string); ok && strings.Contains(s, secret) {
				t.Errorf("secret leaked into payload field %q of event %q: %q", k, ev.Event, s)
			}
		}
	}
	// Verify the credential file holds exactly the secret.
	got, _ := os.ReadFile(filepath.Join(policyRoot, "decodetest", "credential"))
	if string(got) != secret {
		t.Errorf("credential file content = %q, want %q", string(got), secret)
	}
}

// silence "imported and not used" for exec on early lines; the seed helper
// transitively pulls it via gateway.AddRepo (hooks), so this is a no-op.
var _ = exec.Command

func TestRepoLifecycle_settingsHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	store := gateway.FilePolicyStore{Root: policyRoot}
	before, _ := store.Load("foo")
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{
		"repo":           {"foo"},
		"upstream":       {"https://github.com/owner/foo2.git"},
		"protected_refs": {"refs/heads/*"},
	}
	w := httptest.NewRecorder()
	h.settings(w, rlPost(t, "/policy/repo/settings", form, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	after, err := store.Load("foo")
	if err != nil {
		t.Fatal(err)
	}
	if after.UpstreamURL != "https://github.com/owner/foo2.git" {
		t.Fatalf("upstream not updated: %q", after.UpstreamURL)
	}
	if len(after.ProtectedRefs) != 1 || after.ProtectedRefs[0] != "refs/heads/*" {
		t.Fatalf("protected refs not updated: %v", after.ProtectedRefs)
	}
	// Round-trip preserved the rest of the policy (Load→mutate→Save) - Enabled
	// stands in for every non-edited field (notification rail, max-input-size).
	if after.Enabled != before.Enabled {
		t.Fatalf("Enabled changed across settings update: before=%v after=%v", before.Enabled, after.Enabled)
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "settings-update" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("settings-update event: %+v", events)
	}
}

func TestRepoLifecycle_settingsHandler_dupUpstreamExcludesSelf(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo") // upstream http://example.test/foo.git
	seedActiveRepo(t, policyRoot, reposRoot, "bar") // upstream http://example.test/bar.git
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	// Re-saving foo's OWN upstream is allowed (must not self-collide).
	w := httptest.NewRecorder()
	h.settings(w, rlPost(t, "/policy/repo/settings", url.Values{"repo": {"foo"}, "upstream": {"http://example.test/foo.git"}, "protected_refs": {"refs/heads/main"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("self-upstream should be allowed, got %d body=%s", w.Code, w.Body.String())
	}
	// Pointing foo at bar's upstream is blocked.
	w = httptest.NewRecorder()
	h.settings(w, rlPost(t, "/policy/repo/settings", url.Values{"repo": {"foo"}, "upstream": {"http://example.test/bar.git"}, "protected_refs": {"refs/heads/main"}}, "tok"))
	if w.Code != http.StatusConflict {
		t.Fatalf("dup upstream should be blocked, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRepoLifecycle_settingsHandler_rejectsBadCSRF(t *testing.T) {
	h := repoLifecycleHandlers{token: "tok"}
	w := httptest.NewRecorder()
	h.settings(w, rlPost(t, "/policy/repo/settings", url.Values{"repo": {"foo"}}, "nope"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

func TestRepoLifecycle_deleteHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// Must be archived before delete is allowed from the dashboard.
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.delete(w, rlPost(t, "/policy/repo/delete", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	// Lib dirs (the footprint that reserved the name) are gone in both roots.
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo")); !os.IsNotExist(err) {
		t.Fatalf("policy lib dir should be removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git")); !os.IsNotExist(err) {
		t.Fatalf("bare lib dir should be removed, err=%v", err)
	}
	if gateway.IsArchivedRepo(policyRoot, "foo") {
		t.Fatal("repo should no longer be archived after delete")
	}
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "delete" })
	if len(events) != 1 || events[0].Repo != "foo" {
		t.Fatalf("delete event: %+v", events)
	}
}

func TestRepoLifecycle_deleteHandler_refusesActive(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo") // active, NOT archived
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	h.delete(w, rlPost(t, "/policy/repo/delete", url.Values{"name": {"foo"}}, "tok"))
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409 (must archive first); body=%s", w.Code, w.Body.String())
	}
	// Footprint untouched.
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo", "gateway.toml")); err != nil {
		t.Fatalf("active repo footprint should survive a refused delete: %v", err)
	}
}

func TestRepoLifecycle_deleteHandler_rejectsBadCSRF(t *testing.T) {
	h := repoLifecycleHandlers{token: "tok"}
	w := httptest.NewRecorder()
	h.delete(w, rlPost(t, "/policy/repo/delete", url.Values{"name": {"foo"}}, "nope"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403", w.Code)
	}
}

// An archived name still reserves the slot, so re-adding it reports a conflict -
// but with the Restore/Delete-aware message, not the generic "pick another name".
func TestRepoLifecycle_addHandler_archivedNameGivesRestoreDeleteMessage(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	w := httptest.NewRecorder()
	form := url.Values{"name": {"foo"}, "upstream": {"http://example.test/foo.git"}, "enabled": {"1"}}
	h.add(w, rlPost(t, "/policy/repo/add", form, "tok"))
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d, want 409; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "archived") {
		t.Fatalf("expected archived-aware message, got: %s", w.Body.String())
	}
}
