// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// activateBareWithCommit creates a real bare repo at <reposRoot>/_repos/<name>.git
// seeded with one commit on refs/heads/main, plus the activation symlink so
// resolveRepoBare (and thus ReconcileAll) finds it.
func activateBareWithCommit(t *testing.T, reposRoot, name string) {
	t.Helper()
	real := filepath.Join(reposRoot, "_repos", name+".git")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, ".", "init", "--bare", "-q", real)
	work := t.TempDir()
	mustGit(t, work, "init", "-q")
	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-qm", "first")
	mustGit(t, work, "push", "-q", real, "HEAD:refs/heads/main")
	if err := os.Symlink(filepath.Join("_repos", name+".git"), filepath.Join(reposRoot, name+".git")); err != nil {
		t.Fatal(err)
	}
}

// ReconcileAll records a RelayStatus per repo and returns one ReconcileResult
// per repo: a repo whose reconcile succeeds is OK with LastSuccess set; a repo
// whose reconcile fails is OK=false with its prior LastSuccess preserved.
func TestReconcileAll_recordsPerRepoStatus(t *testing.T) {
	reposRoot := t.TempDir()
	policyRoot := t.TempDir()

	// okrepo: gated bare has a commit, upstream is empty -> drift -> reconciled.
	activateBareWithCommit(t, reposRoot, "okrepo")
	upstream := t.TempDir()
	mustGit(t, ".", "init", "--bare", "-q", upstream)
	if err := (FilePolicyStore{Root: policyRoot}).Save(Policy{Repo: "okrepo", UpstreamURL: "file://" + upstream, Enabled: true}); err != nil {
		t.Fatalf("save okrepo policy: %v", err)
	}

	// failrepo: upstream path does not exist -> ls-remote fails fast (offline).
	activateBareWithCommit(t, reposRoot, "failrepo")
	if err := (FilePolicyStore{Root: policyRoot}).Save(Policy{Repo: "failrepo", UpstreamURL: "file:///nonexistent/upstream.git", Enabled: true}); err != nil {
		t.Fatalf("save failrepo policy: %v", err)
	}
	prevSuccess := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	if err := WriteRelayStatus(policyRoot, "failrepo", RelayStatus{LastSuccess: prevSuccess, OK: true}); err != nil {
		t.Fatal(err)
	}

	results, err := ReconcileAll(reposRoot, policyRoot)
	if err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	byRepo := map[string]ReconcileResult{}
	for _, r := range results {
		byRepo[r.Repo] = r
	}
	if len(byRepo) != 2 {
		t.Fatalf("want 2 results, got %d (%+v)", len(byRepo), results)
	}

	ok := byRepo["okrepo"]
	if ok.Err != nil || ok.Drifted != 1 {
		t.Fatalf("okrepo result: %+v, want Drifted=1 Err=nil", ok)
	}
	okStatus, found := ReadRelayStatus(policyRoot, "okrepo")
	if !found || !okStatus.OK || okStatus.DriftedRefs != 1 || okStatus.LastSuccess.IsZero() {
		t.Fatalf("okrepo status: %+v found=%v, want OK with LastSuccess set", okStatus, found)
	}

	fail := byRepo["failrepo"]
	if fail.Err == nil {
		t.Fatalf("failrepo should have errored, got %+v", fail)
	}
	failStatus, found := ReadRelayStatus(policyRoot, "failrepo")
	if !found || failStatus.OK || failStatus.Error == "" {
		t.Fatalf("failrepo status: %+v found=%v, want OK=false with an error", failStatus, found)
	}
	if !failStatus.LastSuccess.Equal(prevSuccess) {
		t.Fatalf("failrepo LastSuccess = %v, want preserved %v", failStatus.LastSuccess, prevSuccess)
	}
}

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
