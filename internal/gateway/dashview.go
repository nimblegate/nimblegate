// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Filter struct {
	Repo        string
	RejectsOnly bool
	Details     bool // show each finding's full message (path/detail) inline; default off
	Last100     bool // cap the view to the most recent 100 decisions; default off
	Limit       int
	Before      time.Time // paging cursor: render only records strictly before this; zero = newest page
}

type Summary struct {
	Repos      int
	Accepts    int
	Rejects    int
	TopBlock   string
	TopBlockN  int
	OldestTime time.Time // Time of the oldest rendered row (next paging cursor); zero if no rows
	HasMore    bool      // true when matching rows exceeded Limit (an older page exists)
}

type DecisionRow struct {
	Time        time.Time
	Repo        string
	Refs        []string
	RefDisplays []RefDisplay // ref name + short SHA per ref, for feed rendering
	Accept      bool
	Messages    []string
	Locations   []string // path:line hits extracted from Messages (fast feed scan)
	Findings    []Finding

	// NotificationStatus is the row-level summary of the notification rail's
	// state for this push. Nil = rail disabled or AuditRecord predates the
	// rail (older log lines); the feed template's {{if}} guard keeps the
	// existing UI unchanged when nil. Per spec §10.6.
	NotificationStatus *NotificationStatusView
	// ActiveLoop is the per-PR loop state surfaced inline so the operator
	// can see "attempt N/M with @bot" without leaving the feed and can
	// reset the loop with one click. Nil = no active state file for this PR
	// (the typical case: PR accepted or never engaged the rail).
	ActiveLoop *ActiveLoopView
}

// NotificationStatusView is the row-level rendering data for the
// notification rail. Symbol + Message + Indicator are decoupled so the
// template can pick any of them: a chip uses the symbol, a tooltip uses the
// message, the rule that decides the visual bucket uses the indicator.
type NotificationStatusView struct {
	Indicator string // "delivered" | "deadlettered" | "queued"
	Message   string // human-readable summary, e.g. "PR comment delivered to #42 · webhook fired"
	Symbol    string // emoji glyph for the inline chip
}

// ActiveLoopView is the per-PR active-loop summary + the Reset URL. Reset is
// surfaced as a POST so it's CSRF-protected and not accidentally triggered
// by a browser pre-fetcher.
type ActiveLoopView struct {
	PRNumber     int
	AttemptCount int
	MaxAttempts  int
	CurrentBot   string
	ResetURL     string // POST target - /feed/reset-loop?repo=<r>&pr=<n>
}

// RefDisplay is the feed-friendly view of a RefUpdate: ref name plus a short
// commit SHA. SHA is "" when the audit record is from before RefUpdates were
// persisted (pre-2026-06-04) or when the update is a delete.
type RefDisplay struct {
	Name     string // refs/heads/main
	ShortSHA string // 7-char prefix of NewRev; empty if unavailable
}

// buildRefDisplays zips ref names with their short SHAs for the feed. Falls
// back to names-only when RefUpdates is empty (older audit lines).
func buildRefDisplays(refs []string, updates []RefUpdate) []RefDisplay {
	out := make([]RefDisplay, 0, len(refs))
	if len(updates) > 0 {
		for _, u := range updates {
			rd := RefDisplay{Name: u.Name}
			if !u.IsDelete() && len(u.NewRev) >= 7 {
				rd.ShortSHA = u.NewRev[:7]
			}
			out = append(out, rd)
		}
		return out
	}
	for _, r := range refs {
		out = append(out, RefDisplay{Name: r})
	}
	return out
}

// locationRe matches a `path/with-segments/file.ext:line` location. The
// extension must START with a letter so version strings like 1.2.3:4 don't
// false-positive. Slashes, hyphens, underscores, dots, and digits are all
// fine in the path segments. Frame IDs like security/no-private-keys-in-repo
// don't match because they have no extension and no :line suffix.
var locationRe = regexp.MustCompile(`[A-Za-z0-9_./-]+\.[a-zA-Z][a-zA-Z0-9]*:\d+`)

