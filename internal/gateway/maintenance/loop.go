// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Runner is the background loop that wakes on Config.Interval and runs four
// maintenance tasks in sequence across the gateway box:
//
//  1. git gc per bare repo under reposRoot
//  2. prune expired auth sessions (if SessionSweeper wired)
//  3. remove /tmp/afgw-* orphans older than tmpOrphanMaxAge
//  4. trim deadletter JSONL to Config.DeadletterRetention per repo
//
// Spawn one per daemon; it shuts down cleanly on ctx.Done().
//
// Wiring uses pluggable Clock + GC + Sessions + EventSink interfaces so the
// loop is testable without real time, real git, real DB, or real audit
// writes. Tmp + deadletter use the filesystem directly (cheap to set up in
// tests with t.TempDir()).
type Runner struct {
	cfg            Config
	reposRoot      string
	policyRoot     string
	tmpDir         string
	gc             GC
	sessionSweeper SessionSweeper
	events         EventSink
	clock          Clock

	mu     sync.Mutex
	status Status
}

// Status is a snapshot of the loop's most recent activity, surfaced on
// /health. Zero value means "never run yet."
type Status struct {
	LastSweepAt   time.Time
	LastSweepTook time.Duration
	NextSweepAt   time.Time
	PerRepo       []RepoResult
	SweepCount    int

	LastSessionSweep SessionSweepResult
	LastTmpOrphans   TmpOrphansResult
	LastDeadletter   []DeadletterResult
	LastAudit        []AuditPruneResult
	LastEvents       EventsPruneResult
}

// RepoResult is one repo's outcome from the most recent sweep.
type RepoResult struct {
	Repo      string
	Took      time.Duration
	Err       error
	StartedAt time.Time
}

// GC runs maintenance against one bare repo. Real impl shells out to
// `git gc --auto --quiet`; tests use a stub.
type GC interface {
	Run(ctx context.Context, repoGitDir string) RepoResult
}

// EventSink records sweep activity in the gateway's events log. Real impl
// appends to <policy-root>/events.log; tests use a slice-collector.
type EventSink interface {
	Append(name string, payload map[string]any)
}

// Clock abstracts time + tickers so tests don't have to wait 168h.
type Clock interface {
	Now() time.Time
	NewTicker(d time.Duration) Ticker
}

// Ticker mirrors time.Ticker's surface so tests can drive ticks deterministically.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

// NewRunner constructs a Runner for the gc-only path (kept for compatibility
// with v1 wiring + tests that don't care about the expanded tasks). Use
// NewRunnerWithTasks to wire the full four-task sweep.
func NewRunner(cfg Config, reposRoot string, gc GC, events EventSink, clock Clock) *Runner {
	return &Runner{
		cfg:       cfg,
		reposRoot: reposRoot,
		gc:        gc,
		events:    events,
		clock:     clock,
	}
}

// NewRunnerWithTasks wires the expanded four-task sweep. policyRoot is needed
// for deadletter; tmpDir is the directory holding orphan worktrees (typically
// "/tmp"). sessionSweeper may be nil - that task is skipped.
func NewRunnerWithTasks(cfg Config, reposRoot, policyRoot, tmpDir string, gc GC, sessionSweeper SessionSweeper, events EventSink, clock Clock) *Runner {
	return &Runner{
		cfg:            cfg,
		reposRoot:      reposRoot,
		policyRoot:     policyRoot,
		tmpDir:         tmpDir,
		gc:             gc,
		sessionSweeper: sessionSweeper,
		events:         events,
		clock:          clock,
	}
}

// Run blocks until ctx is cancelled. If cfg.Enabled is false, Run returns
// immediately. Otherwise it waits one full Interval before the first sweep
// (startup shouldn't trigger heavy work), then sweeps on every subsequent
// tick.
func (r *Runner) Run(ctx context.Context) {
	if !r.cfg.Enabled {
		return
	}
	r.markNextSweep(r.clock.Now().Add(r.cfg.Interval))
	ticker := r.clock.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			r.sweep(ctx)
			r.markNextSweep(r.clock.Now().Add(r.cfg.Interval))
		}
	}
}

// Status returns a defensive copy of the current state for /health rendering.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.status
	out.PerRepo = append([]RepoResult(nil), r.status.PerRepo...)
	out.LastDeadletter = append([]DeadletterResult(nil), r.status.LastDeadletter...)
	out.LastAudit = append([]AuditPruneResult(nil), r.status.LastAudit...)
	return out
}

func (r *Runner) markNextSweep(at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status.NextSweepAt = at
}

