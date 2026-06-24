// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/tasks"
	"nimblegate/internal/whitelist"
)

// isGitPush reports whether this intercepted invocation is `git push …` - the
// production boundary where the dangerous-findings gate applies.
func isGitPush(opts interceptOptions) bool {
	return opts.cmdName == "git" && len(opts.cmdArgs) > 0 && opts.cmdArgs[0] == "push"
}

// filterBlocking returns the findings that should block a push: severity BLOCK
// whose task is NOT deferred. A deferred BLOCK is a recorded "I'm knowingly
// carrying this" decision and passes the gate; WARN/INFO never block a push.
func filterBlocking(findings []tasks.Finding, ledger *tasks.Ledger) []tasks.Finding {
	var out []tasks.Finding
	for _, f := range findings {
		if f.Severity != "BLOCK" {
			continue
		}
		if t := ledger.Tasks[f.Key.ID()]; t != nil && t.Status == tasks.StatusDeferred {
			continue
		}
		out = append(out, f)
	}
	return out
}

// prodBoundaryGate runs a fresh full-tree frame check at push time and blocks
// the push if any open (non-deferred) BLOCK findings remain. Commit freely;
// dangerous code is stopped at the publish boundary. Returns 0 to allow the
// push, non-zero to block. Reads the ledger only for defer status - it does
// not mutate it (that's `nimblegate check`'s job).
func prodBoundaryGate(e *engine.Engine, root, cwd string) int {
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerCLI, // full-tree findings, not the scoped git-wrap set
		ProjectRoot:   root,
		WorkingDir:    cwd,
		ExcludedDirs:  e.ExcludedDirs(),
		IgnorePath:    e.IgnorePathFunc(),
		CurrentBranch: currentGitBranch(root),
	}
	results := engine.Run(e.Registry, ctx)
	for _, r := range results {
		_ = e.Audit.Write(ctx, r)
	}
	wl, err := whitelist.LoadFromProject(root, knownIDsWithLinters(e), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate git: prod-boundary gate: %v\n", err)
		return 2
	}
	filtered, _ := engine.ApplyWhitelist(results, wl, root)

	ledger, _ := tasks.Load(root) // read-only: defer status
	blocking := filterBlocking(tasks.KeysFromResults(filtered, root), ledger)
	if len(blocking) == 0 {
		return 0
	}

	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintf(os.Stderr, "⛔ push blocked: %d dangerous (BLOCK) finding(s) must be resolved before publishing:\n", len(blocking))
	for _, f := range blocking {
		loc := f.Key.File
		if loc == "" {
			loc = "(project-level)"
		} else if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.Key.File, f.Line)
		}
		fmt.Fprintf(os.Stderr, "   [BLOCK] %s: %s: %s\n", f.Key.FrameID, loc, f.Key.Label)
	}
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "   Commit freely; the gate is only at the publish boundary. To proceed:")
	fmt.Fprintln(os.Stderr, "     • fix them, or")
	fmt.Fprintln(os.Stderr, "     • `nimblegate tasks defer <id> --reason \"…\"` to knowingly carry one (recorded), or")
	fmt.Fprintln(os.Stderr, "     • `git push --force-yes --reason \"…\"` to bypass (loud + audited).")
	fmt.Fprintln(os.Stderr, "")
	return 1
}
