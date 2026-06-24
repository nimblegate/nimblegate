// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"time"

	"nimblegate/internal/frames"
	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/gateway/roi"
)

// timeRow is one frame's contribution to a repo's time-prevented estimate: its
// per-hit hours and source, plus the distinct rejected/observed issue counts
// and their hour subtotals.
type timeRow struct {
	FrameID     string
	Source      string
	HoursPerHit float64
	Rejected    int
	Observed    int
	ActualSub   float64
	ModeledSub  float64
}

// timePrevented resolves the actual + modeled time-prevented totals and a
// per-frame breakdown for ONE repo, using that repo's [time-estimates] override.
// Counts are distinct issues (PreventedBreakdown dedups by fingerprint), so
// re-pushing the same debt does not inflate them. Frame IDs absent from the
// stdlib registry (linters, archived frames) have no honest estimate → 0.
func timePrevented(db *analytics.DB, policyRoot, repo string, since time.Time) (actual, modeled float64, rows []timeRow) {
	res := roi.PreventedTime(db, policyRoot, repo, since)
	rows = make([]timeRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, timeRow{
			FrameID: r.FrameID, Source: r.Source, HoursPerHit: r.HoursPerHit,
			Rejected: r.Rejected, Observed: r.Observed, ActualSub: r.ActualHours, ModeledSub: r.ModeledHours,
		})
	}
	return res.ActualHours, res.ModeledHours, rows
}

// stdlibFrameByID maps every embedded stdlib frame ID to its frontmatter; thin
// wrapper over the shared roi registry so the dashboard's other call sites
// (whitelist, tuning, repoblock) keep one source of truth.
func stdlibFrameByID() map[string]frames.Frontmatter {
	return roi.StdlibFrameByID()
}
