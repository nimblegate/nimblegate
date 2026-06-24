// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// seedPendingMarker names the per-repo flag file the add flow drops when the
// registration-time upstream mirror didn't complete (upstream unreachable, or
// an http upstream with no credential yet). Its presence drives the "Sync from
// upstream" issue in Skeleton.Verify and is removed once a sync succeeds.
const seedPendingMarker = ".seed-pending"

// SeedResult reports what SeedFromUpstream pulled into the gateway bare repo.
type SeedResult struct {
	Refs          int    // number of branch refs present after the fetch
	DefaultBranch string // upstream's default branch, mirrored onto the bare HEAD ("" if unknown)
}

// SeedFromUpstream mirrors the upstream's branches and tags into the gateway's
// bare repo, so a repo whose upstream already has history is immediately
// clone-able from the gateway - without anyone SSHing into the gateway to run
// git plumbing by hand. It is a no-op (Refs 0, no error) when the upstream is
// empty or unset, which is the normal case for a brand-new repo.
//
// cred, when non-empty and the URL is http(s), is injected as a token the same
// way Relay does; ssh URLs authenticate via the gateway host's key. After the
// fetch, the bare repo's HEAD is repointed at the upstream's default branch so
// `git clone` from the gateway checks out files instead of landing on a
// dangling ref. Errors never leak the credential - output is redacted the same
// way Relay's is.
func SeedFromUpstream(bareDir, upstreamURL, cred string) (SeedResult, error) {
	if strings.TrimSpace(upstreamURL) == "" {
		return SeedResult{}, nil
	}
	url := authedURL(upstreamURL, cred)
	// Mirror heads + tags into the bare. Force (+) so a re-run converges on the
	// upstream's commits rather than failing on non-fast-forward.
	if out, err := gitBare(bareDir, "fetch", url,
		"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*").CombinedOutput(); err != nil {
		msg := redactURLUserinfo(redactCred(string(out), cred))
		return SeedResult{}, fmt.Errorf("seed from %s failed: %w\n%s",
			redactURLUserinfo(upstreamURL), err, msg)
	}

	res := SeedResult{Refs: countHeads(bareDir)}
	// Point HEAD at the upstream's default branch so clones check out cleanly.
	if b := remoteDefaultBranch(upstreamURL, cred); b != "" && headExists(bareDir, b) {
		_ = gitBare(bareDir, "symbolic-ref", "HEAD", "refs/heads/"+b).Run()
		res.DefaultBranch = b
	} else {
		res.DefaultBranch = fixDanglingHEAD(bareDir)
	}
	return res, nil
}

// gitBare builds a git command scoped to the bare repo at bareDir, with
// safe.directory set so the call succeeds even when the dashboard process
// doesn't own the git-managed bare repos (they're owned by the git user; the
// dashboard runs as a different user). Same guard gitlog.go uses. ls-remote and
// other remote-only calls don't need this and build their own command.
func gitBare(bareDir string, args ...string) *exec.Cmd {
	full := append([]string{"-c", "safe.directory=" + bareDir, "-C", bareDir}, args...)
	return exec.Command("git", full...)
}

// SeedAtRegistration runs SeedFromUpstream for a freshly-registered repo and
// owns the .seed-pending marker: on failure it drops the marker (so /repos
// offers a one-click "Sync from upstream") and returns the error for logging;
// on success it clears any stale marker. Registration must NOT be failed on its
// error - a transient upstream blip shouldn't block onboarding, and the Sync
// action recovers it.
func SeedAtRegistration(policyRoot, reposRoot, repo, upstreamURL, cred string) (SeedResult, error) {
	bare := filepath.Join(reposRoot, repo+".git")
	res, err := SeedFromUpstream(bare, upstreamURL, cred)
	marker := filepath.Join(policyRoot, repo, seedPendingMarker)
	if err != nil {
		_ = os.WriteFile(marker, nil, 0o644)
		return res, err
	}
	if rmErr := os.Remove(marker); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
		return res, nil // marker cleanup is best-effort; the seed itself succeeded
	}
	return res, nil
}

// countHeads returns how many branch refs the bare repo has.
func countHeads(bareDir string) int {
	out, err := gitBare(bareDir, "for-each-ref", "--format=%(refname)", "refs/heads/").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// headExists reports whether refs/heads/<branch> is present in the bare.
func headExists(bareDir, branch string) bool {
	return gitBare(bareDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// remoteDefaultBranch asks the upstream which branch HEAD points at, via
// ls-remote --symref. Returns "" if it can't be determined.
func remoteDefaultBranch(upstreamURL, cred string) string {
	url := authedURL(upstreamURL, cred)
	out, err := exec.Command("git", "ls-remote", "--symref", url, "HEAD").Output()
	if err != nil {
		return ""
	}
	// Line shape: "ref: refs/heads/main\tHEAD"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ref:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return strings.TrimPrefix(fields[1], "refs/heads/")
			}
		}
	}
	return ""
}

// fixDanglingHEAD ensures the bare's HEAD points at an existing branch so a
// clone checks something out. Keeps the current HEAD if it already resolves;
// otherwise prefers main, then master, then the first head. Returns the chosen
// branch ("" if the repo has no heads at all).
func fixDanglingHEAD(bareDir string) string {
	out, _ := gitBare(bareDir, "symbolic-ref", "HEAD").Output()
	cur := strings.TrimPrefix(strings.TrimSpace(string(out)), "refs/heads/")
	if cur != "" && headExists(bareDir, cur) {
		return cur
	}
	for _, b := range []string{"main", "master"} {
		if headExists(bareDir, b) {
			_ = gitBare(bareDir, "symbolic-ref", "HEAD", "refs/heads/"+b).Run()
			return b
		}
	}
	first, err := gitBare(bareDir, "for-each-ref", "--count=1", "--format=%(refname:short)", "refs/heads/").Output()
	if err == nil {
		if b := strings.TrimSpace(string(first)); b != "" {
			_ = gitBare(bareDir, "symbolic-ref", "HEAD", "refs/heads/"+b).Run()
			return b
		}
	}
	return ""
}
