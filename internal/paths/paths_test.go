// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindProjectRoot_FindsAppframesToml(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "sub", "sub2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "appframes.toml"), []byte("[project]\nname='r'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := FindProjectRoot(filepath.Join(repo, "sub", "sub2"))
	if err != nil {
		t.Fatalf("FindProjectRoot error: %v", err)
	}
	gotAbs, _ := filepath.Abs(root)
	wantAbs, _ := filepath.Abs(repo)
	// Resolve symlinks (macOS /private/tmp etc.) for stable comparison.
	gotResolved, _ := filepath.EvalSymlinks(gotAbs)
	wantResolved, _ := filepath.EvalSymlinks(wantAbs)
	if gotResolved != wantResolved {
		t.Errorf("root = %q, want %q", gotResolved, wantResolved)
	}
}

func TestFindProjectRoot_NoTomlReturnsError(t *testing.T) {
	tmp := t.TempDir()
	_, err := FindProjectRoot(tmp)
	if err == nil {
		t.Fatal("expected error when no appframes.toml in ancestry")
	}
}
