// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package-level: git-state-mode test cases.
//
// Added 2026-05-20 with Phase 1 Slice 8 to support frames that depend on
// git refs / branch state / staged-vs-working-tree diff for their checks.
//
// A case is git-state-mode when its directory contains a .gitstate.yaml
// file. The runner parses that file and runs a sequence of `git init` +
// commits + branches + remote refs in the temp project root before
// invoking the CheckFunc. Requires the `git` binary at runtime.
package selection

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// gitStateFilename is the case-directory marker for git-state-mode cases.
const gitStateFilename = ".gitstate.yaml"

// gitStateSpec is the YAML schema describing how to construct a temp git
// repo. Keep small - grow only as frames demand more shape.
type gitStateSpec struct {
	// Commits run in order. Each `branch` is checked out (or created)
	// before the commit. `files` are written, `git add -A`, then committed.
	Commits []commitSpec `yaml:"commits"`

	// CurrentBranch is the branch the runner checks out after all commits
	// + refs are set up. Defaults to the last commit's branch when empty.
	CurrentBranch string `yaml:"current_branch,omitempty"`

	// WorkingDir is the subdirectory of project root passed as
	// ctx.WorkingDir. Defaults to project root when empty.
	WorkingDir string `yaml:"working_dir,omitempty"`

	// Command is the command string passed as ctx.Command. Empty leaves
	// the field unset (frames that only look at git state don't need it).
	Command string `yaml:"command,omitempty"`

	// RemoteRefs creates refs/remotes/origin/<branch> entries pointing
	// at the named commit (HEAD or a commit's msg from Commits[]).
	RemoteRefs []remoteRefSpec `yaml:"remote_refs,omitempty"`

	// StagedFiles are written + `git add`-ed but NOT committed. Used by
	// doc-touches-with-code-style frames that look at staged diff.
	StagedFiles map[string]string `yaml:"staged_files,omitempty"`
}

type commitSpec struct {
	Branch string            `yaml:"branch"`
	Msg    string            `yaml:"msg"`
	Files  map[string]string `yaml:"files"`
}

type remoteRefSpec struct {
	Branch   string `yaml:"branch"`
	PointsTo string `yaml:"points_to"`
}

// gitStateResult is what setupGitState returns to runCase so it can
// construct the right CheckContext.
type gitStateResult struct {
	ProjectRoot   string
	WorkingDir    string
	CurrentBranch string
	Command       string
}

// setupGitState materializes the spec into the given project root.
// Returns the CheckContext field values the runner should use.
//
// Errors are returned with context but the caller is expected to fail
// the case (not the whole Run) - git setup failures usually indicate a
// malformed spec, not a runner bug.
func setupGitState(spec gitStateSpec, projectRoot string) (gitStateResult, error) {
	gitArgs := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = projectRoot
		// Reproducible identity for commits; suppress global config interference.
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=selection-runner",
			"GIT_AUTHOR_EMAIL=runner@nimblegate.local",
			"GIT_COMMITTER_NAME=selection-runner",
			"GIT_COMMITTER_EMAIL=runner@nimblegate.local",
		)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// 1. Init repo with `main` as the default branch (matches modern git).
	if out, err := gitArgs("init", "-b", "main"); err != nil {
		return gitStateResult{}, fmt.Errorf("git init: %w: %s", err, out)
	}

	// 2. Walk commits. Branch checkout (or create) + write files + add + commit.
	msgToHash := map[string]string{}
	for _, c := range spec.Commits {
		if c.Branch != "" {
			if out, err := gitArgs("checkout", "-B", c.Branch); err != nil {
				return gitStateResult{}, fmt.Errorf("checkout %s: %w: %s", c.Branch, err, out)
			}
		}
		for path, content := range c.Files {
			dst := filepath.Join(projectRoot, path)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return gitStateResult{}, fmt.Errorf("mkdir for %s: %w", path, err)
			}
			if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
				return gitStateResult{}, fmt.Errorf("write %s: %w", path, err)
			}
		}
		if out, err := gitArgs("add", "-A"); err != nil {
			return gitStateResult{}, fmt.Errorf("git add: %w: %s", err, out)
		}
		if out, err := gitArgs("commit", "-m", c.Msg, "--allow-empty"); err != nil {
			return gitStateResult{}, fmt.Errorf("commit %q: %w: %s", c.Msg, err, out)
		}
		hash, _ := gitArgs("rev-parse", "HEAD")
		msgToHash[c.Msg] = strings.TrimSpace(hash)
	}

	// 3. Remote refs - create refs/remotes/origin/<branch> via update-ref.
	for _, r := range spec.RemoteRefs {
		var hash string
		switch r.PointsTo {
		case "", "HEAD":
			h, _ := gitArgs("rev-parse", "HEAD")
			hash = strings.TrimSpace(h)
		default:
			h, ok := msgToHash[r.PointsTo]
			if !ok {
				return gitStateResult{}, fmt.Errorf("remote_ref %s points_to %q: no matching commit msg", r.Branch, r.PointsTo)
			}
			hash = h
		}
		ref := "refs/remotes/origin/" + r.Branch
		if out, err := gitArgs("update-ref", ref, hash); err != nil {
			return gitStateResult{}, fmt.Errorf("update-ref %s: %w: %s", ref, err, out)
		}
	}

	// 4. Stage files - write + add, no commit. (Used by doc-touches-style frames.)
	for path, content := range spec.StagedFiles {
		dst := filepath.Join(projectRoot, path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return gitStateResult{}, fmt.Errorf("mkdir for staged %s: %w", path, err)
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return gitStateResult{}, fmt.Errorf("write staged %s: %w", path, err)
		}
		if out, err := gitArgs("add", path); err != nil {
			return gitStateResult{}, fmt.Errorf("git add %s: %w: %s", path, err, out)
		}
	}

	// 5. Final checkout to current_branch.
	finalBranch := spec.CurrentBranch
	if finalBranch == "" && len(spec.Commits) > 0 {
		finalBranch = spec.Commits[len(spec.Commits)-1].Branch
	}
	if finalBranch != "" {
		if out, err := gitArgs("checkout", finalBranch); err != nil {
			return gitStateResult{}, fmt.Errorf("final checkout %s: %w: %s", finalBranch, err, out)
		}
	}

	workingDir := projectRoot
	if spec.WorkingDir != "" {
		workingDir = filepath.Join(projectRoot, spec.WorkingDir)
		// Ensure it exists so checks that stat it don't error.
		_ = os.MkdirAll(workingDir, 0o755)
	}

	return gitStateResult{
		ProjectRoot:   projectRoot,
		WorkingDir:    workingDir,
		CurrentBranch: finalBranch,
		Command:       spec.Command,
	}, nil
}

// parseGitStateSpec unmarshals .gitstate.yaml content into a spec.
func parseGitStateSpec(content []byte) (gitStateSpec, error) {
	var spec gitStateSpec
	if err := yaml.Unmarshal(content, &spec); err != nil {
		return gitStateSpec{}, fmt.Errorf("parse .gitstate.yaml: %w", err)
	}
	return spec, nil
}
