// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package agentapi exposes the gateway's decision analytics to AI agents:
// deterministic aggregations over frame-validated audit data, served via REST
// and MCP. Read-only by construction - it calls only analytics query
// functions; the model narrates numbers computed by SQL.
package agentapi

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/gateway/gitlog"
	"nimblegate/internal/gateway/roi"
)

// Service owns auth verification, parameter clamping, the rate guard, and the
// analytics calls behind every surface (REST and MCP).
type Service struct {
	PolicyRoot     string
	Verify         func(token string) (bool, error) // auth.Store.VerifyAPIToken; nil = API disabled
	ExposeExcerpts bool                             // reserved: finding messages in receipts
	ReposRoot      string                           // gateway bare-repos dir (<repo>.git); "" disables what_changed

	mu         sync.Mutex
	hits       map[string][]time.Time
	lastIngest time.Time
}

// logFn is gitlog.Log, overridable in tests.
var logFn = gitlog.Log

// rateLimitPerMin bounds per-token request rate - protects the SQLite ingest
// path from a looping agent.
const rateLimitPerMin = 60

// ingestEvery throttles the lazy refresh: under agent traffic, re-ingesting
// per request would take a write transaction each time (offsets are saved
// unconditionally) and contend with the dashboard and live pushes.
const ingestEvery = 5 * time.Second

// allow records one request for the token and reports whether it fits the
// fixed one-minute window. The token is hashed before use as a map key so
// the raw secret is never held in memory beyond the call stack.
func (s *Service) allow(token string) bool {
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hits == nil {
		s.hits = map[string][]time.Time{}
	}
	cut := time.Now().Add(-time.Minute)
	kept := s.hits[key][:0]
	for _, t := range s.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rateLimitPerMin {
		s.hits[key] = kept
		return false
	}
	s.hits[key] = append(kept, time.Now())
	return true
}

// Params are the only inputs any tool takes. Zero values get defaults; out-of-
// range values clamp (never error) - small-model callers must always get an
// answer plus a note, not a refusal.
type Params struct {
	Repo      string `json:"repo"`
	Days      int    `json:"days"`
	Severity  string `json:"severity"`
	Result    string `json:"result"`
	MinPushes int    `json:"min_pushes"`
	Limit     int    `json:"limit"`
	Path      string `json:"path"`
	Query     string `json:"query"`
	Format    string `json:"format"` // MCP only: "text" (default) or "json"

	// MaxLimit raises the Limit ceiling above the default 50, bounded by an
	// absolute backstop. Set ONLY by the in-process dashboard (operator surface);
	// the REST/MCP parsers never populate it, so the agent API stays capped at 50
	// regardless of request. json:"-" keeps it off the wire.
	MaxLimit int `json:"-"`
}

// clamp normalizes p in place and returns human-readable notes about what it
// changed.
func (p *Params) clamp() []string {
	var notes []string
	if p.Days <= 0 {
		p.Days = 30
	}
	if p.Days > 365 {
		p.Days = 365
		notes = append(notes, "days clamped to 365")
	}
	if p.Limit <= 0 {
		p.Limit = 10
	}
	// Default ceiling 50 (protects small-model agent payloads); the dashboard
	// raises it via MaxLimit, hard-backstopped at 500.
	ceiling := 50
	if p.MaxLimit > ceiling {
		ceiling = p.MaxLimit
		if ceiling > 500 {
			ceiling = 500
		}
	}
	if p.Limit > ceiling {
		p.Limit = ceiling
		notes = append(notes, fmt.Sprintf("limit clamped to %d", ceiling))
	}
	if p.MinPushes <= 0 {
		p.MinPushes = 5
	}
	switch p.Result {
	case "", "accepted", "rejected":
	default:
		notes = append(notes, fmt.Sprintf("unknown result %q ignored (use accepted|rejected)", p.Result))
		p.Result = ""
	}
	// Validate Severity: allowed "", BLOCK, ERROR, WARN, INFO (case-insensitive).
	switch strings.ToUpper(p.Severity) {
	case "", "BLOCK", "ERROR", "WARN", "INFO":
		if p.Severity != "" {
			p.Severity = strings.ToUpper(p.Severity)
		}
	default:
		notes = append(notes, fmt.Sprintf("unknown severity %q ignored (use BLOCK|ERROR|WARN|INFO)", p.Severity))
		p.Severity = ""
	}
	return notes
}

