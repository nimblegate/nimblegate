// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"sort"
	"strings"
	"time"
)

// StatsQuery selects which decisions to aggregate. Zero values = no bound.
type StatsQuery struct {
	Repo  string
	Since time.Time
	Until time.Time
}

// RepoStat is per-repo activity.
type RepoStat struct {
	Repo      string `json:"repo"`
	Decisions int    `json:"decisions"`
	Rejects   int    `json:"rejects"`
}

// FrameStat is a (frame, severity) finding count.
type FrameStat struct {
	FrameID  string `json:"frame_id"`
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

// StatsResult is the aggregate snapshot.
type StatsResult struct {
	Decisions int         `json:"decisions"`
	Accepts   int         `json:"accepts"`
	Rejects   int         `json:"rejects"`
	Repos     int         `json:"repos"`
	PerRepo   []RepoStat  `json:"per_repo"`
	TopFrames []FrameStat `json:"top_frames"`
	// Consistent is false when accepts+rejects != decisions - a sanity check
	// that surfaces a data/ingest glitch the operator should investigate.
	Consistent bool `json:"consistent"`
}

const topFramesLimit = 20

// whereClause builds a predicate over the decisions table; prefix lets it apply
// to an aliased table in a join (e.g. "d.").
func whereClause(q StatsQuery, prefix string) (string, []any) {
	var conds []string
	var args []any
	if q.Repo != "" {
		conds = append(conds, prefix+"repo = ?")
		args = append(args, q.Repo)
	}
	if !q.Since.IsZero() {
		conds = append(conds, prefix+"ts >= ?")
		args = append(args, q.Since.Unix())
	}
	if !q.Until.IsZero() {
		conds = append(conds, prefix+"ts <= ?")
		args = append(args, q.Until.Unix())
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

// Stats aggregates decisions (and their findings) matching q.
func Stats(d *DB, q StatsQuery) (StatsResult, error) {
	var res StatsResult
	where, args := whereClause(q, "")

	row := d.sql.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(accept=1),0), COALESCE(SUM(accept=0),0), COUNT(DISTINCT repo) FROM decisions`+where, args...)
	if err := row.Scan(&res.Decisions, &res.Accepts, &res.Rejects, &res.Repos); err != nil {
		return res, err
	}
	// Accepts and rejects are counted independently (not derived), so
	// accepts+rejects==decisions is a real integrity check: a mismatch means a
	// stray accept value (not 0/1) or an ingest glitch - surfaced to the operator.
	res.Consistent = res.Accepts+res.Rejects == res.Decisions

	rrows, err := d.sql.Query(
		`SELECT repo, COUNT(*), SUM(1-accept) FROM decisions`+where+
			` GROUP BY repo ORDER BY COUNT(*) DESC, repo`, args...)
	if err != nil {
		return res, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var rs RepoStat
		if err := rrows.Scan(&rs.Repo, &rs.Decisions, &rs.Rejects); err != nil {
			return res, err
		}
		res.PerRepo = append(res.PerRepo, rs)
	}
	if err := rrows.Err(); err != nil {
		return res, err
	}

	jwhere, jargs := whereClause(q, "d.")
	frows, err := d.sql.Query(
		`SELECT f.frame_id, f.severity, COUNT(*) c
		 FROM findings f JOIN decisions d ON d.id = f.decision_id`+jwhere+
			` GROUP BY f.frame_id, f.severity ORDER BY c DESC, f.frame_id LIMIT ?`,
		append(jargs, topFramesLimit)...)
	if err != nil {
		return res, err
	}
	defer frows.Close()
	for frows.Next() {
		var fs FrameStat
		if err := frows.Scan(&fs.FrameID, &fs.Severity, &fs.Count); err != nil {
			return res, err
		}
		res.TopFrames = append(res.TopFrames, fs)
	}
	return res, frows.Err()
}

// PreventedStat is one frame's blocking-severity (BLOCK/ERROR) finding counts,
// split by whether the parent push was rejected (actually prevented) or
// accepted (modeled would-have - only arises in observe mode, where a blocking
// finding rides on an accepted push).
type PreventedStat struct {
	FrameID  string `json:"frame_id"`
	Rejected int    `json:"rejected"` // findings on accept=0 decisions
	Observed int    `json:"observed"` // findings on accept=1 decisions
}

// PreventedBreakdown returns the complete per-frame breakdown of blocking
// findings matching q, split by the parent decision's accept status. Unlike
// Stats.TopFrames it is not row-limited - callers weight every frame by its
// per-hit time estimate. WARN/INFO are excluded (they never block).
func PreventedBreakdown(d *DB, q StatsQuery) ([]PreventedStat, error) {
	where, args := whereClause(q, "d.")
	if where == "" {
		where = " WHERE f.severity IN ('BLOCK','ERROR')"
	} else {
		where += " AND f.severity IN ('BLOCK','ERROR')"
	}
	rows, err := d.sql.Query(
		`SELECT f.frame_id,
		        COUNT(DISTINCT CASE WHEN d.accept=0 THEN f.fingerprint END),
		        COUNT(DISTINCT CASE WHEN d.accept=1 THEN f.fingerprint END)
		 FROM findings f JOIN decisions d ON d.id = f.decision_id`+where+
			` GROUP BY f.frame_id ORDER BY f.frame_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PreventedStat
	for rows.Next() {
		var ps PreventedStat
		if err := rows.Scan(&ps.FrameID, &ps.Rejected, &ps.Observed); err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// RecurringFinding is one deduped finding (by fingerprint) with its recurrence
// across pushes. Scoped to a single repo (q.Repo must be set).
type RecurringFinding struct {
	FrameID   string `json:"frame_id"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	Seen      int    `json:"seen"`       // number of pushes that emitted it
	FirstSeen int64  `json:"first_seen"` // unix ts
	LastSeen  int64  `json:"last_seen"`  // unix ts
}

// Recurring returns the repo's findings collapsed by fingerprint, each with how
// many pushes emitted it and the first/last timestamps. All severities (the
// recurrence view shows recurring WARN/INFO too). Ordered by severity rank then
// seen-count, both descending.
func Recurring(d *DB, q StatsQuery) ([]RecurringFinding, error) {
	where, args := whereClause(q, "d.")
	rows, err := d.sql.Query(
		`SELECT f.frame_id, f.severity, f.message, COUNT(*), MIN(d.ts), MAX(d.ts)
		 FROM findings f JOIN decisions d ON d.id = f.decision_id`+where+
			` GROUP BY f.fingerprint`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecurringFinding
	for rows.Next() {
		var r RecurringFinding
		if err := rows.Scan(&r.FrameID, &r.Severity, &r.Message, &r.Seen, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if ri, rj := sevRank[out[i].Severity], sevRank[out[j].Severity]; ri != rj {
			return ri > rj
		}
		if out[i].Seen != out[j].Seen {
			return out[i].Seen > out[j].Seen
		}
		return out[i].Message < out[j].Message
	})
	return out, nil
}

// BounceStat is one repo's reject ratio - "where does code bounce back".
type BounceStat struct {
	Repo      string  `json:"repo"`
	Decisions int     `json:"decisions"`
	Rejects   int     `json:"rejects"`
	Rate      float64 `json:"rate"`
}

// BounceRate ranks repos by rejects/decisions descending, excluding repos with
// fewer than minDecisions pushes (a 1-push repo with 1 reject is not a trend).
func BounceRate(d *DB, q StatsQuery, minDecisions int) ([]BounceStat, error) {
	if minDecisions < 1 {
		minDecisions = 1
	}
	where, args := whereClause(q, "")
	rows, err := d.sql.Query(
		`SELECT repo, COUNT(*) n, COALESCE(SUM(1-accept),0) rej FROM decisions`+where+
			` GROUP BY repo HAVING n >= ? ORDER BY CAST(rej AS REAL)/n DESC, repo`,
		append(args, minDecisions)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BounceStat
	for rows.Next() {
		var bs BounceStat
		if err := rows.Scan(&bs.Repo, &bs.Decisions, &bs.Rejects); err != nil {
			return nil, err
		}
		bs.Rate = float64(bs.Rejects) / float64(bs.Decisions)
		out = append(out, bs)
	}
	return out, rows.Err()
}

// DecisionSummary is one decision with its top findings - the receipts behind
// any aggregate. Fields are limited to what the analytics store ingests.
type DecisionSummary struct {
	TS          int64    `json:"ts"`
	Repo        string   `json:"repo"`
	Accept      bool     `json:"accept"`
	Refs        string   `json:"refs"` // JSON-array string as ingested
	MaxSeverity string   `json:"max_severity"`
	TopFindings []string `json:"top_findings"` // "frame-id (SEVERITY)", max 3
}

// RecentDecisions returns up to limit newest decisions matching q;
// result filters "accepted"/"rejected" ("" = both). limit <1 defaults to 10, >50 clamps to 50.
// Unrecognized result values fall back to both (no filter).
func RecentDecisions(d *DB, q StatsQuery, result string, limit int) ([]DecisionSummary, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	where, args := whereClause(q, "")
	var cond string
	switch result {
	case "accepted":
		cond = "accept = 1"
	case "rejected":
		cond = "accept = 0"
	}
	if cond != "" {
		if where == "" {
			where = " WHERE " + cond
		} else {
			where += " AND " + cond
		}
	}
	rows, err := d.sql.Query(
		`SELECT id, ts, repo, accept, COALESCE(refs,''), max_severity FROM decisions`+where+
			` ORDER BY ts DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type rec struct {
		id int64
		ds DecisionSummary
	}
	var recs []rec
	for rows.Next() {
		var r rec
		var accept int
		if err := rows.Scan(&r.id, &r.ds.TS, &r.ds.Repo, &accept, &r.ds.Refs, &r.ds.MaxSeverity); err != nil {
			return nil, err
		}
		r.ds.Accept = accept == 1
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]DecisionSummary, 0, len(recs))
	for _, r := range recs {
		// Severity ranks mirror sevRank in helpers.go - keep in sync.
		frows, err := d.sql.Query(
			`SELECT frame_id, severity FROM findings WHERE decision_id = ?
			 ORDER BY CASE severity WHEN 'BLOCK' THEN 4 WHEN 'ERROR' THEN 3 WHEN 'WARN' THEN 2 ELSE 1 END DESC, id
			 LIMIT 3`, r.id)
		if err != nil {
			return nil, err
		}
		for frows.Next() {
			var fid, sev string
			if err := frows.Scan(&fid, &sev); err != nil {
				frows.Close()
				return nil, err
			}
			r.ds.TopFindings = append(r.ds.TopFindings, fid+" ("+sev+")")
		}
		if err := frows.Err(); err != nil {
			frows.Close()
			return nil, err
		}
		frows.Close()
		out = append(out, r.ds)
	}
	return out, nil
}

// Verdict is the gate's decision on the push that made a commit a ref tip.
type Verdict struct {
	Accept      bool     `json:"accept"`
	TopFindings []string `json:"top_findings"` // "frame-id (SEVERITY)", max 3
}

// VerdictForSHAs maps each input SHA that is a recorded push tip in repo to the
// gate's verdict on that push. SHAs with no tip row (mid-push commits, history
// from before this feature, force-pushed-away tips) are simply absent.
func VerdictForSHAs(d *DB, repo string, shas []string) (map[string]Verdict, error) {
	out := map[string]Verdict{}
	if len(shas) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(shas)), ",")
	args := make([]any, 0, len(shas)+1)
	args = append(args, repo)
	for _, s := range shas {
		args = append(args, s)
	}
	rows, err := d.sql.Query(
		`SELECT pt.sha, d.id, d.accept
		 FROM push_tips pt JOIN decisions d ON d.id = pt.decision_id
		 WHERE d.repo = ? AND pt.sha IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	type hit struct {
		sha    string
		id     int64
		accept bool
	}
	var hits []hit
	for rows.Next() {
		var h hit
		var acc int
		if err := rows.Scan(&h.sha, &h.id, &acc); err != nil {
			rows.Close()
			return nil, err
		}
		h.accept = acc == 1
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, h := range hits {
		v := Verdict{Accept: h.accept}
		frows, err := d.sql.Query(
			`SELECT frame_id, severity FROM findings WHERE decision_id = ?
			 ORDER BY CASE severity WHEN 'BLOCK' THEN 4 WHEN 'ERROR' THEN 3 WHEN 'WARN' THEN 2 ELSE 1 END DESC, id
			 LIMIT 3`, h.id)
		if err != nil {
			return nil, err
		}
		for frows.Next() {
			var fid, sev string
			if err := frows.Scan(&fid, &sev); err != nil {
				frows.Close()
				return nil, err
			}
			v.TopFindings = append(v.TopFindings, fid+" ("+sev+")")
		}
		if err := frows.Err(); err != nil {
			frows.Close()
			return nil, err
		}
		frows.Close()
		out[h.sha] = v
	}
	return out, nil
}

// PushFinding is one finding on a push, with the fields the benchmark scorer
// needs (frame, severity, fingerprint for recurrence, message for whitelist
// matching).
type PushFinding struct {
	FrameID     string `json:"frame_id"`
	Severity    string `json:"severity"`
	Fingerprint string `json:"fingerprint"`
	Message     string `json:"message"`
}

// Push is one decision (a gate push) with ALL its findings.
type Push struct {
	TS       int64         `json:"ts"`
	Accept   bool          `json:"accept"`
	Findings []PushFinding `json:"findings"`
}

// RunPushes returns every push for repo, oldest first, each with all of its
// findings. Used by the benchmark scorer to compute convergence/cleanliness/
// recurrence over a run's push sequence.
func RunPushes(d *DB, repo string) ([]Push, error) {
	rows, err := d.sql.Query(
		`SELECT id, ts, accept FROM decisions WHERE repo = ? ORDER BY ts ASC, id ASC`, repo)
	if err != nil {
		return nil, err
	}
	type rec struct {
		id int64
		p  Push
	}
	var recs []rec
	for rows.Next() {
		var r rec
		var acc int
		if err := rows.Scan(&r.id, &r.p.TS, &acc); err != nil {
			rows.Close()
			return nil, err
		}
		r.p.Accept = acc == 1
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	out := make([]Push, 0, len(recs))
	for _, r := range recs {
		frows, err := d.sql.Query(
			`SELECT frame_id, severity, fingerprint, message FROM findings WHERE decision_id = ? ORDER BY id`, r.id)
		if err != nil {
			return nil, err
		}
		for frows.Next() {
			var f PushFinding
			if err := frows.Scan(&f.FrameID, &f.Severity, &f.Fingerprint, &f.Message); err != nil {
				frows.Close()
				return nil, err
			}
			r.p.Findings = append(r.p.Findings, f)
		}
		if err := frows.Err(); err != nil {
			frows.Close()
			return nil, err
		}
		frows.Close()
		out = append(out, r.p)
	}
	return out, nil
}

// Repos returns the distinct repo names in the analytics store, sorted.
func Repos(d *DB) ([]string, error) {
	rows, err := d.sql.Query(`SELECT DISTINCT repo FROM decisions ORDER BY repo`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