// sweep runs all six maintenance tasks in sequence:
//
//  1. git gc per bare repo
//  2. auth session prune
//  3. /tmp/afgw-* orphan cleanup
//  4. deadletter retention rewrite per repo
//  5. decision-aware audit.log retention per repo
//  6. gateway-wide _events.jsonl retention
//
// Per-task errors are logged via EventSink + recorded in Status but never
// abort the sweep - one task failing shouldn't starve the others. Per-repo
// errors within a task have the same isolation.
func (r *Runner) sweep(ctx context.Context) {
	startedAt := r.clock.Now()
	repos := r.findBareRepos()

	r.events.Append("maintenance-sweep-start", map[string]any{
		"repo_count": len(repos),
		"interval":   r.cfg.Interval.String(),
	})

	// Task 1 - git gc per repo
	var gcResults []RepoResult
	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		res := r.gc.Run(ctx, repo)
		gcResults = append(gcResults, res)
		payload := map[string]any{
			"repo":    res.Repo,
			"took_ms": res.Took.Milliseconds(),
		}
		if res.Err != nil {
			payload["error"] = res.Err.Error()
			r.events.Append("maintenance-gc-failed", payload)
		} else {
			r.events.Append("maintenance-gc-ok", payload)
		}
	}

	// Task 2 - auth session prune (skip if no sweeper wired)
	sessionResult := runSessionSweep(r.clock.Now, r.sessionSweeper)
	if r.sessionSweeper != nil {
		payload := map[string]any{}
		if sessionResult.Err != nil {
			payload["error"] = sessionResult.Err.Error()
			r.events.Append("maintenance-sessions-failed", payload)
		} else {
			r.events.Append("maintenance-sessions-ok", payload)
		}
	}

	// Task 3 - /tmp/afgw-* orphan cleanup
	var tmpResult TmpOrphansResult
	if r.tmpDir != "" {
		tmpResult = runTmpOrphanCleanup(r.clock.Now, r.tmpDir)
		payload := map[string]any{
			"scanned": tmpResult.Scanned,
			"removed": tmpResult.Removed,
		}
		if tmpResult.Err != nil {
			payload["error"] = tmpResult.Err.Error()
			r.events.Append("maintenance-tmp-failed", payload)
		} else if tmpResult.Removed > 0 {
			r.events.Append("maintenance-tmp-cleaned", payload)
		}
	}

	// Task 4 - deadletter retention prune per repo
	var dlResults []DeadletterResult
	if r.policyRoot != "" && r.cfg.DeadletterRetention > 0 {
		dlResults = runDeadletterPrune(r.clock.Now, r.policyRoot, r.cfg.DeadletterRetention)
		for _, dr := range dlResults {
			payload := map[string]any{
				"repo":    dr.Repo,
				"scanned": dr.Scanned,
				"kept":    dr.Kept,
				"pruned":  dr.Pruned,
			}
			if dr.Err != nil {
				payload["error"] = dr.Err.Error()
				r.events.Append("maintenance-deadletter-failed", payload)
			} else if dr.Pruned > 0 {
				r.events.Append("maintenance-deadletter-pruned", payload)
			}
		}
	}

	// Task 5 - decision-aware audit.log retention per repo
	var auditResults []AuditPruneResult
	if r.policyRoot != "" && r.cfg.AuditAcceptRetention > 0 {
		auditResults = runAuditPrune(r.clock.Now, r.policyRoot, r.cfg.AuditAcceptRetention, r.cfg.AuditRejectRetention)
		for _, ar := range auditResults {
			payload := map[string]any{
				"repo":             ar.Repo,
				"scanned":          ar.Scanned,
				"pruned_accept":    ar.PrunedAccept,
				"pruned_reject":    ar.PrunedReject,
				"kept_unparseable": ar.KeptUnparseable,
			}
			if ar.Err != nil {
				payload["error"] = ar.Err.Error()
				r.events.Append("maintenance-audit-failed", payload)
			} else if ar.PrunedAccept > 0 || ar.PrunedReject > 0 {
				r.events.Append("maintenance-audit-pruned", payload)
			}
		}
	}

	// Task 6 - gateway-wide _events.jsonl retention
	var eventsResult EventsPruneResult
	if r.policyRoot != "" && r.cfg.EventsRetention > 0 {
		eventsResult = runEventsPrune(r.clock.Now, r.policyRoot, r.cfg.EventsRetention)
		payload := map[string]any{
			"scanned":          eventsResult.Scanned,
			"pruned":           eventsResult.Pruned,
			"kept_unparseable": eventsResult.KeptUnparseable,
		}
		if eventsResult.Err != nil {
			payload["error"] = eventsResult.Err.Error()
			r.events.Append("maintenance-events-failed", payload)
		} else if eventsResult.Pruned > 0 {
			r.events.Append("maintenance-events-pruned", payload)
		}
	}

	took := r.clock.Now().Sub(startedAt)
	r.events.Append("maintenance-sweep-end", map[string]any{
		"repo_count": len(repos),
		"took_ms":    took.Milliseconds(),
	})

	r.mu.Lock()
	r.status.LastSweepAt = startedAt
	r.status.LastSweepTook = took
	r.status.PerRepo = gcResults
	r.status.LastSessionSweep = sessionResult
	r.status.LastTmpOrphans = tmpResult
	r.status.LastDeadletter = dlResults
	r.status.LastAudit = auditResults
	r.status.LastEvents = eventsResult
	r.status.SweepCount++
	r.mu.Unlock()
}

// findBareRepos returns all *.git dirs directly under reposRoot. Skips files
// and any underscore-prefixed dir (e.g., _repos archive). Sorted for
// deterministic order across runs (matters for test assertions + operator-
// readable logs).
func (r *Runner) findBareRepos() []string {
	entries, err := os.ReadDir(r.reposRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue
		}
		if !strings.HasSuffix(name, ".git") {
			continue
		}
		out = append(out, filepath.Join(r.reposRoot, name))
	}
	sort.Strings(out)
	return out
}
