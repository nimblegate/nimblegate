// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

// seedPolicyRoot writes one audit.log with 3 records (2 accepts, 1 reject with
// a BLOCK finding) for repo "demo", so the service's lazy ingest has data.
func seedPolicyRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	repoDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(repoDir, "audit.log")
	now := time.Now().UTC()
	recs := []gateway.AuditRecord{
		{Time: now.Add(-2 * time.Hour), Repo: "demo", Refs: []string{"refs/heads/main"}, Accept: true},
		{Time: now.Add(-1 * time.Hour), Repo: "demo", Refs: []string{"refs/heads/main"}, Accept: false,
			Findings: []gateway.Finding{{ID: "security/no-private-keys-in-repo", Severity: "BLOCK", Message: "key found"}}},
		{Time: now, Repo: "demo", Refs: []string{"refs/heads/main"}, Accept: true},
	}
	for _, r := range recs {
		if err := gateway.AppendAudit(logPath, r); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testService(t *testing.T, root string) *Service {
	t.Helper()
	return &Service{
		PolicyRoot: root,
		Verify:     func(string) (bool, error) { return true, nil },
	}
}

func TestGateStatsText(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	out, err := svc.GateStats(Params{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	txt := out.Text
	if !strings.Contains(txt, "last 30 days") || !strings.Contains(txt, "decisions: 3") ||
		!strings.Contains(txt, "accepted: 2") || !strings.Contains(txt, "rejected: 1") {
		t.Errorf("stats text wrong:\n%s", txt)
	}
	if !strings.Contains(txt, "security/no-private-keys-in-repo") {
		t.Errorf("top frames missing:\n%s", txt)
	}
}

func TestObserveModeBanner(t *testing.T) {
	root := seedPolicyRoot(t)
	svc := testService(t, root)
	tomlPath := filepath.Join(root, "demo", "gateway.toml")

	// No gateway.toml → enforce assumed → no banner.
	out, err := svc.GateStats(Params{Days: 30, Repo: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "OBSERVE MODE") {
		t.Errorf("banner shown without observe=true:\n%s", out.Text)
	}

	// observe = true → banner appears (read fresh from the toml each call).
	if err := os.WriteFile(tomlPath, []byte("observe = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = svc.GateStats(Params{Days: 30, Repo: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "OBSERVE MODE") || !strings.Contains(out.Text, "NEVER blocks") {
		t.Errorf("observe banner missing for observe=true repo:\n%s", out.Text)
	}

	// observe = false → no banner again.
	if err := os.WriteFile(tomlPath, []byte("observe = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = svc.GateStats(Params{Days: 30, Repo: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "OBSERVE MODE") {
		t.Errorf("banner shown with observe=false:\n%s", out.Text)
	}
}

func TestBounceRateAndUnknownRepoNote(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	out, err := svc.BounceRate(Params{Days: 30, MinPushes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "demo") || !strings.Contains(out.Text, "33%") {
		t.Errorf("bounce text wrong:\n%s", out.Text)
	}
	out2, err := svc.GateStats(Params{Repo: "nope", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2.Text, `repo "nope" not found`) || !strings.Contains(out2.Text, "demo") {
		t.Errorf("unknown repo must carry recovery note:\n%s", out2.Text)
	}
}

func TestDecisionsReceipts(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	out, err := svc.Decisions(Params{Days: 30, Result: "rejected", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "REJECTED") || !strings.Contains(out.Text, "demo") ||
		!strings.Contains(out.Text, "security/no-private-keys-in-repo (BLOCK)") {
		t.Errorf("receipts wrong:\n%s", out.Text)
	}
}

func TestParamsClamp(t *testing.T) {
	p := Params{Days: -5, Limit: 999, MinPushes: 0}
	notes := p.clamp()
	if p.Days != 30 || p.Limit != 50 || p.MinPushes != 5 {
		t.Errorf("clamp wrong: %+v", p)
	}
	if len(notes) == 0 {
		t.Error("clamping must be noted")
	}
}

func TestParamsClampMaxLimit(t *testing.T) {
	// Dashboard raises the ceiling via MaxLimit (here 500) - agents never set it.
	p := Params{Limit: 999, MaxLimit: 500}
	p.clamp()
	if p.Limit != 500 {
		t.Errorf("MaxLimit ceiling not honored: got %d, want 500", p.Limit)
	}
	// MaxLimit above the hard backstop is itself capped at 500.
	q := Params{Limit: 999, MaxLimit: 100000}
	q.clamp()
	if q.Limit != 500 {
		t.Errorf("backstop not enforced: got %d, want 500", q.Limit)
	}
	// A request within the raised ceiling is left untouched.
	r := Params{Limit: 250, MaxLimit: 500}
	r.clamp()
	if r.Limit != 250 {
		t.Errorf("limit within ceiling changed: got %d, want 250", r.Limit)
	}
}

func TestDecisionsTruncationNote(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t)) // 3 decisions seeded
	// Limit below the total → result hits the cap → truncation note.
	out, err := svc.Decisions(Params{Days: 30, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "row cap") {
		t.Errorf("expected truncation note when results hit the limit:\n%s", out.Text)
	}
	// Limit above the total → nothing hidden → no note.
	out2, err := svc.Decisions(Params{Days: 30, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2.Text, "row cap") {
		t.Errorf("no truncation note expected below the limit:\n%s", out2.Text)
	}
}

func TestRateGuard(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	for i := 0; i < rateLimitPerMin; i++ {
		if !svc.allow("tok") {
			t.Fatalf("request %d should be allowed", i)
		}
	}
	if svc.allow("tok") {
		t.Error("request over the per-minute budget must be rejected")
	}
	if !svc.allow("other") {
		t.Error("budget is per token")
	}
}

func TestTimeSaved(t *testing.T) {
	root := t.TempDir()
	repoDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	recs := []gateway.AuditRecord{
		{Time: now.Add(-1 * time.Hour), Repo: "demo", Refs: []string{"refs/heads/main"}, Accept: false,
			Findings: []gateway.Finding{{ID: "security/no-private-keys-in-repo", Severity: "BLOCK", Message: "k.pem:1"}}},
	}
	for _, r := range recs {
		if err := gateway.AppendAudit(filepath.Join(repoDir, "audit.log"), r); err != nil {
			t.Fatal(err)
		}
	}
	svc := testService(t, root)
	out, err := svc.TimeSaved(Params{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "actually prevented") || !strings.Contains(out.Text, "4.0h") {
		t.Errorf("time-saved text wrong:\n%s", out.Text)
	}
	if !strings.Contains(out.Text, "security/no-private-keys-in-repo") {
		t.Errorf("per-frame breakdown missing:\n%s", out.Text)
	}
}
