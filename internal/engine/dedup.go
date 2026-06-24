// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"sort"
	"strings"

	"nimblegate/internal/frames"
)

// RenderRow is one line of the human-readable report. Most results map
// 1:1 from CheckResult to RenderRow; cross-frame dedup is the exception
// - two frames sharing dedup-key + hit scope collapse into a single row
// listing all firing frame IDs.
//
// Audit log entries are written BEFORE dedup, so dedup never hides a
// finding from the audit trail. It is purely a presentation transform.
type RenderRow struct {
	Outcome  CheckOutcome
	Category frames.Category // worst-of for ordering; first when severity ties
	FrameIDs []string        // sorted unique
	Reason   string          // primary text (file:line context or original Reason)
	Fix      string          // first non-empty fix from contributing frames
}

// PresentResults transforms raw frame results into renderable rows,
// applying the cross-frame dedup pass.
//
// A result participates in dedup iff:
//
//   - its DedupKey is non-empty (the frame opted in via frontmatter)
//   - AND it has at least one Hit
//
// Within the dedup-participating subset, hits sharing (file:line, dedup-key)
// across frames collapse into one row. Hits at a unique scope produce one
// row each (single frame, single hit). The Reason text is replaced with a
// "file:line - label" header so the row is location-centric, not
// frame-centric. Fix is the first non-empty fix among contributing frames.
//
// Non-participating results pass through unchanged.
//
// Output ordering: category priority (worst-of for deduped rows) → frame ID.
func PresentResults(results []CheckResult) []RenderRow {
	var rows []RenderRow

	// Bucket dedup-participating hits by (dedup-key, file, line).
	// Non-participating results convert directly to passthrough rows.
	type groupKey struct {
		DedupKey string
		File     string
		Line     int
	}
	type groupAccum struct {
		Outcome  CheckOutcome
		Category frames.Category
		FrameIDs map[string]struct{}
		Labels   []string
		Fix      string
		File     string
		Line     int
	}
	groups := map[groupKey]*groupAccum{}

	for _, r := range results {
		if r.DedupKey == "" || len(r.Hits) == 0 {
			// Passthrough - no dedup participation.
			rows = append(rows, RenderRow{
				Outcome:  r.Outcome,
				Category: r.Category,
				FrameIDs: []string{r.FrameID},
				Reason:   r.Reason,
				Fix:      r.Fix,
			})
			continue
		}
		for _, h := range r.Hits {
			scopeLine := h.Line
			if r.DedupKey == "file" {
				scopeLine = 0 // collapse all hits in a file to the same bucket
			}
			k := groupKey{DedupKey: r.DedupKey, File: h.File, Line: scopeLine}
			g, ok := groups[k]
			if !ok {
				g = &groupAccum{
					Category: r.Category,
					FrameIDs: map[string]struct{}{},
					File:     h.File,
					Line:     scopeLine,
				}
				groups[k] = g
			}
			// Worst-of across frames in the group.
			if outcomeRank(r.Outcome) > outcomeRank(g.Outcome) {
				g.Outcome = r.Outcome
				g.Category = r.Category
			}
			g.FrameIDs[r.FrameID] = struct{}{}
			g.Labels = append(g.Labels, h.Label)
			if g.Fix == "" && r.Fix != "" {
				g.Fix = r.Fix
			}
		}
	}

	// Materialize grouped accumulators into rows.
	for _, g := range groups {
		ids := make([]string, 0, len(g.FrameIDs))
		for id := range g.FrameIDs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		rows = append(rows, RenderRow{
			Outcome:  g.Outcome,
			Category: g.Category,
			FrameIDs: ids,
			Reason:   dedupReasonText(g.File, g.Line, g.Labels, ids),
			Fix:      g.Fix,
		})
	}

	// Stable ordering for the final output.
	sort.SliceStable(rows, func(i, j int) bool {
		pi := frames.CategoryPriority(rows[i].Category)
		pj := frames.CategoryPriority(rows[j].Category)
		if pi != pj {
			return pi < pj
		}
		// Within same category, lead with worst outcome.
		oi := outcomeRank(rows[i].Outcome)
		oj := outcomeRank(rows[j].Outcome)
		if oi != oj {
			return oi > oj
		}
		// Then by first frame ID for determinism.
		return rows[i].FrameIDs[0] < rows[j].FrameIDs[0]
	})

	return rows
}

// outcomeRank orders outcomes worst → best for "worst-of" merging.
// BLOCK > ERROR > WARN > INFO > PASS > SKIP. ERROR ranks below BLOCK
// because a real BLOCK from a working frame is the more actionable
// finding when both happen in the same group.
func outcomeRank(o CheckOutcome) int {
	switch o {
	case OutcomeBlock:
		return 5
	case OutcomeError:
		return 4
	case OutcomeWarn:
		return 3
	case OutcomeInfo:
		return 2
	case OutcomePass:
		return 1
	case OutcomeSkip:
		return 0
	}
	return -1
}

// dedupReasonText composes the headline for a deduped row. Format:
//
//	"file:line - label1; label2 [shared by: frame-a, frame-b]"
//
// When only one frame contributes, the "[shared by …]" suffix is dropped
// so the row reads naturally as a single-frame finding that happens to
// be eligible for dedup.
func dedupReasonText(file string, line int, labels, frameIDs []string) string {
	location := file
	if line > 0 {
		location = fmt.Sprintf("%s:%d", file, line)
	}
	// Deduplicate identical labels (two frames may report the same string).
	seen := map[string]struct{}{}
	uniq := make([]string, 0, len(labels))
	for _, l := range labels {
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		uniq = append(uniq, l)
	}
	body := strings.Join(uniq, "; ")
	if len(frameIDs) > 1 {
		return fmt.Sprintf("%s - %s [shared by: %s]", location, body, strings.Join(frameIDs, ", "))
	}
	return fmt.Sprintf("%s - %s", location, body)
}