func (p Params) query() analytics.StatsQuery {
	return analytics.StatsQuery{
		Repo:  p.Repo,
		Since: time.Now().Add(-time.Duration(p.Days) * 24 * time.Hour),
	}
}

// Result carries both renderings of one answer: Text for MCP (the model reads
// it), JSON-ready value for REST, and Notes for JSON consumers.
type Result struct {
	Text  string
	JSON  any
	Notes []string
}

// open opens the analytics DB and lazily refreshes it. An ingest failure is
// NOT fatal: existing (slightly stale) data is served with a note - matching
// the dashboard's behavior.
func (s *Service) open() (*analytics.DB, string, error) {
	db, err := analytics.Open(filepath.Join(s.PolicyRoot, "analytics.db"))
	if err != nil {
		return nil, "", fmt.Errorf("open analytics db: %w", err)
	}
	s.mu.Lock()
	due := time.Since(s.lastIngest) >= ingestEvery
	if due {
		s.lastIngest = time.Now()
	}
	s.mu.Unlock()
	if !due {
		return db, "", nil
	}
	if _, err := analytics.Ingest(db, s.PolicyRoot); err != nil {
		return db, "analytics refresh failed - serving existing data", nil
	}
	return db, "", nil
}

// repoNote returns a recovery note when p.Repo doesn't exist ("" otherwise) -
// and clears the scope so the answer still covers everything.
// The returned note is a plain sentence without trailing newline.
func (s *Service) repoNote(db *analytics.DB, p *Params) string {
	if p.Repo == "" {
		return ""
	}
	known, err := analytics.Repos(db)
	if err != nil {
		return ""
	}
	for _, r := range known {
		if r == p.Repo {
			if s.observeMode(p.Repo) {
				return observeBanner(p.Repo)
			}
			return ""
		}
	}
	note := fmt.Sprintf("repo %q not found - answered for all repos (known: %s)",
		p.Repo, strings.Join(known, ", "))
	p.Repo = ""
	return note
}

// observeBanner is the operator-facing notice that a repo runs observe=true:
// findings are recorded but pushes are never rejected, so even BLOCK-severity
// findings are relayed. The push client stays silent by design (see
// prereceive.go - an observed agent must not see the gate), so a per-repo
// report is the only place an operator learns the gate is advisory-only here.
func observeBanner(repo string) string {
	return fmt.Sprintf("⚠ OBSERVE MODE - %s records findings but NEVER blocks pushes. "+
		"BLOCK-severity findings (e.g. curl-pipe-shell) are relayed, not rejected. "+
		"Set observe=false in its gateway.toml to enforce.", repo)
}

// observeMode reports whether repo is configured observe=true. It reads only
// the `observe` key from <PolicyRoot>/<repo>/gateway.toml, so it needs no
// dependency on the gateway policy loader (no import cycle). Any read/parse
// error is treated as not-observe - the banner is advisory, never load-bearing.
func (s *Service) observeMode(repo string) bool {
	if s.PolicyRoot == "" || repo == "" {
		return false
	}
	var cfg struct {
		Observe bool `toml:"observe"`
	}
	if _, err := toml.DecodeFile(filepath.Join(s.PolicyRoot, repo, "gateway.toml"), &cfg); err != nil {
		return false
	}
	return cfg.Observe
}

// header renders the context line and any notes for text output.
// Each note is rendered as "note: <n>\n".
func header(p Params, notes []string, extra string) string {
	h := fmt.Sprintf("(gateway decision log, last %d days", p.Days)
	if p.Repo != "" {
		h += ", repo " + p.Repo
	}
	h += ")\n"
	if extra != "" {
		h += "note: " + extra + "\n"
	}
	for _, n := range notes {
		h += "note: " + n + "\n"
	}
	return h
}

