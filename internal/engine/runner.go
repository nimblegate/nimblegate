// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"sync"
	"time"

	"nimblegate/internal/frames"
)

// severityToOutcome maps a frame's declared severity to a gate outcome.
// Unknown/empty severity fails safe to BLOCK.
func severityToOutcome(s frames.Severity) CheckOutcome {
	switch s {
	case frames.SeverityWarn:
		return OutcomeWarn
	case frames.SeverityInfo:
		return OutcomeInfo
	default:
		return OutcomeBlock
	}
}

// applySeverity makes a frame's declared severity (frontmatter, after any
// config override applied at registration) authoritative for the gate
// outcome. A CheckFunc decides only whether the frame fired (a fail outcome)
// or not (PASS/SKIP); the frame's severity decides how strongly a fire is
// enforced. PASS/SKIP/ERROR pass through untouched. Frames that legitimately
// emit different severities by confidence opt out with `severity-source:
// frame`, in which case the CheckFunc's own outcome stands.
func applySeverity(out CheckOutcome, f frames.Frame) CheckOutcome {
	switch out {
	case OutcomePass, OutcomeSkip, OutcomeError:
		return out
	}
	if f.Frontmatter.SeveritySource == "frame" {
		return out
	}
	return severityToOutcome(f.Frontmatter.Severity)
}

// Run fans out all frames matching ctx.Trigger to goroutines, collects results,
// and returns them. Order is non-deterministic; sort at the output layer.
func Run(r *Registry, ctx CheckContext) []CheckResult {
	matching := r.MatchingTrigger(string(ctx.Trigger))
	if len(matching) == 0 {
		return nil
	}

	results := make([]CheckResult, len(matching))
	var wg sync.WaitGroup
	for i, rf := range matching {
		i, rf := i, rf
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = runOne(rf, ctx)
		}()
	}
	wg.Wait()
	return results
}

// runOne evaluates a single registered frame, recovering panics into ERROR.
func runOne(rf RegisteredFrame, ctx CheckContext) (res CheckResult) {
	res.FrameID = rf.Frame.ID()
	res.Category = rf.Frame.Frontmatter.Category
	res.Timestamp = time.Now().UTC()

	defer func() {
		if r := recover(); r != nil {
			res.Outcome = OutcomeError
			res.Reason = fmt.Sprintf("frame panic: %v", r)
		}
	}()

	if rf.Check == nil {
		res.Outcome = OutcomeError
		res.Reason = "frame has no check function bound"
		return
	}
	res = rf.Check(ctx)
	if res.FrameID == "" {
		res.FrameID = rf.Frame.ID()
	}
	if res.Category == "" {
		res.Category = rf.Frame.Frontmatter.Category
	}
	if res.Timestamp.IsZero() {
		res.Timestamp = time.Now().UTC()
	}
	// Propagate dedup-key from frontmatter so the presentation layer can
	// group hits across frames without re-reading the registry. Frames
	// should not set this themselves; it is metadata, not behavior.
	if res.DedupKey == "" {
		res.DedupKey = rf.Frame.Frontmatter.DedupKey
	}
	// Severity is data, not a hardcoded verdict: the frame's declared
	// severity (frontmatter + config override) decides BLOCK/WARN/INFO; the
	// CheckFunc only signals fired-or-not. Lets projects tune catch-vs-block
	// per frame without editing Go. (severity-source: frame opts out.)
	res.Outcome = applySeverity(res.Outcome, rf.Frame)
	return
}
