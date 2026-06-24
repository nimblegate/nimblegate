// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PreviewTree materializes the bare repo's current HEAD tip into a fresh temp
// dir and returns it plus a cleanup func. Errors if the repo has no commits.
// Reuses the gate's materializeTree. The caller scans the dir with the proposed
// regex - no policy overlay (preview shows the raw tree).
func PreviewTree(bareDir string) (dir string, cleanup func(), err error) {
	tip, err := resolveTip(bareDir)
	if err != nil {
		return "", func() {}, err
	}
	dest, err := os.MkdirTemp("", "nimblegate-preview-")
	if err != nil {
		return "", func() {}, err
	}
	if err := materializeTree(bareDir, tip, dest); err != nil {
		os.RemoveAll(dest)
		return "", func() {}, err
	}
	return dest, func() { os.RemoveAll(dest) }, nil
}

// resolveTip returns the commit the bare repo's HEAD points at.
func resolveTip(bareDir string) (string, error) {
	c := exec.Command("git", "--git-dir", bareDir, "rev-parse", "HEAD")
	out, err := c.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("no pushed tree: %s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