// GateStats answers "what has the gate done": totals, per-repo, top frames.
func (s *Service) GateStats(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	res, err := analytics.Stats(db, p.query())
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	if !res.Consistent {
		intNote := "data integrity check failed (accepts+rejects != decisions) - operator should investigate ingest"
		notes = append(notes, intNote)
		b.WriteString("warning: " + intNote + "\n")
	}
	fmt.Fprintf(&b, "decisions: %d, accepted: %d, rejected: %d, repos: %d\n",
		res.Decisions, res.Accepts, res.Rejects, res.Repos)
	if len(res.PerRepo) > 0 {
		b.WriteString("per repo:\n")
		for _, r := range res.PerRepo {
			fmt.Fprintf(&b, "  %s - %d decisions, %d rejected\n", r.Repo, r.Decisions, r.Rejects)
		}
	}
	if len(res.TopFrames) > 0 {
		b.WriteString("top rules:\n")
		for _, f := range res.TopFrames {
			fmt.Fprintf(&b, "  %s (%s) - %d findings\n", f.FrameID, f.Severity, f.Count)
		}
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: res, Notes: notes}, nil
}

// BounceRate answers "where does code bounce back the most".
func (s *Service) BounceRate(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	out, err := analytics.BounceRate(db, p.query(), p.MinPushes)
	if err != nil {
		return Result{}, err
	}
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	if len(out) == 0 {
		fmt.Fprintf(&b, "no repo reached %d pushes in the window\n", p.MinPushes)
	}
	for _, r := range out {
		fmt.Fprintf(&b, "%s - %.0f%% bounce (%d of %d pushes rejected)\n",
			r.Repo, r.Rate*100, r.Rejects, r.Decisions)
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: out, Notes: notes}, nil
}

// TopRules answers "which rules fire most", optionally filtered by severity.
func (s *Service) TopRules(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	res, err := analytics.Stats(db, p.query())
	if err != nil {
		return Result{}, err
	}
	filtered := res.TopFrames[:0:0]
	for _, f := range res.TopFrames {
		if p.Severity == "" || strings.EqualFold(p.Severity, f.Severity) {
			filtered = append(filtered, f)
		}
	}
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	n := 0
	for _, f := range filtered {
		fmt.Fprintf(&b, "%s (%s) - %d findings\n", f.FrameID, f.Severity, f.Count)
		n++
	}
	if n == 0 {
		switch {
		case res.Decisions > 0 && p.Severity != "":
			fmt.Fprintf(&b, "no %s findings - %d push(es) gated, none at this severity ✓\n", strings.ToLower(p.Severity), res.Decisions)
		case res.Decisions > 0:
			fmt.Fprintf(&b, "no findings - %d push(es) gated, all clean ✓\n", res.Decisions)
		default:
			b.WriteString("no findings - no pushes gated in this window\n")
		}
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: filtered, Notes: notes}, nil
}

// Recurring answers "what keeps coming back" for one repo.
func (s *Service) Recurring(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	out, err := analytics.Recurring(db, p.query())
	if err != nil {
		return Result{}, err
	}
	shown := out
	if len(shown) > p.Limit {
		shown = shown[:p.Limit]
	}
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	if len(out) == 0 {
		b.WriteString("no recurring findings\n")
	}
	for _, r := range shown {
		fmt.Fprintf(&b, "%s (%s) seen on %d pushes - %s\n", r.FrameID, r.Severity, r.Seen, r.Message)
	}
	if len(out) > p.Limit {
		fmt.Fprintf(&b, "… and %d more\n", len(out)-p.Limit)
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: shown, Notes: notes}, nil
}

// Decisions returns the receipts - the evidence behind any aggregate.
func (s *Service) Decisions(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	out, err := analytics.RecentDecisions(db, p.query(), p.Result, p.Limit)
	if err != nil {
		return Result{}, err
	}
	if len(out) >= p.Limit {
		notes = append(notes, fmt.Sprintf("showing the newest %d (row cap) - older decisions in this window are not shown; raise Rows, narrow the window, or pick a repo", p.Limit))
	}
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	if len(out) == 0 {
		b.WriteString("no decisions in the window\n")
	}
	for _, d := range out {
		verdict := "ACCEPTED"
		if !d.Accept {
			verdict = "REJECTED"
		}
		fmt.Fprintf(&b, "%s %s %s %s", time.Unix(d.TS, 0).UTC().Format("2006-01-02 15:04"), verdict, d.Repo, d.Refs)
		if len(d.TopFindings) > 0 {
			fmt.Fprintf(&b, " - %s", strings.Join(d.TopFindings, ", "))
		}
		b.WriteString("\n")
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: out, Notes: notes}, nil
}

