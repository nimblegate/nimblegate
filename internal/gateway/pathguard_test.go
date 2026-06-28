// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "testing"

func TestSafeRepoName(t *testing.T) {
	good := []string{"myrepo", "a-b_c", "Repo123"}
	bad := []string{"", ".", "..", "../escape", "a/b", `a\b`, "/abs", "_repos", ".hidden"}
	for _, g := range good {
		if !safeRepoName(g) {
			t.Errorf("safeRepoName(%q) = false, want true", g)
		}
	}
	for _, b := range bad {
		if safeRepoName(b) {
			t.Errorf("safeRepoName(%q) = true, want false", b)
		}
	}
}

func TestLoadFramePolicyRejectsUnsafeRepo(t *testing.T) {
	if _, err := LoadFramePolicy(t.TempDir(), "../escape"); err == nil {
		t.Errorf("LoadFramePolicy with traversal repo should error")
	}
}

func TestFilePolicyStoreLoadRejectsUnsafeRepo(t *testing.T) {
	if _, err := (FilePolicyStore{Root: t.TempDir()}).Load("a/b"); err == nil {
		t.Errorf("FilePolicyStore.Load with separator repo should error")
	}
}
