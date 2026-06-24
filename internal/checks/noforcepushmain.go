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

	args := strings.Fields(ctx.Command)
	if len(args) < 2 || args[0] != "git" || args[1] != "push" {
		res.Outcome = engine.OutcomeSkip
		return res
	}

	hasForce := false
	for _, a := range args[2:] {
		if a == "--force" || a == "-f" || a == "--force-with-lease" {
			hasForce = true
			break
		}
	}
	if !hasForce {
		res.Outcome = engine.OutcomePass
		return res
	}

	for _, a := range args[2:] {
		for _, pb := range protectedBranches {
			if a == pb {
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
