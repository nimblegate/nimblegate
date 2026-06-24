// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ScanFirstPush extracts HEAD of the given bare repo into a tmpdir, shells
// out to <selfExe> scan <tmp> --recommend-json, and writes the JSON output to
// <policyRoot>/<repo>/scan-recommendation.json. Best-effort from the
// post-receive caller's perspective: returns an error but the caller logs and
// continues so the push is never blocked. The caller resolves the bare path -
// the post-receive hook runs with a relative GIT_DIR and cwd inside the bare,
// so any reposRoot-derivation has to happen at the entry point, not here.
func ScanFirstPush(bare, repo, policyRoot, selfExe string) error {
	tmp, err := os.MkdirTemp("", "nimblegate-scan-")
	if err != nil {
		return fmt.Errorf("mkdir tmp: %w", err)
	}
	defer os.RemoveAll(tmp)

	// Resolve a ref to archive: prefer symbolic HEAD, fall back to the first
	// branch under refs/heads/. On a fresh bare repo the symbolic HEAD may
	// point to a branch that has no commits yet (e.g. default "master" while
	// the first push went to "main"); the fallback covers that.
	ref := resolveArchiveRef(bare)
	if ref == "" {
		return fmt.Errorf("no ref to archive in %s", bare)
	}

	// git archive <ref> | tar -xC <tmp>
	archive := exec.Command("git", "-C", bare, "archive", ref)
	untar := exec.Command("tar", "-xC", tmp)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return fmt.Errorf("archive stdout pipe: %w", err)
	}
	untar.Stdin = pipe
	if err := archive.Start(); err != nil {
		return fmt.Errorf("archive start: %w", err)
	}
	if err := untar.Run(); err != nil {
		_ = archive.Wait()
		return fmt.Errorf("tar: %w", err)
	}
	if err := archive.Wait(); err != nil {
		return fmt.Errorf("archive wait: %w", err)
	}

	// Shell out: <selfExe> scan <tmp> --recommend-json
	out, err := exec.Command(selfExe, "scan", tmp, "--recommend-json").Output()
	if err != nil {
		return fmt.Errorf("scan exec: %w", err)
	}

	recPath := filepath.Join(policyRoot, repo, "scan-recommendation.json")
	if err := os.WriteFile(recPath, out, 0o644); err != nil {
		return fmt.Errorf("write rec: %w", err)
	}
	return nil
}

// resolveArchiveRef returns a ref suitable for `git archive`. Tries symbolic
// HEAD first (which is the natural "default branch" in the bare); falls back
// to the first ref under refs/heads/. Empty string means no archivable ref.
func resolveArchiveRef(bare string) string {
	if out, err := exec.Command("git", "-C", bare, "rev-parse", "--verify", "-q", "HEAD").Output(); err == nil && len(out) > 0 {
		return "HEAD"
	}
	out, err := exec.Command("git", "-C", bare, "for-each-ref", "--count=1", "--format=%(refname)", "refs/heads/").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
