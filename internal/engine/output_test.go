// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"bytes"
	"strings"
	"testing"

	"nimblegate/internal/frames"
)

func TestFormatResults_OrdersByCategoryPriority(t *testing.T) {
	results := []CheckResult{
		{FrameID: "convention/x", Category: frames.CategoryDocumentation, Outcome: OutcomeWarn, Reason: "convention"},
		{FrameID: "git-safety/y", Category: frames.CategoryGitSafety, Outcome: OutcomeBlock, Reason: "destructive"},
		{FrameID: "security/z", Category: frames.CategorySecurity, Outcome: OutcomeBlock, Reason: "xss"},
	}
	var buf bytes.Buffer
	exitCode := FormatResults(&buf, results)

	out := buf.String()
	posGit := strings.Index(out, "git-safety/y")
	posSec := strings.Index(out, "security/z")
	posCon := strings.Index(out, "convention/x")
	if !(posGit < posSec && posSec < posCon) {
		t.Errorf("output order wrong; got positions git=%d sec=%d conv=%d", posGit, posSec, posCon)
	}
	if exitCode != 1 {
		t.Errorf("exitCode = %d, want 1 (any BLOCK present)", exitCode)
	}
}

func TestFormatResults_AllPassReturnsZero(t *testing.T) {
	results := []CheckResult{
		{FrameID: "git-safety/y", Category: frames.CategoryGitSafety, Outcome: OutcomePass},
	}
	var buf bytes.Buffer
	exitCode := FormatResults(&buf, results)
	if exitCode != 0 {
		t.Errorf("exitCode = %d, want 0", exitCode)
	}
	if !strings.Contains(buf.String(), "✓ nimblegate: 1 frame(s) passed.") {
		t.Errorf("expected positive confirmation line on all-pass; got:\n%s", buf.String())
	}
}

// TestFormatResults_MixedNoConfirmationLine - when some non-pass result is
// present, suppress the ✓ line (it would lie).
func TestFormatResults_MixedNoConfirmationLine(t *testing.T) {
	results := []CheckResult{
		{FrameID: "git-safety/y", Category: frames.CategoryGitSafety, Outcome: OutcomePass},
		{FrameID: "convention/x", Category: frames.CategoryDocumentation, Outcome: OutcomeWarn, Reason: "drift"},
	}
	var buf bytes.Buffer
	_ = FormatResults(&buf, results)
	if strings.Contains(buf.String(), "✓ nimblegate:") {
		t.Errorf("✓ line must not appear when warnings/blocks/errors are present; got:\n%s", buf.String())
	}
}

func TestFormatResults_WarnsOnlyReturnsZero(t *testing.T) {
	results := []CheckResult{
		{FrameID: "convention/x", Category: frames.CategoryDocumentation, Outcome: OutcomeWarn, Reason: "warn"},
	}
	var buf bytes.Buffer
	exitCode := FormatResults(&buf, results)
	if exitCode != 0 {
		t.Errorf("WARN-only exit = %d, want 0 (only BLOCK fails the gate)", exitCode)
	}
}
