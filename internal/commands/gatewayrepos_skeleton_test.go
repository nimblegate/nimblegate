// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReposPage_NoIssuesBannerWhenClean confirms the banner is absent when
// AddRepo+credential install leaves Verify clean. Default of the happy path
// - operator should NOT see clutter when everything works.
func TestReposPage_NoIssuesBannerWhenClean(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
		ReposRoot:  reposRoot,
	})

	if strings.Contains(body, "Issues to address") {
		t.Errorf("clean repo state should NOT render the issues banner; body contains it")
	}
}

// TestReposPage_IssuesBannerShowsMissingAppframesTOML deletes the seeded
// appframes.toml and asserts the banner surfaces the issue with a Repair
// button - the exact "click does nothing, no feedback" trap inverted.
func TestReposPage_IssuesBannerShowsMissingAppframesTOML(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	if err := os.Remove(filepath.Join(policyRoot, "demo", "appframes.toml")); err != nil {
		t.Fatal(err)
	}

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
		ReposRoot:  reposRoot,
	})

	for _, want := range []string{
		"Issues to address",
		"appframes.toml",
		`name="operation" value="regen-nimblegate-toml"`,
		`/policy/repo/repair`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

// TestReposPage_IssuesBannerHidesRepairForNonAutoRepair confirms blocking
// issues without auto-repair (missing credential for HTTPS upstream) render
// "operator action" text instead of a Repair button. Tells the operator
// the dashboard can't fix it for them.
func TestReposPage_IssuesBannerHidesRepairForNonAutoRepair(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedReposTestRepo(t, policyRoot, reposRoot, "demo", "http://gitea.internal/you/demo.git")
	// HTTP upstream + no credential file → blocking issue, no auto-repair.

	body := renderReposBody(t, reposPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		PolicyRoot: policyRoot,
		ReposRoot:  reposRoot,
	})

	if !strings.Contains(body, "Issues to address") {
		t.Errorf("body should contain issues banner; got\n%s", body)
	}
	if !strings.Contains(body, "operator action") {
		t.Errorf("body should show 'operator action' for non-auto-repair issue; got\n%s", body)
	}
	// Should NOT contain a Repair button for the credential issue.
	if strings.Contains(body, `value="regen-credential"`) {
		t.Errorf("body should NOT contain regen-credential repair (no auto-repair); got\n%s", body)
	}
}

// TestRepoLifecycle_repairHandler exercises the POST endpoint end-to-end:
// delete appframes.toml, hit /policy/repo/repair with regen-nimblegate-toml,
// confirm the file is recreated and a skeleton-repair event is logged.
func TestRepoLifecycle_repairHandler(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "demo")
	target := filepath.Join(policyRoot, "demo", "appframes.toml")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{
		"repo":      {"demo"},
		"operation": {"regen-nimblegate-toml"},
	}
	req := rlPost(t, "/policy/repo/repair", form, "tok")
	w := httptest.NewRecorder()
	h.repair(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("Repair did not recreate appframes.toml: %v", err)
	}
}

// TestRepoLifecycle_repairHandlerRejectsUnknownOp confirms the handler
// returns 400 (not 500) for unknown operations - so the dashboard surfaces
// a meaningful error rather than a generic internal server error.
func TestRepoLifecycle_repairHandlerRejectsUnknownOp(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "demo")

	h := repoLifecycleHandlers{policyRoot: policyRoot, reposRoot: reposRoot, selfExe: "/bin/true", token: "tok"}
	form := url.Values{"repo": {"demo"}, "operation": {"format-c"}}
	req := rlPost(t, "/policy/repo/repair", form, "tok")
	w := httptest.NewRecorder()
	h.repair(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d body=%s", w.Code, w.Body.String())
	}
}