// LocationsFromMessages scans every audit Decision.Message line for path:line
// patterns and returns them deduplicated, first-seen order. Each call is pure
// and total; nil/empty input returns nil. The feed renders this list as the
// "location" column so an operator can scan where a push fired without
// reading the verbose gateway narrative.
func LocationsFromMessages(msgs []string) []string {
	if len(msgs) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range msgs {
		for _, hit := range locationRe.FindAllString(m, -1) {
			if !seen[hit] {
				seen[hit] = true
				out = append(out, hit)
			}
		}
	}
	return out
}

type ViewModel struct {
	Summary Summary
	Repos   []string
	Rows    []DecisionRow
	Filter  Filter
}

// frameFromMessage pulls the bracketed frame id out of an audit message like
// "refs/heads/main: BLOCK [security/no-private-keys-in-repo] reason". "" if none.
func frameFromMessage(msg string) string {
	i := strings.IndexByte(msg, '[')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(msg[i+1:], ']')
	if j < 0 {
		return ""
	}
	return msg[i+1 : i+1+j]
}

// notifStatusView projects a NotificationStatus into the row-level view shape.
// The three buckets (delivered / deadlettered / queued) cover the lifecycle
// states defined in audit.go. Nil in → nil out so legacy audit lines with no
// notification field don't get a spurious "queued" badge.
func notifStatusView(n *NotificationStatus) *NotificationStatusView {
	if n == nil {
		return nil
	}
	switch {
	case n.Deadlettered:
		return &NotificationStatusView{
			Indicator: "deadlettered",
			Symbol:    "warn",
			Message:   fmtDeadletter(n),
		}
	case n.InlineSucceeded || !n.DeliveredAt.IsZero():
		return &NotificationStatusView{
			Indicator: "delivered",
			Symbol:    "notif",
			Message:   "PR comment delivered · webhook fired",
		}
	default:
		return &NotificationStatusView{
			Indicator: "queued",
			Symbol:    "pending",
			Message:   "in queue",
		}
	}
}

func fmtDeadletter(n *NotificationStatus) string {
	if n.DeliveryAttempts > 0 {
		return fmt.Sprintf("delivery failed after %d attempts", n.DeliveryAttempts)
	}
	return "delivery failed"
}

// BuildView is pure and total: summary over all records; rows filtered, newest-first, capped.
func BuildView(records []AuditRecord, f Filter) ViewModel {
	if f.Limit <= 0 {
		f.Limit = 500
	}
	repoSet := map[string]bool{}
	var accepts, rejects int
	blocks := map[string]int{}
	for _, r := range records {
		repoSet[r.Repo] = true
		if r.Accept {
			accepts++
			continue
		}
		rejects++
		for _, m := range r.Messages {
			if fr := frameFromMessage(m); fr != "" {
				blocks[fr]++
			}
		}
	}
	repos := make([]string, 0, len(repoSet))
	for k := range repoSet {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	topBlock, topN := "", 0
	for fr, n := range blocks {
		if n > topN || (n == topN && fr < topBlock) {
			topBlock, topN = fr, n
		}
	}

	rows := make([]DecisionRow, 0, len(records))
	for _, r := range records {
		if !f.Before.IsZero() && !r.Time.Before(f.Before) {
			continue
		}
		if f.Repo != "" && r.Repo != f.Repo {
			continue
		}
		if f.RejectsOnly && r.Accept {
			continue
		}
		rows = append(rows, DecisionRow{
			Time:               r.Time,
			Repo:               r.Repo,
			Refs:               r.Refs,
			RefDisplays:        buildRefDisplays(r.Refs, r.RefUpdates),
			Accept:             r.Accept,
			Messages:           r.Messages,
			Locations:          LocationsFromMessages(r.Messages),
			Findings:           r.Findings,
			NotificationStatus: notifStatusView(r.Notification),
		})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Time.After(rows[j].Time) })
	hasMore := len(rows) > f.Limit
	if hasMore {
		rows = rows[:f.Limit]
	}
	var oldest time.Time
	if len(rows) > 0 {
		oldest = rows[len(rows)-1].Time
	}

	return ViewModel{
		Summary: Summary{Repos: len(repoSet), Accepts: accepts, Rejects: rejects, TopBlock: topBlock, TopBlockN: topN, OldestTime: oldest, HasMore: hasMore},
		Repos:   repos,
		Rows:    rows,
		Filter:  f,
	}
}
