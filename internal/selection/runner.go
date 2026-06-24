// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package selection implements the negative-selection runner for frames:
// each frame can declare positive (should-match) and negative (should-not-match)
// test corpora; the runner exercises the frame's CheckFunc against each and
// computes a selection grade. Inspired by thymic negative selection in the
// immune system - frames that produce false positives don't deploy.
//
// Added 2026-05-20 as part of Phase 1 Slice 2.
package selection

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/engine"
)

// CaseResult is the outcome of running one test case against a frame.
type CaseResult struct {
	Filename string              `json:"filename"`
	Kind     string              `json:"kind"` // "positive" or "negative"
	Outcome  engine.CheckOutcome `json:"outcome"`
	Passed   bool                `json:"passed"`
	Reason   string              `json:"reason,omitempty"`
}

// RunResult summarizes a full selection run for one frame.
type RunResult struct {
	FrameID         string       `json:"frame_id"`
	Cases           []CaseResult `json:"cases"`
	PositivesPassed int          `json:"positives_passed"`
	PositivesTotal  int          `json:"positives_total"`
	NegativesPassed int          `json:"negatives_passed"`
	NegativesTotal  int          `json:"negatives_total"`
	Grade           string       `json:"grade"` // passing | failing | pending
	LastRun         time.Time    `json:"last_run"`
}

// FS is the minimal filesystem interface the runner needs to read
// testdata. Both os.DirFS and embed.FS satisfy this so the runner works
// in tests (filesystem-backed) and in production (embed-backed).
type FS interface {
	fs.ReadDirFS
	fs.ReadFileFS
}

// Run executes the given CheckFunc against the testdata located in the
// given filesystem under positives/ and negatives/ directories.
//
// Layout supports three case shapes:
//   - Single-file case: positives/case-1.sh - one file copied to a
//     temp project root; check is invoked against just that file.
//   - Multi-file case: positives/case-1/ (a directory) - ALL files
//     under that directory copied to the temp project root preserving
//     relative paths; check is invoked against all copied files.
//     Used for paired-file frames like dynamic-env-declared (needs
//     .svelte + wrangler.toml) or schema-vs-code-drift (needs code +
//     schema.sql).
//   - Command-mode case: positives/case-1.command.txt - the file's
//     content is read as a command string and set on ctx.Command;
//     ChangedFiles is left empty. Used for command-shape frames like
//     no-bypass-pre-commit (looks at ctx.Command for --no-verify) or
//     rm-rf-protected-paths (looks for `rm -rf <path>` shape).
//     A directory case containing a `.command.txt` plus other files
//     is a hybrid: ctx.Command is set from the .command.txt, and the
//     other files become ChangedFiles in the temp project root.
//
// The trigger value is derived from the command's first word: `git ...`
// → TriggerGitWrap; anything else → TriggerCLI. Matches what each frame
// expects in production. Added 2026-05-20 with Phase 1 Slice 7.
//
// For each positive case: the frame is expected to BLOCK or WARN (any
// outcome other than PASS or SKIP counts as the frame correctly firing).
// For each negative case: the frame is expected to PASS or SKIP (the
// frame should NOT fire on known-good code).
//
// If both directories are empty or missing, returns a RunResult with
// Grade = "pending" and no cases. This is the default state for frames
// that haven't been retrofitted with testdata yet.
func Run(frameID string, checkFn engine.CheckFunc, testdataFS FS) (RunResult, error) {
	result := RunResult{
		FrameID: frameID,
		LastRun: time.Now().UTC(),
	}

	posCases := readCasesSorted(testdataFS, "positives")
	negCases := readCasesSorted(testdataFS, "negatives")

	if len(posCases) == 0 && len(negCases) == 0 {
		result.Grade = "pending"
		return result, nil
	}

	for _, c := range posCases {
		res, err := runCase(checkFn, testdataFS, "positives", c)
		if err != nil {
			result.Cases = append(result.Cases, CaseResult{
				Filename: c.Name,
				Kind:     "positive",
				Outcome:  engine.OutcomeError,
				Passed:   false,
				Reason:   err.Error(),
			})
			result.PositivesTotal++
			continue
		}
		passed := res.Outcome != engine.OutcomePass && res.Outcome != engine.OutcomeSkip
		result.Cases = append(result.Cases, CaseResult{
			Filename: c.Name,
			Kind:     "positive",
			Outcome:  res.Outcome,
			Passed:   passed,
			Reason:   res.Reason,
		})
		if passed {
			result.PositivesPassed++
		}
		result.PositivesTotal++
	}

	for _, c := range negCases {
		res, err := runCase(checkFn, testdataFS, "negatives", c)
		if err != nil {
			result.Cases = append(result.Cases, CaseResult{
				Filename: c.Name,
				Kind:     "negative",
				Outcome:  engine.OutcomeError,
				Passed:   false,
				Reason:   err.Error(),
			})
			result.NegativesTotal++
			continue
		}
		passed := res.Outcome == engine.OutcomePass || res.Outcome == engine.OutcomeSkip
		result.Cases = append(result.Cases, CaseResult{
			Filename: c.Name,
			Kind:     "negative",
			Outcome:  res.Outcome,
			Passed:   passed,
			Reason:   res.Reason,
		})
		if passed {
			result.NegativesPassed++
		}
		result.NegativesTotal++
	}

	if result.PositivesPassed == result.PositivesTotal && result.NegativesPassed == result.NegativesTotal {
		result.Grade = "passing"
	} else {
		result.Grade = "failing"
	}
	return result, nil
}

