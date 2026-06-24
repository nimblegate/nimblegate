// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// activateRepo mirrors `gateway add`: the real bare dir at _repos/<name>.git
// under reposRoot, plus the activation symlink <name>.git -> _repos/<name>.git.
func activateRepo(t *testing.T, reposRoot, name string) string {
	t.Helper()
	real := filepath.Join(reposRoot, "_repos", name+".git")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", name+".git"), filepath.Join(reposRoot, name+".git")); err != nil {
		t.Fatal(err)
	}
	return real
}

// An active repo resolves to the canonical real bare dir - following the
// activation symlink (which DirEntry.IsDir would have reported false for).
func TestResolveRepoBare_followsActivationSymlink(t *testing.T) {
	root := t.TempDir()
	real := activateRepo(t, root, "demo")
	got, err := resolveRepoBare(root, "demo")
	if err != nil {
		t.Fatalf("resolveRepoBare: %v", err)
	}
	want, _ := filepath.EvalSymlinks(real)
	if got != want {
		t.Fatalf("resolved %q, want canonical %q", got, want)
	}
}

// Names that could traverse or hit reserved/internal paths are refused.
func TestResolveRepoBare_rejectsUnsafeNames(t *testing.T) {
	root := t.TempDir()
	activateRepo(t, root, "demo") // a real repo exists, but bad names must still fail
	for _, bad := range []string{"", ".", "..", "../escape", "a/b", "/abs", `a\b`, "_repos", ".hidden"} {
		if _, err := resolveRepoBare(root, bad); err == nil {
			t.Errorf("name %q should be refused", bad)
		}
	}
}

// A swapped/hostile activation symlink that resolves OUTSIDE the repos root
// must be refused - it cannot redirect the relay to some other location.
func TestResolveRepoBare_refusesSymlinkEscapingRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // real dir, outside root
	if err := os.Symlink(outside, filepath.Join(root, "evil.git")); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRepoBare(root, "evil"); err == nil {
		t.Fatal("a symlink resolving outside the repos root must be refused")
	}
}

// An unregistered/inactive repo (real bare exists in _repos/ but no activation
// symlink) is not relayable.
func TestResolveRepoBare_refusesInactiveRepo(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "_repos", "dormant.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveRepoBare(root, "dormant"); err == nil {
		t.Fatal("an inactive repo (no activation symlink) must be refused")
	}
}
