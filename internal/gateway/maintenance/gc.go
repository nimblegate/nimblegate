// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// ShellGC runs `git gc --auto --quiet` against a bare repo directory.
// --auto self-throttles: if the repo is below gc.auto + gc.autoPackLimit
// thresholds, git exits 0 without doing real work, so the periodic sweep
// is cheap on healthy repos.
//
// Intentionally NOT passing --aggressive or --prune=now (see
// .appframes/_design.md "Gateway maintenance loop" rationale):
//   - --aggressive trades large CPU spend for marginal pack-size gain;
//     not worth the schedule slot for a relay box.
//   - --prune=now breaks delta-base chains for incremental pushes
//     because objects shared with rejected pushes (still useful as
//     delta-base candidates) would be deleted too eagerly.
type ShellGC struct{}

// Run shells out to git in repoGitDir. Returns timing + error; never panics.
func (ShellGC) Run(ctx context.Context, repoGitDir string) RepoResult {
	start := time.Now()
	cmd := exec.CommandContext(ctx, "git", "gc", "--auto", "--quiet")
	cmd.Dir = repoGitDir
	output, err := cmd.CombinedOutput()
	took := time.Since(start)
	res := RepoResult{
		Repo:      filepath.Base(repoGitDir),
		Took:      took,
		StartedAt: start,
	}
	if err != nil {
		res.Err = fmt.Errorf("git gc: %w: %s", err, string(output))
	}
	return res
}
