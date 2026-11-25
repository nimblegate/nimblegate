// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// protectedBranches are the branch names blocked from being force-pushed.
var protectedBranches = []string{"main", "master", "trunk", "production", "prod"}

// NoForcePushMain blocks force-push (--force / -f) to protected branches.
func NoForcePushMain(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "git/no-force-push-main",
		Category: frames.CategoryGitSafety,
	}

	_, verb, args, ok := gitCommand(ctx)
	if !ok || verb != "push" {
		res.Outcome = engine.OutcomeSkip
		return res
	}

	hasForce := false
	for _, a := range args {
		if isForcePushArg(a) {
			hasForce = true
			break
		}
	}
	if !hasForce {
		res.Outcome = engine.OutcomePass
		return res
	}

	for _, a := range args {
		// A refspec names its destination after the colon; a leading +
		// forces that one ref (git push origin +main).
		dst := strings.TrimPrefix(a, "+")
		if _, after, found := strings.Cut(dst, ":"); found {
			dst = strings.TrimPrefix(after, "refs/heads/")
		}
		for _, pb := range protectedBranches {
			if dst == pb {
				res.Outcome = engine.OutcomeBlock
				res.Reason = "force-push to protected branch " + pb
				res.Fix = "either don't force-push, or use `nimblegate git --force-yes --reason=\"...\" push --force <args>` to record an audited bypass"
				return res
			}
		}
	}
	res.Outcome = engine.OutcomePass
	return res
}

// isForcePushArg reports whether one `git push` argument forces: --force and
// its variants and abbreviations (--forc, --force-with-lease=...,
// --force-if-includes), -f alone or bundled (-fu), or a +refspec.
func isForcePushArg(a string) bool {
	switch {
	case strings.HasPrefix(a, "+") && len(a) > 1:
		return true
	case strings.HasPrefix(a, "--"):
		name, _, _ := strings.Cut(a, "=")
		return strings.HasPrefix(name, "--force") || (len(name) >= len("--fo") && strings.HasPrefix("--force", name))
	case strings.HasPrefix(a, "-") && len(a) > 1:
		for _, c := range a[1:] {
			if c == 'f' {
				return true
			}
			if c == 'o' { // --push-option value follows
				return false
			}
		}
	}
	return false
}