// caseEntry describes one test case - either a single file or a
// directory of files that get copied together (multi-file case).
type caseEntry struct {
	Name  string // display name (file basename or directory name)
	IsDir bool
}

// commandSidecarSuffix is the basename suffix marking a command-mode
// case. Two forms:
//   - positives/case-1.command.txt        (pure command-mode case)
//   - positives/case-1/<name>.command.txt (sidecar in a directory case)
const commandSidecarSuffix = ".command.txt"

func isCommandSidecar(name string) bool {
	return strings.HasSuffix(name, commandSidecarSuffix)
}

// runCase materializes one test case into a temp project dir, builds
// a CheckContext pointing at it, and runs the frame's CheckFunc. The
// temp dir is cleaned up before returning.
//
// Four case shapes: single-file (one file → ChangedFiles), multi-file
// directory (all files copied preserving paths → ChangedFiles),
// command-mode (.command.txt content → ctx.Command), and git-state-mode
// (.gitstate.yaml → temp git repo construction; sets ProjectRoot,
// WorkingDir, CurrentBranch, Command on the CheckContext).
func runCase(checkFn engine.CheckFunc, testdataFS FS, kindDir string, c caseEntry) (engine.CheckResult, error) {
	tempDir, err := os.MkdirTemp("", "nimblegate-selection-")
	if err != nil {
		return engine.CheckResult{}, fmt.Errorf("mkdir temp: %w", err)
	}
	defer os.RemoveAll(tempDir)

	var changed []string
	var command string

	if c.IsDir {
		caseRoot := filepath.Join(kindDir, c.Name)
		// First pass: check for .gitstate.yaml. If present, this is a
		// git-state-mode case - parse the spec, run the git setup, build
		// CheckContext from the gitStateResult and skip the per-file copy.
		gitStatePath := filepath.Join(caseRoot, gitStateFilename)
		if specContent, err := testdataFS.ReadFile(gitStatePath); err == nil {
			spec, err := parseGitStateSpec(specContent)
			if err != nil {
				return engine.CheckResult{}, fmt.Errorf("parse %s: %w", gitStatePath, err)
			}
			result, err := setupGitState(spec, tempDir)
			if err != nil {
				return engine.CheckResult{}, fmt.Errorf("setup git state %s: %w", c.Name, err)
			}
			// Derive trigger from command shape (git → GitWrap, else CLI).
			trigger := engine.TriggerCLI
			if strings.HasPrefix(result.Command, "git ") || result.Command == "git" {
				trigger = engine.TriggerGitWrap
			}
			ctx := engine.CheckContext{
				Trigger:       trigger,
				ProjectRoot:   result.ProjectRoot,
				WorkingDir:    result.WorkingDir,
				CurrentBranch: result.CurrentBranch,
				Command:       result.Command,
			}
			return checkFn(ctx), nil
		}
		err := fs.WalkDir(testdataFS, caseRoot, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			content, err := testdataFS.ReadFile(path)
			if err != nil {
				return err
			}
			if isCommandSidecar(path) {
				// Command sidecar inside a directory case - read content
				// as ctx.Command; do NOT add to ChangedFiles.
				command = strings.TrimSpace(string(content))
				return nil
			}
			rel, err := filepath.Rel(caseRoot, path)
			if err != nil {
				return err
			}
			dst := filepath.Join(tempDir, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, content, 0o644); err != nil {
				return err
			}
			changed = append(changed, dst)
			return nil
		})
		if err != nil {
			return engine.CheckResult{}, fmt.Errorf("walk multi-file case %s: %w", c.Name, err)
		}
	} else {
		path := filepath.Join(kindDir, c.Name)
		content, err := testdataFS.ReadFile(path)
		if err != nil {
			return engine.CheckResult{}, fmt.Errorf("read testdata %s: %w", path, err)
		}
		if isCommandSidecar(c.Name) {
			// Pure command-mode case: no files at all, just ctx.Command.
			command = strings.TrimSpace(string(content))
		} else {
			// Single-file case (existing behavior).
			dst := filepath.Join(tempDir, c.Name)
			if err := os.WriteFile(dst, content, 0o644); err != nil {
				return engine.CheckResult{}, fmt.Errorf("write %s: %w", dst, err)
			}
			changed = []string{dst}
		}
	}

	// Derive trigger from command shape. git commands route through the
	// git-wrap surface in production - match that so frames see the
	// trigger they expect.
	trigger := engine.TriggerCLI
	if strings.HasPrefix(command, "git ") || command == "git" {
		trigger = engine.TriggerGitWrap
	}

	ctx := engine.CheckContext{
		Trigger:      trigger,
		ProjectRoot:  tempDir,
		WorkingDir:   tempDir,
		Command:      command,
		ChangedFiles: changed,
	}
	return checkFn(ctx), nil
}

func readCasesSorted(testdataFS FS, dir string) []caseEntry {
	entries, err := testdataFS.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []caseEntry
	for _, e := range entries {
		out = append(out, caseEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
