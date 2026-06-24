// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// runCredCheck is a small helper to keep table-driven cases concise.
func runCredCheck(t *testing.T, fileBody string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, "src/test.go", fileBody)
	return NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoHardcodedCredentials_AWSAccessKey(t *testing.T) {
	got := runCredCheck(t, `const KEY = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "AWS access key") {
		t.Errorf("reason missing pattern name: %s", got.Reason)
	}
	// CRITICAL: the raw matched bytes must NEVER appear in the reason.
	if strings.Contains(got.Reason, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("REDACTION FAILURE: raw secret leaked into reason: %s", got.Reason)
	}
}

func TestNoHardcodedCredentials_GitHubPAT(t *testing.T) {
	got := runCredCheck(t, `token := "ghp_1234567890abcdefghijklmnopqrstuvwxyz"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "GitHub personal access token") {
		t.Errorf("reason missing pattern: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "ghp_1234567890") {
		t.Errorf("REDACTION FAILURE: raw token in reason: %s", got.Reason)
	}
}

func TestNoHardcodedCredentials_GitHubFineGrainedPAT(t *testing.T) {
	// Fine-grained PATs are 93 chars: "github_pat_" (11) + 82 of [A-Za-z0-9_].
	secret := "github_pat_" + strings.Repeat("A", 82)
	got := runCredCheck(t, `token := "`+secret+`"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "fine-grained") {
		t.Errorf("reason should identify fine-grained PAT: %s", got.Reason)
	}
}

func TestNoHardcodedCredentials_GitHubServerToken(t *testing.T) {
	secret := "ghs_" + strings.Repeat("a", 36)
	got := runCredCheck(t, `t = "`+secret+`"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoHardcodedCredentials_StripeLiveSecretBlocks(t *testing.T) {
	got := runCredCheck(t, `stripe := "sk_live_abcdefghijklmnopqrstuvwxyz"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "Stripe secret key (live)") {
		t.Errorf("reason missing live-specific name: %s", got.Reason)
	}
}

func TestNoHardcodedCredentials_StripeTestSecretBlocks(t *testing.T) {
	got := runCredCheck(t, `stripe := "sk_test_abcdefghijklmnopqrstuvwxyz"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
	}
}

// TestNoHardcodedCredentials_StripePublishableInfoOnly - pk_live_ is
// intentionally public, so it doesn't BLOCK the commit. It IS catalogued
// at INFO severity so the user can audit where their public keys appear
// (and catch a swapped pk_live_/pk_test_ in a fixture).
func TestNoHardcodedCredentials_StripePublishableInfoOnly(t *testing.T) {
	got := runCredCheck(t, `publishable := "pk_live_abcdefghijklmnopqrstuvwxyz"`+"\n")
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s, want INFO - pk_live_* catalogued not blocked; reason: %s",
			got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "publishable keys catalogued") {
		t.Errorf("reason missing 'publishable keys catalogued' label: %s", got.Reason)
	}
	// INFO path should NOT use rotation-urgency language.
	if strings.Contains(got.Reason, "ROTATE") || strings.Contains(got.Fix, "ROTATE") {
		t.Errorf("INFO path should not say ROTATE; reason: %s; fix: %s", got.Reason, got.Fix)
	}
	// Raw bytes still redacted.
	if strings.Contains(got.Reason, "pk_live_abcdefg") {
		t.Errorf("REDACTION FAILURE: raw publishable key in reason: %s", got.Reason)
	}
}

func TestNoHardcodedCredentials_StripePublishableTestInfoOnly(t *testing.T) {
	got := runCredCheck(t, `pk := "pk_test_abcdefghijklmnopqrstuvwxyz"`+"\n")
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s, want INFO", got.Outcome)
	}
	if !strings.Contains(got.Reason, "Stripe publishable key (test)") {
		t.Errorf("reason missing test-specific name: %s", got.Reason)
	}
}

// TestNoHardcodedCredentials_BlockWinsOverInfo - when BOTH a real
// credential AND a publishable key appear, BLOCK takes precedence and
// the reason includes both.
func TestNoHardcodedCredentials_BlockWinsOverInfo(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/config.go",
		`const SECRET = "sk_live_abcdefghijklmnopqrstuvwxyz"
const PUB = "pk_live_abcdefghijklmnopqrstuvwxyz"
`)
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK (BLOCK + INFO → BLOCK wins)", got.Outcome)
	}
	if !strings.Contains(got.Reason, "Stripe secret key (live)") {
		t.Errorf("reason missing BLOCK pattern: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "Stripe publishable key (live)") {
		t.Errorf("reason should also surface the INFO finding when BLOCK is present: %s", got.Reason)
	}
	if !strings.Contains(got.Fix, "ROTATE") {
		t.Errorf("BLOCK path must keep rotation-urgency fix language: %s", got.Fix)
	}
}

