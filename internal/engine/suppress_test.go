// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/frames"
)

// stubSuppressor lets us inject deterministic match decisions in tests
// without depending on the whitelist package (would cause an import
// cycle).
type stubSuppressor struct {
	match map[string]bool // key: frameID + "|" + file + "|" + label
	calls []string        // ordered record of calls for assertion
}

func newStubSuppressor() *stubSuppressor {
	return &stubSuppressor{match: map[string]bool{}}
}

func (s *stubSuppressor) allow(frameID, file, label string) {
	s.match[frameID+"|"+file+"|"+label] = true
}

func (s *stubSuppressor) Match(frameID, file, label string) bool {
	s.calls = append(s.calls, frameID+"|"+file+"|"+label)
	return s.match[frameID+"|"+file+"|"+label]
}

func TestApplyWhitelist_NilSuppressorIsNoop(t *testing.T) {
	results := []CheckResult{
		{FrameID: "a/x", Outcome: OutcomeBlock, Reason: "bad", Hits: []Hit{{File: "f", Line: 1, Label: "L"}}},
	}
	out, log := ApplyWhitelist(results, nil, "/r")
	if len(out) != 1 || out[0].Outcome != OutcomeBlock {
		t.Errorf("nil suppressor should pass through unchanged; got %+v", out)
	}
	if log != nil {
		t.Errorf("nil suppressor should produce no log; got %v", log)
	}
}

func TestApplyWhitelist_ResultWithoutHitsPassesThrough(t *testing.T) {
	// Frame-level result (no Hits) cannot be suppressed via whitelist -
	// the whitelist mechanism is hit-scoped. Frame-level results need
	// severity overrides or appframes:disable markers.
	s := newStubSuppressor()
	results := []CheckResult{
		{FrameID: "git-safety/no-force-push-main", Outcome: OutcomeBlock, Reason: "force-push", Hits: nil},
	}
	out, log := ApplyWhitelist(results, s, "/r")
	if out[0].Outcome != OutcomeBlock {
		t.Error("result without Hits should pass through unchanged")
	}
	if len(log) != 0 {
		t.Errorf("no suppression log expected; got %v", log)
	}
}

func TestApplyWhitelist_AllHitsSuppressedDemotesToPass(t *testing.T) {
	root := "/repo"
	s := newStubSuppressor()
	s.allow("security/no-hardcoded-credentials", "vendor/lib.go", "AWS access key")
	s.allow("security/no-hardcoded-credentials", "vendor/lib.go", "Stripe live key")

	results := []CheckResult{
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "credentials detected: /repo/vendor/lib.go:1 - AWS access key; /repo/vendor/lib.go:2 - Stripe live key",
			Fix:      "rotate immediately",
			Hits: []Hit{
				{File: filepath.Join(root, "vendor/lib.go"), Line: 1, Label: "AWS access key"},
				{File: filepath.Join(root, "vendor/lib.go"), Line: 2, Label: "Stripe live key"},
			},
		},
	}
	out, log := ApplyWhitelist(results, s, root)

	if out[0].Outcome != OutcomePass {
		t.Errorf("all-suppressed should demote to PASS; got %s", out[0].Outcome)
	}
	if len(out[0].Hits) != 0 {
		t.Errorf("Hits should be cleared on full suppression; got %v", out[0].Hits)
	}
	if !strings.Contains(out[0].Reason, "suppressed by whitelist") {
		t.Errorf("Reason should note suppression; got %q", out[0].Reason)
	}
	if out[0].Fix != "" {
		t.Errorf("Fix should be cleared on PASS; got %q", out[0].Fix)
	}
	if len(log) != 2 {
		t.Errorf("SuppressionLog count = %d, want 2", len(log))
	}
}

func TestApplyWhitelist_PartialSuppressionRebuildsReason(t *testing.T) {
	root := "/repo"
	s := newStubSuppressor()
	// Whitelist only the vendor hit; the src hit remains.
	s.allow("security/no-hardcoded-credentials", "vendor/lib.go", "AWS access key")

	results := []CheckResult{
		{
			FrameID:  "security/no-hardcoded-credentials",
			Category: frames.CategorySecurity,
			Outcome:  OutcomeBlock,
			Reason:   "credentials detected: /repo/vendor/lib.go:1 - AWS access key; /repo/src/main.go:42 - Stripe live key",
			Fix:      "rotate immediately",
			Hits: []Hit{
				{File: filepath.Join(root, "vendor/lib.go"), Line: 1, Label: "AWS access key"},
				{File: filepath.Join(root, "src/main.go"), Line: 42, Label: "Stripe live key"},
			},
		},
	}
	out, log := ApplyWhitelist(results, s, root)

	if out[0].Outcome != OutcomeBlock {
		t.Errorf("partial suppression should keep Outcome; got %s", out[0].Outcome)
	}
	if len(out[0].Hits) != 1 {
		t.Fatalf("Hits after partial = %d, want 1", len(out[0].Hits))
	}
	if out[0].Hits[0].File != filepath.Join(root, "src/main.go") {
		t.Errorf("surviving hit file = %q", out[0].Hits[0].File)
	}
	// Rebuilt Reason should preserve the "credentials detected: " header
	// AND only mention the surviving hit (not the vendor one).
	if !strings.Contains(out[0].Reason, "credentials detected:") {
		t.Errorf("rebuilt Reason should keep header; got %q", out[0].Reason)
	}
	if !strings.Contains(out[0].Reason, "/repo/src/main.go:42") {
		t.Errorf("rebuilt Reason should keep surviving hit; got %q", out[0].Reason)
	}
	if strings.Contains(out[0].Reason, "vendor/lib.go") {
		t.Errorf("rebuilt Reason should NOT mention suppressed hit; got %q", out[0].Reason)
	}
	if len(log) != 1 {
		t.Errorf("SuppressionLog count = %d, want 1", len(log))
	}
}

func TestApplyWhitelist_InputNotMutated(t *testing.T) {
	root := "/repo"
	s := newStubSuppressor()
	s.allow("security/no-hardcoded-credentials", "vendor/lib.go", "AWS")
	original := []CheckResult{
		{
			FrameID: "security/no-hardcoded-credentials",
			Outcome: OutcomeBlock,
			Hits: []Hit{
				{File: filepath.Join(root, "vendor/lib.go"), Line: 1, Label: "AWS"},
			},
		},
	}
	_, _ = ApplyWhitelist(original, s, root)
	if original[0].Outcome != OutcomeBlock {
		t.Error("ApplyWhitelist mutated input Outcome")
	}
	if len(original[0].Hits) != 1 {
		t.Error("ApplyWhitelist mutated input Hits")
	}
}
