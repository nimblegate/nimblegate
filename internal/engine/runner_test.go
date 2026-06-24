// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"sort"
	"testing"
	"time"

	"nimblegate/internal/frames"
)

func TestRunner_RunsAllMatchingFramesAndCollectsResults(t *testing.T) {
	r := NewRegistry()

	passing := makeFrame(frames.CategoryGitSafety, "passing", []string{"cli"})
	_ = r.Add(passing, func(ctx CheckContext) CheckResult {
		return CheckResult{FrameID: passing.ID(), Category: passing.Frontmatter.Category, Outcome: OutcomePass, Timestamp: time.Now()}
	})

	blocking := makeFrame(frames.CategorySecurity, "blocking", []string{"cli"})
	_ = r.Add(blocking, func(ctx CheckContext) CheckResult {
		return CheckResult{FrameID: blocking.ID(), Category: blocking.Frontmatter.Category, Outcome: OutcomeBlock, Reason: "bad", Timestamp: time.Now()}
	})

	skipFrame := makeFrame(frames.CategoryDocumentation, "skipped", []string{"git-wrap"})
	_ = r.Add(skipFrame, func(ctx CheckContext) CheckResult {
		t.Fatal("must not be called")
		return CheckResult{}
	})

	results := Run(r, CheckContext{Trigger: TriggerCLI})

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	sort.Slice(results, func(i, j int) bool { return results[i].FrameID < results[j].FrameID })
	if results[0].FrameID != "git/passing" || results[0].Outcome != OutcomePass {
		t.Errorf("results[0] = %+v", results[0])
	}
	if results[1].FrameID != "security/blocking" || results[1].Outcome != OutcomeBlock {
		t.Errorf("results[1] = %+v", results[1])
	}
}

func TestRunner_NilCheckFuncProducesError(t *testing.T) {
	r := NewRegistry()
	_ = r.Add(makeFrame(frames.CategorySecurity, "nilcheck", []string{"cli"}), nil)
	results := Run(r, CheckContext{Trigger: TriggerCLI})
	if len(results) != 1 || results[0].Outcome != OutcomeError {
		t.Fatalf("expected one ERROR result, got %+v", results)
	}
}
