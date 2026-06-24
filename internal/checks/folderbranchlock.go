// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package checks contains built-in check functions for the V0 stdlib frames.
// Each function implements engine.CheckFunc and is bound to its frame ID in
// internal/commands/builtin.go.
package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/canonical"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const folderBranchMapName = "folder-branch-map.toml"

// FolderBranchLock verifies the current working directory's expected branch
// (per folder-branch-map.toml) matches CurrentBranch.
func FolderBranchLock(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "git/folder-branch-lock",
		Category: frames.CategoryGitSafety,
	}

	tablePath := filepath.Join(ctx.ProjectRoot, ".appframes", "_canonical", folderBranchMapName)
	if _, err := os.Stat(tablePath); errors.Is(err, fs.ErrNotExist) {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "no folder-branch-map.toml; rule not applicable"
		return res
	} else if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = fmt.Sprintf("stat %s: %v", tablePath, err)
		return res
	}

	tbl, err := canonical.Load(tablePath)
	if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = err.Error()
		return res
	}
	folders, ok := tbl.Section("folders")
	if !ok {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "folder-branch-map.toml has no [folders] section"
		return res
	}

	rel, err := filepath.Rel(ctx.ProjectRoot, ctx.WorkingDir)
	if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = err.Error()
		return res
	}
	rel = filepath.ToSlash(rel)
	leaf := ""
	if rel == "." || rel == "" {
		leaf = "./"
	} else {
		parts := strings.SplitN(rel, "/", 2)
		leaf = parts[0] + "/"
	}

	expected, found := folders[leaf]
	if !found {
		res.Outcome = engine.OutcomeSkip
		res.Reason = fmt.Sprintf("folder %q not in folder-branch-map.toml", leaf)
		return res
	}

	if ctx.CurrentBranch == expected {
		res.Outcome = engine.OutcomePass
		return res
	}

	res.Outcome = engine.OutcomeBlock
	res.Reason = fmt.Sprintf("you're in %s but current branch is %q; expected %q",
		leaf, ctx.CurrentBranch, expected)
	// Two remedies - the right one depends on the project's checkout layout.
	// Single-checkout projects switch branches; multi-folder-per-branch projects
	// cd to the matching folder. Mention both.
	res.Fix = fmt.Sprintf("either `git checkout %s` (the branch %s expects) or cd into the folder mapped to branch %q",
		expected, leaf, ctx.CurrentBranch)
	return res
}
