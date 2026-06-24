// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"io"

	"nimblegate/internal/frames"
)

// FormatLoadWarnings writes a prominent banner to w if any frames failed to
// load. Should be called by trigger handlers BEFORE FormatResults so users
// see broken-frame warnings in the main output (not just stderr).
//
// Returns the number of warnings printed so callers can include it in their
// own summaries if desired.
func FormatLoadWarnings(w io.Writer, errs []error) int {
	if len(errs) == 0 {
		return 0
	}
	fmt.Fprintf(w, "⚠️  %d frame(s) failed to load - they will NOT run. Fix with `nimblegate lint`:\n", len(errs))
	for _, e := range errs {
		fmt.Fprintf(w, "   - %v\n", e)
	}
	fmt.Fprintln(w)
	return len(errs)
}

// FormatResults writes a human-readable report to w. Returns 1 if any result
// is BLOCK or ERROR (causing the trigger to fail the action), 0 otherwise.
//
// Output ordering is by category priority (most destructive first), then
// FrameID. Results from frames that opted into dedup (frontmatter
// `dedup-key`) are collapsed by PresentResults so the user sees one row
// per (file:line, dedup-key) instead of one row per frame. The audit log
// is written separately by the caller and is unaffected by dedup.
//
// Pass count comes from raw results, not deduped rows, so the summary
// reports the true number of frames that passed.
func FormatResults(w io.Writer, results []CheckResult) int {
	if len(results) == 0 {
		fmt.Fprintln(w, "✓ nimblegate: no checks ran for this trigger.")
		return 0
	}

	passes := 0
	for _, r := range results {
		if r.Outcome == OutcomePass {
			passes++
		}
	}

	rows := PresentResults(results)

	var blocks, warns, infos, errs int
	for _, r := range rows {
		// Sanitize all frame-supplied strings to defeat terminal injection
		// via crafted frontmatter or check function output.
		fids := make([]string, len(r.FrameIDs))
		for i, id := range r.FrameIDs {
			fids[i] = frames.SanitizeForOutput(id)
		}
		// Primary frame label for the row: first frame ID (already sorted).
		fid := fids[0]
		cat := frames.SanitizeForOutput(string(r.Category))
		reason := frames.SanitizeForOutput(r.Reason)
		fix := frames.SanitizeForOutput(r.Fix)
		switch r.Outcome {
		case OutcomePass:
			// pass rows don't render here (already counted from raw results)
		case OutcomeBlock:
			blocks++
			fmt.Fprintf(w, "❌ %s (%s) - %s\n", fid, cat, reason)
			if fix != "" {
				fmt.Fprintf(w, "   fix: %s\n", fix)
			}
		case OutcomeWarn:
			warns++
			fmt.Fprintf(w, "⚠️  %s (%s) - %s\n", fid, cat, reason)
			if fix != "" {
				fmt.Fprintf(w, "   fix: %s\n", fix)
			}
		case OutcomeInfo:
			infos++
			fmt.Fprintf(w, "ℹ️  %s (%s) - %s\n", fid, cat, reason)
		case OutcomeError:
			errs++
			fmt.Fprintf(w, "💥 %s (%s) - frame error: %s\n", fid, cat, reason)
		case OutcomeSkip:
			// silent
		}
	}
	// Positive confirmation when everything passed cleanly. Veteran users
	// still get the terse summary line below; new users get explicit
	// "yes, it worked".
	if blocks == 0 && warns == 0 && infos == 0 && errs == 0 && passes > 0 {
		fmt.Fprintf(w, "✓ nimblegate: %d frame(s) passed.\n", passes)
	}

	fmt.Fprintf(w, "\nSummary: %d pass, %d warn, %d info, %d block, %d error\n",
		passes, warns, infos, blocks, errs)

	if blocks > 0 || errs > 0 {
		return 1
	}
	return 0
}
