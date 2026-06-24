// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// AptPurgePreview blocks `apt purge` / `apt-get purge` / `apt-get remove`
// unless `--simulate` (or `--dry-run`) is also present.
func AptPurgePreview(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "commands/apt-purge-preview",
		Category: frames.CategoryCommands,
	}

	args := strings.Fields(ctx.Command)
	if len(args) < 2 {
		res.Outcome = engine.OutcomeSkip
		return res
	}
	tool := args[0]
	if tool != "apt" && tool != "apt-get" {
		res.Outcome = engine.OutcomeSkip
		return res
	}
	verb := args[1]
	if verb != "purge" && verb != "remove" {
		res.Outcome = engine.OutcomeSkip
		return res
	}

	for _, a := range args[2:] {
		if a == "--simulate" || a == "--dry-run" || a == "-s" {
			res.Outcome = engine.OutcomePass
			return res
		}
	}

	res.Outcome = engine.OutcomeBlock
	res.Reason = "destructive `" + tool + " " + verb + "` without --simulate review"
	res.Fix = "run `" + tool + " " + verb + " --simulate ...` first; review the REMOVING: block; then `nimblegate git --force-yes --reason=\"reviewed simulate\" " + ctx.Command + "`"
	return res
}
