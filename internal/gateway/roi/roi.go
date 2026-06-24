// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package roi computes the gateway's "debugging time prevented" estimate from
// the decision log: distinct blocking findings × per-frame hours-per-hit
// (stdlib defaults or a repo's [time-estimates] override). Shared by the
// dashboard's /stats page and the agent stats API so both report identical
// numbers.
package roi

import (
	"sort"
	"time"

	"nimblegate/internal/frames"
	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/stdlib"
)

// Row is one frame's contribution to the estimate: its per-hit hours and
// source, the distinct rejected/observed issue counts, and their subtotals.
type Row struct {
	FrameID      string  `json:"frame_id"`
	Source       string  `json:"source"`
	HoursPerHit  float64 `json:"hours_per_hit"`
	Rejected     int     `json:"rejected"`
	Observed     int     `json:"observed"`
	ActualHours  float64 `json:"actual_hours"`
	ModeledHours float64 `json:"modeled_hours"`
}

// Result is the time-prevented estimate for one repo (or all repos): actual
// (blocked-and-fixed) vs modeled (conservative upper bound), plus the
// per-frame breakdown sorted by total contribution.
type Result struct {
	ActualHours  float64 `json:"actual_hours"`
	ModeledHours float64 `json:"modeled_hours"`
	Rows         []Row   `json:"rows"`
}

// PreventedTime resolves the actual + modeled totals and per-frame breakdown
// for repo (empty = all repos), using that repo's [time-estimates] override.
// Counts are distinct issues (PreventedBreakdown dedups by fingerprint), so
// re-pushing the same debt doesn't inflate them. Frame IDs absent from the
// stdlib registry (linters, archived frames) have no honest estimate → 0.
func PreventedTime(db *analytics.DB, policyRoot, repo string, since time.Time) Result {
	frameByID := StdlibFrameByID()
	te, _ := gateway.LoadTimeEstimates(policyRoot, repo)
	bd, err := analytics.PreventedBreakdown(db, analytics.StatsQuery{Repo: repo, Since: since})
	if err != nil {
		return Result{}
	}
	var res Result
	for _, st := range bd {
		fm, ok := frameByID[st.FrameID]
		if !ok {
			continue
		}
		tierOv, tierSet := te.Lookup(fm.EffectiveTier())
		hpp, src := fm.EffectiveTimeCostHoursPrevented(tierOv, tierSet)
		aSub := float64(st.Rejected) * hpp
		mSub := float64(st.Observed) * hpp
		res.ActualHours += aSub
		res.ModeledHours += mSub
		res.Rows = append(res.Rows, Row{
			FrameID: st.FrameID, Source: string(src), HoursPerHit: hpp,
			Rejected: st.Rejected, Observed: st.Observed, ActualHours: aSub, ModeledHours: mSub,
		})
	}
	sort.Slice(res.Rows, func(i, j int) bool {
		ti, tj := res.Rows[i].ActualHours+res.Rows[i].ModeledHours, res.Rows[j].ActualHours+res.Rows[j].ModeledHours
		if ti != tj {
			return ti > tj
		}
		return res.Rows[i].FrameID < res.Rows[j].FrameID
	})
	return res
}

// StdlibFrameByID maps every embedded stdlib frame ID to its frontmatter. The
// gateway policy overlay wipes any pushed .appframes/, so the embedded stdlib
// registry is the authoritative frame set server-side.
func StdlibFrameByID() map[string]frames.Frontmatter {
	m := map[string]frames.Frontmatter{}
	sf, _ := stdlib.Load()
	for _, f := range sf {
		m[f.ID()] = f.Frontmatter
	}
	return m
}
