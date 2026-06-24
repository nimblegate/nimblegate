// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
)

func frameWith(sev frames.Severity, source string) frames.Frame {
	return frames.Frame{Frontmatter: frames.Frontmatter{Severity: sev, SeveritySource: source}}
}

func TestApplySeverity_frontmatterIsAuthoritative(t *testing.T) {
	// CheckFunc fired BLOCK, but the frame's severity is WARN → WARN wins.
	if got := applySeverity(OutcomeBlock, frameWith(frames.SeverityWarn, "")); got != OutcomeWarn {
		t.Errorf("got %s, want WARN (frontmatter authoritative)", got)
	}
	// CheckFunc fired WARN, frame severity BLOCK → BLOCK wins.
	if got := applySeverity(OutcomeWarn, frameWith(frames.SeverityBlock, "")); got != OutcomeBlock {
		t.Errorf("got %s, want BLOCK", got)
	}
	// Frame severity INFO → INFO.
	if got := applySeverity(OutcomeBlock, frameWith(frames.SeverityInfo, "")); got != OutcomeInfo {
		t.Errorf("got %s, want INFO", got)
	}
}

func TestApplySeverity_passSkipErrorUntouched(t *testing.T) {
	f := frameWith(frames.SeverityBlock, "")
	for _, o := range []CheckOutcome{OutcomePass, OutcomeSkip, OutcomeError} {
		if got := applySeverity(o, f); got != o {
			t.Errorf("applySeverity(%s) = %s, want unchanged", o, got)
		}
	}
}

func TestApplySeverity_frameManagedOptsOut(t *testing.T) {
	// severity-source: frame → the CheckFunc's own outcome stands, so a
	// confidence-graded frame (credentials/keys) keeps its BLOCK-or-INFO call.
	f := frameWith(frames.SeverityBlock, "frame")
	if got := applySeverity(OutcomeInfo, f); got != OutcomeInfo {
		t.Errorf("got %s, want INFO (frame-managed severity preserved)", got)
	}
	if got := applySeverity(OutcomeBlock, f); got != OutcomeBlock {
		t.Errorf("got %s, want BLOCK (frame-managed severity preserved)", got)
	}
}

// TestApplyOverride_severityWinsOverSeveritySourceFrame is the regression test
// for the gateway policy-tuning fix: an explicit [frames.<id>] severity override
// must win even when the frame declares severity-source: frame. Before the fix,
// applyOverride left SeveritySource intact and applySeverity short-circuited on
// it, so the gate still BLOCKed while the UI showed the overridden WARN (the UI
// lied). The fix: applyOverride clears SeveritySource when a severity override is
// set. This test is load-bearing - it fails if the `SeveritySource = ""` line in
// applyOverride is removed.
func TestApplyOverride_severityWinsOverSeveritySourceFrame(t *testing.T) {
	base := frames.Frame{
		Frontmatter: frames.Frontmatter{
			Name:           "x",
			Category:       frames.CategorySecurity,
			Severity:       frames.SeverityBlock,
			SeveritySource: "frame",
		},
	}
	// Verify our base frame ID is what we expect.
	if base.ID() != "security/x" {
		t.Fatalf("unexpected frame ID %q", base.ID())
	}

	cfg := config.ProjectConfig{
		FrameOverrides: map[string]config.FrameOverride{
			"security/x": {Severity: "WARN"},
		},
	}

	// Override case: applyOverride must clear SeveritySource so that
	// applySeverity uses the overridden severity, not the frame's self-grade.
	overridden := applyOverride(base, cfg)
	if overridden.Frontmatter.Severity != frames.SeverityWarn {
		t.Errorf("applyOverride: Severity = %q, want WARN", overridden.Frontmatter.Severity)
	}
	if overridden.Frontmatter.SeveritySource != "" {
		t.Errorf("applyOverride: SeveritySource = %q, want \"\" (cleared so override wins)", overridden.Frontmatter.SeveritySource)
	}
	// The override is honored end-to-end: the CheckFunc fired BLOCK but the
	// project said WARN - applySeverity must yield WARN, not BLOCK.
	if got := applySeverity(OutcomeBlock, overridden); got != OutcomeWarn {
		t.Errorf("applySeverity after override: got %s, want WARN", got)
	}

	// Control case: no override → SeveritySource=="frame" is preserved and
	// the frame's self-grade stands (the CheckFunc outcome wins).
	noCfg := config.ProjectConfig{
		FrameOverrides: map[string]config.FrameOverride{},
	}
	noOverride := applyOverride(base, noCfg)
	if noOverride.Frontmatter.SeveritySource != "frame" {
		t.Errorf("applyOverride (no override): SeveritySource = %q, want \"frame\"", noOverride.Frontmatter.SeveritySource)
	}
	if got := applySeverity(OutcomeBlock, noOverride); got != OutcomeBlock {
		t.Errorf("applySeverity (no override): got %s, want BLOCK (self-grade intact)", got)
	}
}

func TestSeverityToOutcome(t *testing.T) {
	cases := map[frames.Severity]CheckOutcome{
		frames.SeverityBlock: OutcomeBlock,
		frames.SeverityWarn:  OutcomeWarn,
		frames.SeverityInfo:  OutcomeInfo,
		frames.Severity(""):  OutcomeBlock, // default/unknown → block (fail-safe)
	}
	for sev, want := range cases {
		if got := severityToOutcome(sev); got != want {
			t.Errorf("severityToOutcome(%q) = %s, want %s", sev, got, want)
		}
	}
}
