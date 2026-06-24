// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"testing"

	"nimblegate/internal/engine"
)

func TestFinalizeRun(t *testing.T) {
	// go vet exits non-zero with no parseable findings → it couldn't build
	// the packages. A "no findings" PASS would be a lie; surface a WARN.
	clean := engine.CheckResult{Outcome: engine.OutcomePass, Reason: "go vet: no findings"}
	got := finalizeRun(clean, true)
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("non-zero exit + no hits: Outcome = %q, want WARN", got.Outcome)
	}

	// Non-zero exit WITH findings is the normal "vet found issues" case -
	// leave the parsed result untouched.
	withHits := engine.CheckResult{Outcome: engine.OutcomeBlock, Hits: []engine.Hit{{File: "a.go", Line: 1, Label: "x"}}}
	if got := finalizeRun(withHits, true); got.Outcome != engine.OutcomeBlock {
		t.Errorf("non-zero exit + hits: Outcome = %q, want BLOCK (unchanged)", got.Outcome)
	}

	// Clean exit stays PASS.
	if got := finalizeRun(clean, false); got.Outcome != engine.OutcomePass {
		t.Errorf("clean exit: Outcome = %q, want PASS (unchanged)", got.Outcome)
	}
}

func TestResolveOutcome(t *testing.T) {
	cases := []struct {
		severity string
		want     engine.CheckOutcome
	}{
		{"block", engine.OutcomeBlock},
		{"warn", engine.OutcomeWarn},
		{"", engine.OutcomeBlock},      // default is block
		{"Block", engine.OutcomeBlock}, // case-insensitive
		{"WARN", engine.OutcomeWarn},
		{"nonsense", engine.OutcomeBlock}, // unknown falls back to block
	}
	for _, c := range cases {
		if got := resolveOutcome(c.severity); got != c.want {
			t.Errorf("resolveOutcome(%q) = %q, want %q", c.severity, got, c.want)
		}
	}
}