func TestNoHardcodedCredentials_SlackToken(t *testing.T) {
	got := runCredCheck(t, `slack := "xoxb-12345678901-1234567890123-AbCdEfGhIjKlMnOpQrStUvWx"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoHardcodedCredentials_GoogleAPIKey(t *testing.T) {
	got := runCredCheck(t, `g := "AIza`+strings.Repeat("a", 35)+`"`+"\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
	}
}

// TestNoHardcodedCredentials_LookalikesNotMatched - bytes that resemble
// real tokens but don't fit the documented length/charset must not fire.
func TestNoHardcodedCredentials_LookalikesNotMatched(t *testing.T) {
	cases := []string{
		// AWS-shape but too short
		`KEY = "AKIASHORT"`,
		// AWS-shape but lowercase in the prefix
		`KEY = "akiaIOSFODNN7EXAMPLE"`,
		// GitHub-style word but unknown prefix
		`KEY = "gp_1234567890abcdefghijklmnopqrstuvwxyzAB"`,
		// Stripe with too few chars after prefix
		`KEY = "sk_live_short"`,
		// Sentence containing "AKIA" but not as a token boundary
		`// This was discussed at AKIA last year.`,
	}
	for _, body := range cases {
		got := runCredCheck(t, body+"\n")
		if got.Outcome != engine.OutcomePass {
			t.Errorf("false positive on lookalike %q: outcome=%s reason=%s",
				body, got.Outcome, got.Reason)
		}
	}
}

func TestNoHardcodedCredentials_PerFileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/quiet.go",
		`# appframes:disable security/no-hardcoded-credentials
const K = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-file disable)", got.Outcome)
	}
}

func TestNoHardcodedCredentials_PerLineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/quiet.go",
		`// appframes:disable-next-line security/no-hardcoded-credentials
const FAKE = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-line disable)", got.Outcome)
	}
}

// TestNoHardcodedCredentials_NoiseDirsExcluded - vendored secrets in
// node_modules/ are someone else's problem; we don't scan them.
func TestNoHardcodedCredentials_NoiseDirsExcluded(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "node_modules/dep/leak.js",
		`const K = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded)", got.Outcome)
	}
}

// TestNoHardcodedCredentials_PreCommitEmptyChangesPasses - file-scan scope.
func TestNoHardcodedCredentials_PreCommitEmptyChangesPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/leak.go", `const K = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit + empty stage", got.Outcome)
	}
}

// TestNoHardcodedCredentials_PreCommitStagedScansThoseOnly
func TestNoHardcodedCredentials_PreCommitStagedScansThoseOnly(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/staged.go", `const K = "AKIAIOSFODNN7EXAMPLE";`+"\n")
	writeSource(t, root, "src/untouched.go", `const K2 = "ghp_1234567890abcdefghijklmnopqrstuvwxyz";`+"\n")
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/staged.go")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "src/staged.go") {
		t.Errorf("missing staged file: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "src/untouched.go") {
		t.Errorf("untouched file leaked into staged scan: %s", got.Reason)
	}
}

// TestNoHardcodedCredentials_LargeFileSkipped - over 1MB → assumed binary,
// not scanned.
func TestNoHardcodedCredentials_LargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a >1MB file with a real-shaped secret inside.
	big := strings.Repeat("x", 2*1024*1024) + "\nAKIAIOSFODNN7EXAMPLE\n"
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoHardcodedCredentials(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (>1MB file should be skipped)", got.Outcome)
	}
}

// TestNoHardcodedCredentials_HitCap - many leaks in one file get capped.
func TestNoHardcodedCredentials_HitCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString(`const K = "AKIAIOSFODNN7EXAMPLE`)
		// Pad to keep the 16-after-AKIA char-class valid across iterations.
		b.WriteString(`";`)
		b.WriteString("\n")
	}
	got := runCredCheck(t, b.String())
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	// Cap is 10; "-" appears once per hit.
	hits := strings.Count(got.Reason, " - ")
	if hits != 10 {
		t.Errorf("expected exactly 10 hits (cap), got %d in reason: %s", hits, got.Reason)
	}
}