// TimeSaved answers "how much debugging time has the gate prevented": distinct
// blocking findings weighted by per-frame hours-per-hit (stdlib defaults or a
// repo's [time-estimates] override). Same computation as the dashboard's
// /stats "Time saved" tab. "Actually prevented" = blocked-and-fixed (verified
// in the log); "modeled" = a conservative upper-bound estimate, not measured.
func (s *Service) TimeSaved(p Params) (Result, error) {
	notes := p.clamp()
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}
	if n := s.repoNote(db, &p); n != "" {
		notes = append(notes, n)
	}
	res := roi.PreventedTime(db, s.PolicyRoot, p.Repo, p.query().Since)
	var b strings.Builder
	b.WriteString(header(p, notes, ""))
	fmt.Fprintf(&b, "actually prevented: %.1fh (blocked and fixed) · modeled: %.1fh (conservative upper bound)\n",
		res.ActualHours, res.ModeledHours)
	if len(res.Rows) == 0 {
		b.WriteString("no blocking findings with an honest time estimate in the window\n")
	} else {
		b.WriteString("per frame:\n")
		for i, r := range res.Rows {
			if i >= p.Limit {
				fmt.Fprintf(&b, "… and %d more\n", len(res.Rows)-p.Limit)
				break
			}
			fmt.Fprintf(&b, "  %s - %.1fh actual, %.1fh modeled (%.1fh/hit, %s)\n",
				r.FrameID, r.ActualHours, r.ModeledHours, r.HoursPerHit, r.Source)
		}
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: res, Notes: notes}, nil
}

