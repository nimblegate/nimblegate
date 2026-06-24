// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
)

// reconcileRepo re-pushes refs whose gated-repo value the upstream is missing or
// behind - the recovery path for a push the gate accepted but the relay never
// delivered (e.g. the relay service was down). It must be forward-only and
// idempotent.
func TestReconcileRepo_repushesDriftThenIdempotent(t *testing.T) {
	bare, sha := makeBareWithCommit(t) // bare main = sha
	upstream := t.TempDir()
	mustGit(t, ".", "init", "--bare", "-q", upstream) // upstream empty: drift

	n, err := reconcileRepo(bare, "file://"+upstream, "")
	if err != nil {
		t.Fatalf("reconcileRepo: %v", err)
	}
	if n != 1 {
		t.Fatalf("reconciled %d refs, want 1", n)
	}
	got := strings.TrimSpace(mustGit(t, ".", "--git-dir", upstream, "rev-parse", "refs/heads/main"))
	if got != sha {
		t.Fatalf("upstream main = %s, want re-pushed %s", got, sha)
	}
	// Second run: upstream now matches, so no drift.
	n2, err := reconcileRepo(bare, "file://"+upstream, "")
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 0 {
		t.Fatalf("second reconcile found %d drift, want 0", n2)
	}
}

// listActiveRepos returns logical names of ACTIVE repos (activation symlink
// present), skipping the internal _repos store and dotfiles. Symlink-aware.
func TestListActiveRepos(t *testing.T) {
	root := t.TempDir()
	activateRepo(t, root, "alpha")
	activateRepo(t, root, "beta")
	// a real bare in _repos/ without activation must NOT be listed
	mustGit(t, ".", "init", "--bare", "-q", root+"/_repos/dormant.git")

	got, err := listActiveRepos(root)
	if err != nil {
		t.Fatalf("listActiveRepos: %v", err)
	}
	set := map[string]bool{}
	for _, n := range got {
		set[n] = true
	}
	if !set["alpha"] || !set["beta"] {
		t.Fatalf("want alpha+beta active, got %v", got)
	}
	if set["dormant"] || set["_repos"] {
		t.Fatalf("listed an inactive/internal repo: %v", got)
	}
}
