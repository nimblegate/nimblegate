// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package selection

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// stubCheck returns a CheckFunc that scans the changed-files content for
// substring `marker`. Returns BLOCK if any file matches, PASS otherwise.
// Used to make the runner's positive/negative bookkeeping testable
// without binding to a real stdlib check.
func stubCheck(marker string) engine.CheckFunc {
	return func(ctx engine.CheckContext) engine.CheckResult {
		for _, p := range ctx.ChangedFiles {
			data, err := os.ReadFile(p)
			if err != nil {
				return engine.CheckResult{
					FrameID: "test/stub",
					Outcome: engine.OutcomeError,
					Reason:  err.Error(),
				}
			}
			if strings.Contains(string(data), marker) {
				return engine.CheckResult{
					FrameID:  "test/stub",
					Category: frames.CategoryDocumentation,
					Outcome:  engine.OutcomeBlock,
					Reason:   "matched marker",
				}
			}
		}
		return engine.CheckResult{
			FrameID:  "test/stub",
			Category: frames.CategoryDocumentation,
			Outcome:  engine.OutcomePass,
		}
	}
}

func TestRun_AllPositivesAndNegativesPass(t *testing.T) {
	mfs := fstest.MapFS{
		"positives/case-1.txt":  {Data: []byte("contains FORBIDDEN word")},
		"positives/case-2.txt":  {Data: []byte("also FORBIDDEN in here")},
		"negatives/clean-1.txt": {Data: []byte("clean content")},
		"negatives/clean-2.txt": {Data: []byte("nothing bad")},
	}
	result, err := Run("test/stub", stubCheck("FORBIDDEN"), mfs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Grade != "passing" {
		t.Errorf("Grade: got %q, want %q", result.Grade, "passing")
	}
	if result.PositivesPassed != 2 || result.PositivesTotal != 2 {
		t.Errorf("Positives: got %d/%d, want 2/2", result.PositivesPassed, result.PositivesTotal)
	}
	if result.NegativesPassed != 2 || result.NegativesTotal != 2 {
		t.Errorf("Negatives: got %d/%d, want 2/2", result.NegativesPassed, result.NegativesTotal)
	}
}

func TestRun_FailingOnFalseNegative(t *testing.T) {
	mfs := fstest.MapFS{
		"positives/case-1.txt":  {Data: []byte("clean content")}, // should fire but won't
		"negatives/clean-1.txt": {Data: []byte("clean content")},
	}
	result, err := Run("test/stub", stubCheck("FORBIDDEN"), mfs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Grade != "failing" {
		t.Errorf("Grade: got %q, want %q (positive didn't fire)", result.Grade, "failing")
	}
	if result.PositivesPassed != 0 {
		t.Errorf("PositivesPassed: got %d, want 0", result.PositivesPassed)
	}
}

func TestRun_FailingOnFalsePositive(t *testing.T) {
	mfs := fstest.MapFS{
		"positives/case-1.txt": {Data: []byte("contains FORBIDDEN word")},
		"negatives/case-1.txt": {Data: []byte("also contains FORBIDDEN")},
	}
	result, err := Run("test/stub", stubCheck("FORBIDDEN"), mfs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Grade != "failing" {
		t.Errorf("Grade: got %q, want %q (negative fired)", result.Grade, "failing")
	}
	if result.NegativesPassed != 0 {
		t.Errorf("NegativesPassed: got %d, want 0", result.NegativesPassed)
	}
}

func TestRun_EmptyCorpusGivesPending(t *testing.T) {
	mfs := fstest.MapFS{}
	result, err := Run("test/stub", stubCheck("FORBIDDEN"), mfs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Grade != "pending" {
		t.Errorf("Grade: got %q, want %q", result.Grade, "pending")
	}
	if len(result.Cases) != 0 {
		t.Errorf("Cases: got %d, want 0", len(result.Cases))
	}
}

func TestRun_OnlyPositivesNoNegatives(t *testing.T) {
	mfs := fstest.MapFS{
		"positives/case-1.txt": {Data: []byte("contains FORBIDDEN word")},
	}
	result, err := Run("test/stub", stubCheck("FORBIDDEN"), mfs)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Grade != "passing" {
		t.Errorf("Grade: got %q, want %q", result.Grade, "passing")
	}
}
