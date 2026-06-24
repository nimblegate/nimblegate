// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

type fakeChecker struct {
	results    []engine.CheckResult
	suppressed []engine.SuppressionLog
}

func (f fakeChecker) Check(treeDir string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	return f.results, f.suppressed, nil
}

type errChecker struct{}

func (errChecker) Check(string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	return nil, nil, &checkErr{}
}

type checkErr struct{}

func (*checkErr) Error() string { return "boom" }

func TestRunPreReceive_blockRejects(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	deps := PreReceiveDeps{
		Policy:    Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir:    bare,
		Checker:   fakeChecker{results: []engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}}},
		AuditPath: t.TempDir() + "/a.log",
	}
	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code == 0 {
		t.Error("BLOCK must reject (non-zero exit)")
	}
	if !strings.Contains(out.String(), "security/x") {
		t.Errorf("reject output should explain why:\n%s", out.String())
	}
}

func TestRunPreReceive_cleanAccepts(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	deps := PreReceiveDeps{
		Policy:    Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir:    bare,
		Checker:   fakeChecker{results: nil},
		AuditPath: t.TempDir() + "/a.log",
	}
	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 0 {
		t.Errorf("clean push must accept (exit 0), got %d", code)
	}
}

func TestRunPreReceive_observeRelaysWouldBlock(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	auditPath := t.TempDir() + "/a.log"
	deps := PreReceiveDeps{
		Policy:    Policy{Repo: "demo", Enabled: true, Observe: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir:    bare,
		Checker:   fakeChecker{results: []engine.CheckResult{{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "boom"}}},
		AuditPath: auditPath,
	}
	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPreReceive(deps, stdin, &out); code != 0 {
		t.Fatalf("observe mode must relay a would-block (exit 0), got %d\n%s", code, out.String())
	}
	if out.Len() != 0 {
		t.Errorf("observe mode must be silent to the client (would-block goes to audit only):\n%s", out.String())
	}
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var rec AuditRecord
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatalf("decode audit %q: %v", b, err)
	}
	if !rec.Accept {
		t.Error("observe-relayed push should be recorded Accept=true (it was relayed)")
	}
	if !rec.Observed {
		t.Error("observe-relayed would-block should be recorded Observed=true")
	}
	if len(rec.Findings) == 0 || rec.Findings[0].ID != "security/x" {
		t.Errorf("findings must still be recorded in observe mode: %+v", rec.Findings)
	}
}

func TestRunPreReceive_checkErrorFailsClosed(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	deps := PreReceiveDeps{
		Policy:    Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir:    bare,
		Checker:   errChecker{},
		AuditPath: t.TempDir() + "/a.log",
	}
	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code == 0 {
		t.Error("a checker error must fail closed (reject)")
	}
}

func TestRunPreReceive_recordsSuppressions(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	auditPath := t.TempDir() + "/a.log"
	deps := PreReceiveDeps{
		Policy: Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir: bare,
		Checker: fakeChecker{
			results:    nil, // nothing left to block after suppression
			suppressed: []engine.SuppressionLog{{FrameID: "security/no-private-keys-in-repo", File: "internal/x_test.go", Label: "PEM key"}},
		},
		AuditPath: auditPath,
	}
	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	if code := RunPreReceive(deps, stdin, new(bytes.Buffer)); code != 0 {
		t.Fatalf("exit = %d, want 0 (nothing blocks)", code)
	}
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	var rec AuditRecord
	if err := json.Unmarshal(bytes.TrimSpace(b), &rec); err != nil {
		t.Fatalf("decode audit %q: %v", b, err)
	}
	if len(rec.Suppressed) != 1 || rec.Suppressed[0].Frame != "security/no-private-keys-in-repo" || rec.Suppressed[0].File != "internal/x_test.go" {
		t.Errorf("Suppressed = %+v, want one no-private-keys entry", rec.Suppressed)
	}
}

// A relay failure must be SILENT to the pusher (it would reveal the gateway/
// relay); the push already succeeded from the client's view. Recorded
// operator-side instead.
func TestRunPostReceive_relayFailureSilentToPusher(t *testing.T) {
	d := PostReceiveDeps{
		Policy: Policy{UpstreamURL: "file:///nonexistent-" + t.Name() + ".git"},
		GitDir: t.TempDir(), // not a real git dir - relay will fail
	}
	stdin := strings.NewReader("abc123 def456 refs/heads/main\n")
	var out bytes.Buffer
	if code := RunPostReceive(d, stdin, &out); code != 0 {
		t.Errorf("relay failure must not fail the push to the pusher, got code %d", code)
	}
	low := strings.ToLower(out.String())
	for _, leak := range []string{"warning", "relay", "upstream", "gateway", "out-of-sync"} {
		if strings.Contains(low, leak) {
			t.Errorf("pusher output leaked %q:\n%s", leak, out.String())
		}
	}
}

func TestParseRefLines(t *testing.T) {
	in := strings.NewReader("a b refs/heads/x\n\n bad line with five fields here\nc d refs/heads/y\n")
	got, err := parseRefLines(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 well-formed refs (blank + wrong-field lines skipped), got %d: %+v", len(got), got)
	}
	if got[0].Name != "refs/heads/x" || got[0].OldRev != "a" || got[0].NewRev != "b" {
		t.Errorf("first ref parsed wrong: %+v", got[0])
	}
}
