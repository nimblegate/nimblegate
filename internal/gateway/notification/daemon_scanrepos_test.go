// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanRepos_followsSymlinkedRepos guards the regression where the daemon
// skipped every registered repo: registered repos are activation symlinks
// (<policy-root>/<name> -> _repos/<name>), and DirEntry.IsDir() is Lstat-based,
// so the symlink was treated as a non-dir and its queue never drained.
func TestScanRepos_followsSymlinkedRepos(t *testing.T) {
	root := t.TempDir()
	// Activation layout: real lib dir under _repos + a symlink at the top level.
	if err := os.MkdirAll(filepath.Join(root, "_repos", "gw"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "gw"), filepath.Join(root, "gw")); err != nil {
		t.Fatal(err)
	}
	// A plain (non-symlink) repo dir should still be picked up.
	if err := os.MkdirAll(filepath.Join(root, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}

	repos, err := scanRepos(root)
	if err != nil {
		t.Fatalf("scanRepos: %v", err)
	}
	got := map[string]bool{}
	for _, r := range repos {
		got[r] = true
	}
	if !got["gw"] {
		t.Error("scanRepos skipped symlinked repo 'gw' - daemon would never drain its queue")
	}
	if !got["plain"] {
		t.Error("scanRepos skipped plain repo 'plain'")
	}
	if got["_repos"] {
		t.Error("scanRepos should skip the internal _repos dir")
	}
}
