// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/whitelist"
)

// recurringRow is one deduped finding for display: location + how often it
// recurred + when last seen.
type recurringRow struct {
	FrameID  string
	Severity string
	Message  string
	Seen     int
	LastSeen int64 // unix timestamp; formatted by the template's tsFuncs
	Path     string
}

// whitelistRow is one active whitelist entry for the read-only "Whitelisted" panel.
type whitelistRow struct {
	Frame  string
	Path   string
	Reason string
}

// repoBlock is one repo's complete stats panel (decision counts + distinct
// time-prevented + recurrence detail). Built for exactly one repo; the all-repos
// page renders one of these per registered repo and sums the scalar totals.
type repoBlock struct {
	Repo            string
	Decisions       int
	Accepts         int
	Rejects         int
	ActualHours     float64
	ModeledHours    float64
	ActualHoursStr  string
	ModeledHoursStr string
	TimeRows        []timeRow
	Recurring       []recurringRow
	Whitelisted     []whitelistRow
	HasData         bool
	AllowEdits      bool
}

// buildRepoBlock assembles one repo's panel. Reuses analytics.Stats (decision
// counts), timePrevented (distinct hours), and analytics.Recurring (dedup
// detail) - all scoped to this repo.
func buildRepoBlock(db *analytics.DB, policyRoot, repo string, since time.Time) repoBlock {
	b := repoBlock{Repo: repo}
	s, err := analytics.Stats(db, analytics.StatsQuery{Repo: repo, Since: since})
	if err == nil {
		b.Decisions, b.Accepts, b.Rejects = s.Decisions, s.Accepts, s.Rejects
		b.HasData = s.Decisions > 0
	}
	if b.HasData {
		b.ActualHours, b.ModeledHours, b.TimeRows = timePrevented(db, policyRoot, repo, since)
		b.ActualHoursStr = formatHours(b.ActualHours)
		b.ModeledHoursStr = formatHours(b.ModeledHours)
		if rf, err := analytics.Recurring(db, analytics.StatsQuery{Repo: repo, Since: since}); err == nil {
			for _, r := range rf {
				b.Recurring = append(b.Recurring, recurringRow{
					FrameID: r.FrameID, Severity: r.Severity, Message: r.Message,
					Seen: r.Seen, LastSeen: r.LastSeen,
					Path: firstPathInMessage(r.Message),
				})
			}
		}
	}
	known := map[string]bool{}
	for id := range stdlibFrameByID() {
		known[id] = true
	}
	if wl, err := whitelist.LoadFromProject(filepath.Join(policyRoot, repo), known, time.Now().UTC()); err == nil && wl != nil {
		for _, ev := range wl.Entries() {
			b.Whitelisted = append(b.Whitelisted, whitelistRow{Frame: ev.Frame, Path: ev.Path, Reason: ev.Reason})
		}
	}
	return b
}

var firstPathRe = regexp.MustCompile(`([^\s:;]+):\d+`)

// firstPathInMessage extracts the first "file:line" path from a finding message
// for whitelist-path prefill. Empty when none - the path field is editable, so
// a miss just means the operator types it.
func firstPathInMessage(msg string) string {
	if m := firstPathRe.FindStringSubmatch(msg); m != nil {
		return m[1]
	}
	return ""
}

// pathIsPattern reports whether a whitelist path glob matches potentially more
// than one file - i.e. it's NOT a single exact file. Conservative: glob
// metacharacters or a trailing slash. A bare path with neither is treated as a
// single file (it matches that literal path; if it's actually a dir it matches
// nothing - over-narrow, never over-broad).
func pathIsPattern(p string) bool {
	return strings.ContainsAny(p, "*?[") || strings.HasSuffix(p, "/")
}
