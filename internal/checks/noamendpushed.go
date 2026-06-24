// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"os/exec"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// NoAmendPushedCommits blocks `git commit --amend` when HEAD has
// already been pushed to origin. See
// internal/stdlib/frames/git/no-amend-pushed-commits.md for the
// full design + override mechanism.
//
// Trigger: git-wrap only. The pre-commit hook fires AFTER the amend is
// in-flight, by which point we'd be intervening too late; intercepting
// at the wrap layer is the only point where blocking is meaningful.
func NoAmendPushedCommits(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "git/no-amend-pushed-commits",
		Category: frames.CategoryGitSafety,
	}

	args := strings.Fields(ctx.Command)
	if len(args) < 2 || args[0] != "git" || args[1] != "commit" {
		res.Outcome = engine.OutcomeSkip
		return res
	}

	hasAmend := false
	for _, a := range args[2:] {
		if a == "--amend" {
			hasAmend = true
			break
		}
		// Combined short forms - `--amend` doesn't have a short equivalent
		// and there's no risk of accidentally matching as part of another
		// flag, so a literal string compare is sufficient.
	}
	if !hasAmend {
		res.Outcome = engine.OutcomePass
		return res
	}

	// Get the current HEAD. Errors here (not a git repo, detached HEAD,
	// empty repo) all fall through to PASS - git itself will reject the
	// amend before any damage happens.
	head, err := gitOutput(ctx.ProjectRoot, "rev-parse", "HEAD")
	if err != nil {
		res.Outcome = engine.OutcomePass
		return res
	}
	head = strings.TrimSpace(head)
	if head == "" {
		res.Outcome = engine.OutcomePass
		return res
	}

	// Ask git which remote branches contain this commit. Anything
	// matching `origin/<name>` means the commit has already been
	// published; amending it would rewrite shared history.
	remotes, err := gitOutput(ctx.ProjectRoot, "branch", "-r", "--contains", head)
	if err != nil {
		// No remote tracking branches yet, or other transient git error.
		// Conservative default: allow the amend (the legitimate case
		// "first commit on a new local branch" must remain ergonomic).
		res.Outcome = engine.OutcomePass
		return res
	}

	var pushedTo []string
	for _, line := range strings.Split(remotes, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// `git branch -r --contains` may list "origin/HEAD -> origin/main"
		// for detached symbolic refs. Strip the arrow form to its actual
		// target name only.
		if i := strings.Index(line, " -> "); i >= 0 {
			line = line[i+4:]
		}
		if strings.HasPrefix(line, "origin/") {
			pushedTo = append(pushedTo, line)
		}
	}
	if len(pushedTo) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}

	short := head
	if len(short) > 7 {
		short = short[:7]
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = fmt.Sprintf(
		"HEAD (%s) is already on %s - amending rewrites history other collaborators have pulled",
		short, strings.Join(pushedTo, ", "),
	)
	res.Fix = "either don't amend, or `git commit --fixup HEAD` + interactive rebase locally only; for an audited bypass: `nimblegate git --force-yes --reason=\"...\" commit --amend`"
	return res
}

// gitOutput runs `git <args...>` in projectRoot and returns stdout.
// Stderr is captured so genuine failures don't pollute the terminal
// during a normal `nimblegate git commit --amend` (which still needs
// to produce its own clean output if the check passes).
func gitOutput(projectRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
