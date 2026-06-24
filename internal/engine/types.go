// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package engine is the runtime: frame registry, parallel check runner,
// output formatter, and audit log writer.
package engine

import (
	"fmt"
	"time"

	"nimblegate/internal/frames"
)

// Trigger identifies which surface invoked the engine.
type Trigger string

const (
	TriggerCLI       Trigger = "cli"
	TriggerPreCommit Trigger = "pre-commit"
	TriggerGitWrap   Trigger = "git-wrap"
	TriggerWatcher   Trigger = "watcher"
	TriggerServer    Trigger = "server"
)

// CheckContext is everything a frame needs to evaluate itself.
// It is passed by value to every frame's check function.
type CheckContext struct {
	Trigger       Trigger
	ProjectRoot   string
	WorkingDir    string
	Command       string
	ChangedFiles  []string
	CurrentBranch string

	// ExcludedDirs is the list of directory-name segments that file-scanning
	// check functions should skip. Populated by the engine from the project
	// config ([scan].exclude) or built-in defaults. Apply via
	// checks.IsExcluded(path, ProjectRoot, ExcludedDirs).
	//
	// This is the original segment-name exclusion. New code should consult
	// IgnorePath (below) instead - it composes ExcludedDirs with the newer
	// [scan] exclude-paths globs + discovered .appframes-ignore markers.
	ExcludedDirs []string

	// IgnorePath, when non-nil, returns true when a path (absolute) should
	// be skipped by file-scanning checks. Encapsulates the full ignore
	// pipeline: [scan].exclude (segments) + [scan].exclude-paths (globs)
	// + .appframes-ignore marker files anywhere in the tree.
	//
	// Checks should call this BEFORE opening the file. When nil (older
	// callers / tests that build CheckContext by hand), checks should
	// fall back to the segment-only behavior via checks.IsExcluded.
	IgnorePath func(absPath string) bool
}

// CheckOutcome is the verdict from running one frame.
type CheckOutcome string

const (
	OutcomePass  CheckOutcome = "PASS"
	OutcomeBlock CheckOutcome = "BLOCK"
	OutcomeWarn  CheckOutcome = "WARN"
	OutcomeInfo  CheckOutcome = "INFO"
	OutcomeSkip  CheckOutcome = "SKIP"
	OutcomeError CheckOutcome = "ERROR"
)

// Hit is a structured finding location, optionally populated by frames
// that produce file:line findings. Used by the dedup pass (V0.5) to
// collapse rows where multiple frames hit the same scope, and by the
// whitelist suppression pass to drop individual hits without
// suppressing the whole frame.
//
// Frames are NOT required to populate Hits - checks that report a
// command-level or project-level finding leave it empty and render as
// before. Adding Hits is opt-in alongside `dedup-key:` in the frame's
// frontmatter.
type Hit struct {
	File  string // absolute or project-relative path
	Line  int    // 1-based; 0 = file-level (no specific line)
	Label string // human-readable detection name (already sanitized)
}

// Format returns the canonical "file:line - label" string. Used to
// rebuild a frame's Reason after whitelist suppression filters Hits.
func (h Hit) Format() string {
	if h.Line > 0 {
		return fmt.Sprintf("%s:%d - %s", h.File, h.Line, h.Label)
	}
	return fmt.Sprintf("%s:0 - %s", h.File, h.Label)
}

// CheckResult captures one frame's result against one CheckContext.
//
// Hits and DedupKey were added in V0.5 to support cross-frame dedup.
// DedupKey is propagated from frontmatter by the runner - frames don't
// set it directly.
type CheckResult struct {
	FrameID   string
	Category  frames.Category
	Outcome   CheckOutcome
	Reason    string
	Fix       string
	Override  bool
	Timestamp time.Time

	Hits     []Hit
	DedupKey string
}

// CheckFunc is the signature every frame's check implementation must have.
type CheckFunc func(ctx CheckContext) CheckResult
