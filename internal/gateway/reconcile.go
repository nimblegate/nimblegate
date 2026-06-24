// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// reconcileRepo re-pushes any head whose value in the gated bare repo differs
// from (or is absent at) the upstream - the recovery path for pushes the gate
// accepted but the relay never delivered (e.g. the relay service was down).
//
// Forward-only: it never deletes upstream refs (a missing-locally ref is left
// alone, since deletion is destructive and could clobber refs that exist
// upstream for other reasons). It reuses Relay, so the no-bypass guard still
// applies - it only ever pushes the bare repo's own current ref values.
// Returns the number of refs re-pushed.
func reconcileRepo(bareDir, upstreamURL, cred string) (int, error) {
	local, err := localHeads(bareDir)
	if err != nil {
		return 0, fmt.Errorf("read gated repo heads: %w", err)
	}
	remote, err := remoteHeads(authedURL(upstreamURL, cred))
	if err != nil {
		return 0, fmt.Errorf("read upstream heads: %s", redactURLUserinfo(redactCred(err.Error(), cred)))
	}
	var drift []RefUpdate
	for ref, sha := range local {
		if remote[ref] != sha {
			drift = append(drift, RefUpdate{Name: ref, OldRev: zeroRev, NewRev: sha})
		}
	}
	if len(drift) == 0 {
		return 0, nil
	}
	if err := Relay(upstreamURL, cred, bareDir, drift); err != nil {
		return 0, err
	}
	return len(drift), nil
}

// localHeads maps refs/heads/* -> sha in a bare repo.
func localHeads(gitDir string) (map[string]string, error) {
	out, err := exec.Command("git", "--git-dir", gitDir, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads/").Output()
	if err != nil {
		return nil, err
	}
	return parseRefMap(string(out)), nil
}

// remoteHeads maps refs/heads/* -> sha at a remote via ls-remote.
func remoteHeads(authedURL string) (map[string]string, error) {
	out, err := exec.Command("git", "ls-remote", "--heads", authedURL).Output()
	if err != nil {
		return nil, err
	}
	return parseRefMap(string(out)), nil
}

// parseRefMap parses "sha<ws>refname" lines (both for-each-ref's space and
// ls-remote's tab) into refname -> sha.
func parseRefMap(s string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			m[f[1]] = f[0]
		}
	}
	return m
}

// ReconcileAll reconciles every ACTIVE repo under reposRoot against its
// upstream. Best-effort per repo: an unresolvable repo or a single failed
// reconcile is skipped, not fatal. Returns the total refs re-pushed.
func ReconcileAll(reposRoot, policyRoot string) (int, error) {
	names, err := listActiveRepos(reposRoot)
	if err != nil {
		return 0, err
	}
	resolve := NewRepoResolver(reposRoot, policyRoot)
	total := 0
	for _, name := range names {
		bare, url, cred, err := resolve(name)
		if err != nil || url == "" {
			continue // unresolvable or no upstream to relay to
		}
		if n, err := reconcileRepo(bare, url, cred); err == nil {
			total += n
		}
	}
	return total, nil
}

// listActiveRepos returns the logical names of active repos under reposRoot:
// entries named <name>.git whose activation symlink resolves to a directory.
// Skips the internal _repos store and dotfiles. Symlink-aware (os.Stat, not the
// Lstat-based DirEntry.IsDir).
func listActiveRepos(reposRoot string) ([]string, error) {
	entries, err := os.ReadDir(reposRoot)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".git") || strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") {
			continue
		}
		if fi, err := os.Stat(filepath.Join(reposRoot, n)); err != nil || !fi.IsDir() {
			continue
		}
		names = append(names, strings.TrimSuffix(n, ".git"))
	}
	return names, nil
}
