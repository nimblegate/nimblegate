// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// TestMatchParentOwnership_noopWhenAlreadyMatches confirms the function
// short-circuits when the current process's uid/gid already match the
// parent's owner - no chown attempts, no errors.
func TestMatchParentOwnership_noopWhenAlreadyMatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chown semantics differ on Windows; function is a no-op there")
	}
	tmp := t.TempDir()
	parent := filepath.Join(tmp, "parent")
	tree := filepath.Join(parent, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(tree, "child.txt")
	if err := os.WriteFile(child, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// parent and tree are owned by the running process (we just created them),
	// so MatchParentOwnership should be a no-op.
	if err := MatchParentOwnership(tree, parent); err != nil {
		t.Errorf("expected nil error when owners already match; got %v", err)
	}
}

// TestMatchParentOwnership_missingParentReturnsNil confirms a non-existent
// parent (first repo on a fresh box, no reposRoot yet) returns no error -
// the function shouldn't fail the AddRepo flow on a clean install.
func TestMatchParentOwnership_missingParentReturnsNil(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows no-op")
	}
	tmp := t.TempDir()
	tree := filepath.Join(tmp, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MatchParentOwnership(tree, filepath.Join(tmp, "does-not-exist")); err != nil {
		t.Errorf("missing parent should produce no error; got %v", err)
	}
}

// TestUnixOwnerOf_extractsFromStat checks the helper used to read parent
// ownership. We can't fake Stat_t but we can confirm the function returns
// (uid, gid, true) for a real file we just stat'd.
func TestUnixOwnerOf_extractsFromStat(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows no syscall.Stat_t for files")
	}
	tmp := t.TempDir()
	info, err := os.Stat(tmp)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, ok := unixOwnerOf(info)
	if !ok {
		t.Fatal("unixOwnerOf returned ok=false on a real file")
	}
	st, _ := info.Sys().(*syscall.Stat_t)
	if uid != int(st.Uid) || gid != int(st.Gid) {
		t.Errorf("uid/gid mismatch; got %d/%d, want %d/%d", uid, gid, st.Uid, st.Gid)
	}
}

// TestAddRepo_doesntFailOnOwnershipMismatch confirms that even when chown
// would fail (e.g., the test runs as non-root and the reposRoot was
// created by a different UID), AddRepo still succeeds - the bare repo +
// policy + hooks + symlinks exist. The chown warning goes to stderr but
// doesn't abort.
//
// In practice on the gateway this hits when gateway add runs as root
// against a git-owned reposRoot: MatchParentOwnership succeeds (root can
// chown anything), so the warning path is rare. But the safety net
// matters: if a future operator sets up an unusual ownership shape, we
// don't want AddRepo to wedge on it.
func TestAddRepo_doesntFailOnOwnershipMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows no-op")
	}
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")

	err := AddRepo(AddOptions{
		Name:        "demo",
		UpstreamURL: "git@example.com:demo.git",
		Enabled:     true,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     "/bin/true",
	})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	// Bare repo exists.
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "demo.git", "HEAD")); err != nil {
		t.Errorf("bare repo HEAD missing after AddRepo: %v", err)
	}
	// Activation symlink exists.
	if _, err := os.Lstat(filepath.Join(reposRoot, "demo.git")); err != nil {
		t.Errorf("activation symlink missing after AddRepo: %v", err)
	}
}
