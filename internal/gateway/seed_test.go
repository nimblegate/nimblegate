// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git runs a git command in dir with deterministic identity, failing the test
// on error. Used to build throwaway upstream repos for the seed tests.
func gitT(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{
		"-c", "user.email=t@example.com",
		"-c", "user.name=test",
		"-c", "commit.gpgsign=false",
		"-c", "init.defaultBranch=main",
	}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// makeUpstream builds a bare repo with two branches (main, feature) and HEAD
// pointing at main, then returns a file:// URL to it.
func makeUpstream(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# up\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, work, "add", "-A")
	gitT(t, work, "commit", "-q", "-m", "init")
	gitT(t, work, "branch", "feature")

	bare := filepath.Join(root, "up.git")
	gitT(t, root, "clone", "-q", "--bare", work, bare)
	return "file://" + bare
}

func newBare(t *testing.T) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "gw.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	return bare
}

func TestSeedFromUpstream_mirrorsBranchesAndSetsHEAD(t *testing.T) {
	up := makeUpstream(t)
	bare := newBare(t)

	res, err := SeedFromUpstream(bare, up, "")
	if err != nil {
		t.Fatalf("SeedFromUpstream: %v", err)
	}
	if res.Refs != 2 {
		t.Errorf("Refs = %d; want 2 (main + feature)", res.Refs)
	}
	if res.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q; want main", res.DefaultBranch)
	}
	if !headExists(bare, "main") || !headExists(bare, "feature") {
		t.Error("expected both main and feature heads in the bare")
	}
	// HEAD must resolve to main so a clone checks out files.
	out, _ := exec.Command("git", "--git-dir", bare, "symbolic-ref", "HEAD").Output()
	if got := string(out); got != "refs/heads/main\n" {
		t.Errorf("HEAD = %q; want refs/heads/main", got)
	}
}

func TestSeedFromUpstream_emptyUpstreamIsNoOp(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.git")
	if out, err := exec.Command("git", "init", "--bare", "-q", empty).CombinedOutput(); err != nil {
		t.Fatalf("init empty bare: %v\n%s", err, out)
	}
	bare := newBare(t)

	res, err := SeedFromUpstream(bare, "file://"+empty, "")
	if err != nil {
		t.Fatalf("SeedFromUpstream on empty upstream: %v", err)
	}
	if res.Refs != 0 {
		t.Errorf("Refs = %d; want 0 for empty upstream", res.Refs)
	}
}

func TestSeedFromUpstream_blankURL(t *testing.T) {
	res, err := SeedFromUpstream(newBare(t), "  ", "")
	if err != nil {
		t.Fatalf("blank URL should be a no-op, got: %v", err)
	}
	if res.Refs != 0 {
		t.Errorf("Refs = %d; want 0", res.Refs)
	}
}

func TestSeedAtRegistration_marksPendingOnFailureClearsOnSuccess(t *testing.T) {
	policyRoot := t.TempDir()
	reposRoot := t.TempDir()
	repo := "demo"
	if err := os.MkdirAll(filepath.Join(policyRoot, repo), 0o755); err != nil {
		t.Fatal(err)
	}
	// A bare must exist at reposRoot/<repo>.git for the fetch to target.
	bare := filepath.Join(reposRoot, repo+".git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	marker := filepath.Join(policyRoot, repo, seedPendingMarker)

	// Failure: unreachable upstream → marker written, error returned.
	if _, err := SeedAtRegistration(policyRoot, reposRoot, repo, "file:///nonexistent/nope.git", ""); err == nil {
		t.Fatal("expected error for unreachable upstream")
	}
	if !skeletonFileExists(marker) {
		t.Fatal("expected .seed-pending marker after a failed seed")
	}

	// Success: real upstream → marker cleared, no error.
	up := makeUpstream(t)
	if _, err := SeedAtRegistration(policyRoot, reposRoot, repo, up, ""); err != nil {
		t.Fatalf("SeedAtRegistration success path: %v", err)
	}
	if skeletonFileExists(marker) {
		t.Error("expected .seed-pending marker removed after a successful seed")
	}
}

func TestSkeletonVerify_flagsSeedPending(t *testing.T) {
	policyRoot := t.TempDir()
	reposRoot := t.TempDir()
	repo := "demo"

	// Minimal wired repo: bare + gateway.toml (with upstream) + appframes.toml.
	bare := filepath.Join(reposRoot, repo+".git")
	if out, err := exec.Command("git", "init", "--bare", "-q", bare).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, out)
	}
	repoCfg := filepath.Join(policyRoot, repo)
	if err := os.MkdirAll(repoCfg, 0o755); err != nil {
		t.Fatal(err)
	}
	store := FilePolicyStore{Root: policyRoot}
	if err := writeGatewayTOML(store.file(repo), Policy{
		Repo: repo, UpstreamURL: "git@host:owner/demo.git", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(framePolicyPath(policyRoot, repo), defaultAppframesTOML(), 0o644); err != nil {
		t.Fatal(err)
	}

	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}

	// No marker → no seed issue.
	issues, err := sk.Verify(repo)
	if err != nil {
		t.Fatal(err)
	}
	if hasSeedIssue(issues) {
		t.Error("did not expect a sync-from-upstream issue without the marker")
	}

	// Drop the marker → the issue appears with the sync repair op.
	if err := os.WriteFile(filepath.Join(repoCfg, seedPendingMarker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	issues, err = sk.Verify(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSeedIssue(issues) {
		t.Errorf("expected a sync-from-upstream issue once the marker exists; got %+v", issues)
	}
}

func hasSeedIssue(issues []SkeletonIssue) bool {
	for _, iss := range issues {
		if iss.Repair == "sync-from-upstream" {
			return true
		}
	}
	return false
}