// WhatChanged answers "what changed / where to look": recent commits in a repo
// (or all repos) from the authoritative bare clones, each push-tip commit
// tagged with the gate's verdict. Read-only: git log only.
func (s *Service) WhatChanged(p Params) (Result, error) {
	notes := p.clamp()
	if s.ReposRoot == "" {
		return Result{Text: "repo browsing unavailable (no repos-root configured)", JSON: []any{}, Notes: notes}, nil
	}
	// Small models often pass a repository name as `query` instead of `repo`.
	// If `query` exactly names a known repo (and no repo was given), treat it
	// as the repo - same recovery philosophy as docs_search collections.
	if p.Repo == "" && p.Query != "" {
		for _, a := range s.bareRepos() {
			if a == p.Query {
				p.Repo = p.Query
				notes = append(notes, fmt.Sprintf("interpreted query %q as the repository name", p.Query))
				p.Query = ""
				break
			}
		}
	}
	reqRepo := p.Repo
	repos, rnote := s.resolveRepos(p.Repo)
	if rnote != "" {
		notes = append(notes, rnote)
	}
	db, ingNote, err := s.open()
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if ingNote != "" {
		notes = append(notes, ingNote)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	opts := gitlog.Options{
		Since: fmt.Sprintf("%d days ago", p.Days),
		Path:  p.Path, Grep: p.Query, Limit: p.Limit,
	}

	type commitOut struct {
		Repo    string             `json:"repo"`
		Commit  gitlog.Commit      `json:"commit"`
		Verdict *analytics.Verdict `json:"verdict,omitempty"`
	}
	var all []commitOut
	var capped []string // repos whose git log hit the row cap (older commits hidden)
	for _, repo := range repos {
		commits, err := logFn(ctx, filepath.Join(s.ReposRoot, repo+".git"), opts)
		if err != nil {
			// Surface git's own message (first line) so failures are diagnosable.
			msg := err.Error()
			if i := strings.IndexByte(msg, '\n'); i >= 0 {
				msg = msg[:i]
			}
			notes = append(notes, repo+": "+msg)
			continue
		}
		if len(commits) >= p.Limit {
			capped = append(capped, repo)
		}
		shas := make([]string, 0, len(commits))
		for _, c := range commits {
			shas = append(shas, c.SHA)
		}
		verdicts, _ := analytics.VerdictForSHAs(db, repo, shas)
		for _, c := range commits {
			co := commitOut{Repo: repo, Commit: c}
			if v, ok := verdicts[c.SHA]; ok {
				vc := v
				co.Verdict = &vc
			}
			all = append(all, co)
		}
	}

	if len(repos) == 0 {
		notes = append(notes, "no repositories found under repos-root")
	}
	if len(capped) > 0 {
		notes = append(notes, fmt.Sprintf("%d repo(s) hit the %d-commit cap (%s) - older commits in this window are not shown; raise Rows or scope to a single repo", len(capped), p.Limit, strings.Join(capped, ", ")))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "(gateway repos, last %d days", p.Days)
	if reqRepo != "" {
		b.WriteString(", repo " + reqRepo)
	}
	if p.Path != "" {
		b.WriteString(", path " + p.Path)
	}
	if p.Query != "" {
		b.WriteString(", query " + p.Query)
	}
	b.WriteString(")\n")
	for _, n := range notes {
		b.WriteString("note: " + n + "\n")
	}
	if len(all) == 0 {
		b.WriteString("no matching commits in the window\n")
	}
	multi := len(repos) > 1
	for _, co := range all {
		c := co.Commit
		sha := c.SHA
		if len(sha) > 7 {
			sha = sha[:7]
		}
		if multi {
			fmt.Fprintf(&b, "%s ", co.Repo)
		}
		// sha · author · subject - separators (with spacing) so the fields read
		// distinctly instead of running together. The date is the row's chip.
		fmt.Fprintf(&b, "%s %s · %s · %s", c.Date, sha, c.Author, c.Subject)
		if co.Verdict != nil {
			tag := "✓ accepted"
			if !co.Verdict.Accept {
				tag = "✗ rejected"
			}
			if len(co.Verdict.TopFindings) > 0 {
				tag += " - " + strings.Join(co.Verdict.TopFindings, ", ")
			}
			fmt.Fprintf(&b, "  [%s]", tag)
		}
		if len(c.Files) > 0 {
			files := c.Files
			more := ""
			if len(files) > 3 {
				files = files[:3]
				more = ", …"
			}
			fmt.Fprintf(&b, "  (%d files: %s%s)", len(c.Files), strings.Join(files, ", "), more)
		}
		b.WriteString("\n")
	}
	return Result{Text: strings.TrimRight(b.String(), "\n"), JSON: all, Notes: notes}, nil
}

// resolveRepos returns the repos to log. With repo set: validated + must have a
// bare clone, else a recovery note + the available repos. Empty: every bare
// clone under ReposRoot.
func (s *Service) resolveRepos(repo string) ([]string, string) {
	avail := s.bareRepos()
	if repo == "" {
		return avail, ""
	}
	name, err := gitlog.SafeRepoName(repo)
	if err == nil {
		for _, a := range avail {
			if a == name {
				return []string{name}, ""
			}
		}
	}
	note := fmt.Sprintf("repo %q not found - searched all repos instead (available: %s)",
		repo, strings.Join(avail, ", "))
	return avail, note
}

// bareRepos lists the <name> of every <name>.git directory under ReposRoot.
func (s *Service) bareRepos() []string {
	entries, err := os.ReadDir(s.ReposRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".git") {
			continue
		}
		// os.Stat follows symlinks (DirEntry.IsDir does NOT): a repo symlinked
		// into ReposRoot - e.g. a top-level <repo>.git pointing at a real bare
		// repo under a subdir - gates and receives pushes fine but would
		// otherwise be invisible here, since ReadDir reports the link as a
		// symlink, not a directory. Resolve it so what_changed sees it too.
		fi, err := os.Stat(filepath.Join(s.ReposRoot, e.Name()))
		if err == nil && fi.IsDir() {
			out = append(out, strings.TrimSuffix(e.Name(), ".git"))
		}
	}
	return out
}
