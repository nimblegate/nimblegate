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

	"nimblegate/internal/gateway"
)

// teSeedRepo registers a repo with the activation-symlink layout the new handler
// requires. Returns nothing - t.Fatal on failure.
func teSeedRepo(t *testing.T, policyRoot, reposRoot, name string) {
	t.Helper()
	seedActiveRepo(t, policyRoot, reposRoot, name)
}

func TestTimeEstimates_update_writesSectionAndLogsEvent(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	teSeedRepo(t, policyRoot, reposRoot, "demo")

	h := timeEstimatesHandlers{policyRoot: policyRoot, token: "tok"}
	form := url.Values{
		"repo":   {"demo"},
		"tier-1": {"5.5"},
		"tier-2": {""}, // blank → revert to default
		"tier-3": {"0.75"},
		"tier-4": {""},
		"tier-5": {""},
		"tier-6": {""},
	}
	w := httptest.NewRecorder()
	h.update(w, rlPost(t, "/policy/repo/time-estimates", form, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d want 303; body=%s", w.Code, w.Body.String())
	}

	// File contents include the [time-estimates] section with only the set tiers.
	got, err := os.ReadFile(filepath.Join(policyRoot, "_repos", "demo", "appframes.toml"))
	if err != nil {
		t.Fatalf("read appframes.toml: %v", err)
	}
	s := string(got)
	if !strings.Contains(s, "[time-estimates]") {
		t.Errorf("missing [time-estimates] section:\n%s", s)
	}
	if !strings.Contains(s, "tier-1 = 5.5") {
		t.Errorf("tier-1 not written:\n%s", s)
	}
	if !strings.Contains(s, "tier-3 = 0.75") {
		t.Errorf("tier-3 not written:\n%s", s)
	}
	for _, blank := range []string{"tier-2 ", "tier-4 ", "tier-5 ", "tier-6 "} {
		if strings.Contains(s, blank) {
			t.Errorf("blank field %s should not be emitted:\n%s", blank, s)
		}
	}

	// Loadback via the same path the stats page uses returns the operator values.
	te, err := gateway.LoadTimeEstimates(policyRoot, "demo")
	if err != nil {
		t.Fatalf("LoadTimeEstimates: %v", err)
	}
	if te.Tier1 == nil || *te.Tier1 != 5.5 {
		t.Errorf("Tier1: got %v, want 5.5", te.Tier1)
	}
	if te.Tier3 == nil || *te.Tier3 != 0.75 {
		t.Errorf("Tier3: got %v, want 0.75", te.Tier3)
	}
	if te.Tier2 != nil || te.Tier4 != nil || te.Tier5 != nil || te.Tier6 != nil {
		t.Errorf("blank tiers should stay nil; got Tier2=%v Tier4=%v Tier5=%v Tier6=%v",
			te.Tier2, te.Tier4, te.Tier5, te.Tier6)
	}

	// Audit event recorded so /events surfaces the change.
	events, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool {
		return e.Event == "time-estimates-update"
	})
	if len(events) != 1 {
		t.Fatalf("event: got %d, want 1; %+v", len(events), events)
	}
	if events[0].Repo != "demo" || !events[0].OK {
		t.Errorf("event payload off: %+v", events[0])
	}
	if v, ok := events[0].Payload["tier-1"].(float64); !ok || v != 5.5 {
		t.Errorf("event tier-1 payload missing/wrong: %+v", events[0].Payload)
	}
}

func TestTimeEstimates_update_rejectsBadCSRF(t *testing.T) {
	tmp := t.TempDir()
	h := timeEstimatesHandlers{policyRoot: filepath.Join(tmp, "policy"), token: "tok"}
	w := httptest.NewRecorder()
	h.update(w, rlPost(t, "/policy/repo/time-estimates", url.Values{"repo": {"x"}}, "wrong"))
	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d want 403", w.Code)
	}
}

func TestTimeEstimates_update_rejectsNonPost(t *testing.T) {
	h := timeEstimatesHandlers{token: "tok"}
	w := httptest.NewRecorder()
	h.update(w, httptest.NewRequest("GET", "/policy/repo/time-estimates", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d want 405", w.Code)
	}
}

func TestTimeEstimates_update_rejectsOutOfRange(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	teSeedRepo(t, policyRoot, reposRoot, "demo")
	h := timeEstimatesHandlers{policyRoot: policyRoot, token: "tok"}

	for _, bad := range []string{"-1", "999", "abc"} {
		t.Run(bad, func(t *testing.T) {
			form := url.Values{"repo": {"demo"}, "tier-1": {bad}}
			w := httptest.NewRecorder()
			h.update(w, rlPost(t, "/policy/repo/time-estimates", form, "tok"))
			if w.Code != http.StatusBadRequest {
				t.Errorf("input %q: got %d want 400", bad, w.Code)
			}
		})
	}
}

func TestTimeEstimates_update_rejectsUnknownRepo(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	_ = os.MkdirAll(policyRoot, 0o755)
	h := timeEstimatesHandlers{policyRoot: policyRoot, token: "tok"}
	form := url.Values{"repo": {"unregistered"}, "tier-1": {"4"}}
	w := httptest.NewRecorder()
	h.update(w, rlPost(t, "/policy/repo/time-estimates", form, "tok"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d want 400", w.Code)
	}
}

func TestTimeEstimates_update_preservesOtherSections(t *testing.T) {
	// Set up a repo with an existing [frames] enabled list + a severity override,
	// then write time-estimates. The frames section must survive untouched -
	// otherwise the gate's enabled-frames list would silently get clobbered.
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	teSeedRepo(t, policyRoot, reposRoot, "demo")

	// Seed frames policy
	fp := gateway.FramePolicy{
		Enabled:  []string{"@tier-1", "@web"},
		Severity: map[string]string{"security/no-hardcoded-credentials": "WARN"},
	}
	if err := fp.Save(policyRoot, "demo"); err != nil {
		t.Fatal(err)
	}

	// Save time-estimates
	h := timeEstimatesHandlers{policyRoot: policyRoot, token: "tok"}
	w := httptest.NewRecorder()
	h.update(w, rlPost(t, "/policy/repo/time-estimates",
		url.Values{"repo": {"demo"}, "tier-1": {"6"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status: got %d want 303", w.Code)
	}

	// Frames + severity overrides survived
	fp2, err := gateway.LoadFramePolicy(policyRoot, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(fp2.Enabled) != 2 || fp2.Enabled[0] != "@tier-1" || fp2.Enabled[1] != "@web" {
		t.Errorf("frames.enabled got clobbered: %+v", fp2.Enabled)
	}
	if fp2.Severity["security/no-hardcoded-credentials"] != "WARN" {
		t.Errorf("severity override lost: %+v", fp2.Severity)
	}
}
