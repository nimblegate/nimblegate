// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package gitlog reads commit history from a bare git repo for the agent
// "what changed" tool. Pure read: it runs `git log` only, via fixed argv (no
// shell), with the dir and all filters passed as separate arguments.
package gitlog

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// runner runs a command and returns stdout; overridable in tests.
var runner = func(ctx context.Context, name string, args ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, name, args...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git log failed: %s", msg)
	}
	return stdout.String(), nil
}

// Commit is one commit's trail metadata.
type Commit struct {
	SHA     string   `json:"sha"`
	Date    string   `json:"date"`
	Author  string   `json:"author"`
	Subject string   `json:"subject"`
	Files   []string `json:"files"`
}

// Options filter the log. Zero values mean "no filter".
type Options struct {
	Since string // git --since value, e.g. "30 days ago"
	Path  string // pathspec; limits to commits touching it
	Grep  string // --grep keyword
	Limit int    // -n cap
}

// record/unit separators chosen so they can't appear in subjects or paths.
const (
	rs = "\x1e"
	us = "\x1f"
)

// Log returns commits from the bare repo at gitDir, newest first, with the
// given filters. Subject and field separators are control bytes so parsing is
// unambiguous even with odd subjects.
func Log(ctx context.Context, gitDir string, o Options) ([]Commit, error) {
	// safe.directory: the dashboard process may not own the bare repos (the
	// gate's hook runs git in a different context). The path is already
	// validated under the repos root, so trusting this exact dir is safe and
	// avoids git's "dubious ownership" refusal.
	// --branches logs commits across all branches (not HEAD): a bare gateway
	// repo's HEAD often points at an unborn "master" while pushes land on
	// "main", so bare `git log` would fatal with "does not have any commits
	// yet". --branches also returns empty (not fatal) for a truly empty repo.
	args := []string{"-c", "safe.directory=" + gitDir, "-C", gitDir, "log", "--branches", "--no-color", "--date=short",
		"--pretty=format:" + rs + "%H" + us + "%ad" + us + "%an" + us + "%s", "--name-only"}
	if o.Limit > 0 {
		args = append(args, "-n", fmt.Sprintf("%d", o.Limit))
	}
	if o.Since != "" {
		args = append(args, "--since="+o.Since)
	}
	if o.Grep != "" {
		args = append(args, "--grep="+o.Grep)
	}
	if o.Path != "" {
		args = append(args, "--", o.Path)
	}
	out, err := runner(ctx, "git", args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(out, rs) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		lines := strings.Split(rec, "\n")
		fields := strings.Split(lines[0], us)
		if len(fields) != 4 {
			continue
		}
		c := Commit{SHA: fields[0], Date: fields[1], Author: fields[2], Subject: fields[3]}
		for _, f := range lines[1:] {
			if f = strings.TrimSpace(f); f != "" {
				c.Files = append(c.Files, f)
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// SafeRepoName returns name if it is a safe single path segment for
// <reposRoot>/<name>.git, else an error. Rejects empty, leading ".", and any
// "/", "\", or ":" - so a repo argument can never escape the repos root.
func SafeRepoName(name string) (string, error) {
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\:`) {
		return "", fmt.Errorf("unsafe repo name %q", name)
	}
	return name, nil
}
