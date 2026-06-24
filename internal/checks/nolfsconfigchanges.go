// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const noLFSConfigDisableMarker = "appframes:disable git/no-lfsconfig-changes"

// NoLFSConfigChanges blocks any push that adds, modifies, or deletes a
// .lfsconfig file. The committed .lfsconfig controls where Git LFS uploads
// go; a one-line change can silently redirect every future binary upload
// from any dev pulling the repo to an attacker-controlled LFS endpoint.
// See frame markdown at internal/stdlib/frames/git-safety/no-lfsconfig-changes.md
// for the full attack writeup.
//
// Scope: triggers on pre-commit and CLI. Pre-receive is implicit (gateway
// runs the CLI-equivalent path against the materialized tree). The frame
// fires on basename match - `.lfsconfig` at any depth - because git-lfs
// reads the nearest one walking up from the working dir.
//
// Override: include the disable marker in any file in the staged set
// (typically the commit message file). Per-commit scope.
func NoLFSConfigChanges(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "git/no-lfsconfig-changes",
		Category: frames.CategoryGitSafety,
	}

	if len(ctx.ChangedFiles) == 0 {
		// No staged set - nothing to gate. Pre-commit + empty stage is PASS
		// per the file-scan scope contract; CLI with no staged set is SKIP.
		if ctx.Trigger == engine.TriggerCLI {
			res.Outcome = engine.OutcomeSkip
			res.Reason = "no changed files; rule evaluates against staged/working-tree changes"
			return res
		}
		res.Outcome = engine.OutcomePass
		return res
	}

	// First scan for a commit-scoped override. The marker can land in any
	// file in the changed set (most commonly the commit message itself,
	// captured via .git/COMMIT_EDITMSG by the pre-commit hook), so check
	// every file's bounded content.
	for _, file := range ctx.ChangedFiles {
		if data, ok := ReadFileBounded(file, DefaultMaxFileBytes); ok {
			if strings.Contains(string(data), noLFSConfigDisableMarker) {
				res.Outcome = engine.OutcomeSkip
				res.Reason = "override marker present"
				return res
			}
		}
	}

	var hits []string
	var hitsStruct []engine.Hit
	for _, file := range ctx.ChangedFiles {
		if filepath.Base(file) != ".lfsconfig" {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		hits = append(hits, file)
		hitsStruct = append(hitsStruct, engine.Hit{
			File:  file,
			Label: ".lfsconfig staged for change",
		})
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}

	res.Outcome = engine.OutcomeBlock
	res.Reason = fmt.Sprintf(".lfsconfig change in: %s", strings.Join(hits, ", "))
	res.Fix = "If this redirection is intentional (e.g., moving to a new LFS server), add the disable marker to the commit message:\n  appframes:disable git/no-lfsconfig-changes - reason: <why>\nOtherwise, revert the .lfsconfig change - it may be a silent LFS-redirection attack."
	res.Hits = hitsStruct
	return res
}
