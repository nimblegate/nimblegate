// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// NoBypassPreCommit blocks `git commit --no-verify` (and the short form
// `-n`). `--no-verify` is git's escape hatch to skip the pre-commit hook
// - which is also how nimblegate runs its pre-commit-trigger frames.
// Allowing silent bypass defeats the load-bearing guarantee.
//
// The shim already routes `git commit --no-verify` through `nimblegate
// git`; this frame is what then BLOCKs the operation. Together they
// close the silent-bypass route.
//
// If a bypass is genuinely needed, the user supplies `--force-yes
// --reason="..."` which records the override in the audit log - same
// pattern as every other gate.
func NoBypassPreCommit(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "git/no-bypass-pre-commit",
		Category: frames.CategoryGitSafety,
	}
	cmd := ctx.Command
	// Only relevant for `git commit` invocations. The shim already
	// routes here only when --no-verify is present, but this frame can
	// also fire from CLI invocations (`nimblegate git commit --no-verify`)
	// so we re-check the args here.
	if !strings.HasPrefix(cmd, "git commit") {
		res.Outcome = engine.OutcomePass
		return res
	}
	for _, f := range strings.Fields(cmd) {
		if f == "--no-verify" || f == "-n" {
			res.Outcome = engine.OutcomeBlock
			res.Reason = "git commit invoked with --no-verify (or -n) - this skips the pre-commit hook, including all nimblegate gates. Silent bypass is what this frame exists to prevent."
			res.Fix = "remove --no-verify from your commit command. If the bypass is genuinely needed for this one commit (the hook is broken, an emergency, etc.), record it explicitly:\n  nimblegate git --force-yes --reason=\"specific-justification\" commit --no-verify ...\nThis logs the bypass in the audit log. Vague reasons (\"test\", \"fix\") will be flagged by `nimblegate audit analyze`."
			res.Hits = []engine.Hit{{File: "(commit command)", Line: 0, Label: "git commit --no-verify"}}
			return res
		}
	}
	res.Outcome = engine.OutcomePass
	return res
}
