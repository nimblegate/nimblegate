// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupGitRepoWithBaseline creates a tmp dir with an initialized git repo,
// commits one "clean.js" baseline file so future diffs have a base, and
// returns the dir path.
func setupGitRepoWithBaseline(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	for _, args := range [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "clean.js"), []byte("// clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "clean.js"},
		{"git", "commit", "-q", "-m", "baseline"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatalf("setup %v: %v", args, err)
		}
	}
	return tmp
}

// TestPreCommitChangedFiles_DefaultStagedOnly - staged file appears,
// unstaged working-tree edit does NOT.
func TestPreCommitChangedFiles_DefaultStagedOnly(t *testing.T) {
	root := setupGitRepoWithBaseline(t)

	// Stage one file.
	if err := os.WriteFile(filepath.Join(root, "staged.js"), []byte("// staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.js")
	cmd.Dir = root
	_ = cmd.Run()

	// Unstaged working-tree edit on existing tracked file.
	if err := os.WriteFile(filepath.Join(root, "clean.js"), []byte("// clean modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := preCommitChangedFiles(root, false)
	if len(got) != 1 || !strings.HasSuffix(got[0], "staged.js") {
		t.Errorf("default = %v, want [staged.js only]", got)
	}
}

// TestPreCommitChangedFiles_IncludeUnstagedMergesBoth - with the flag,
// both staged and working-tree changes appear, deduped.
func TestPreCommitChangedFiles_IncludeUnstagedMergesBoth(t *testing.T) {
	root := setupGitRepoWithBaseline(t)

	if err := os.WriteFile(filepath.Join(root, "staged.js"), []byte("// staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "staged.js")
	cmd.Dir = root
	_ = cmd.Run()

	if err := os.WriteFile(filepath.Join(root, "clean.js"), []byte("// clean modified\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := preCommitChangedFiles(root, true)
	if len(got) != 2 {
		t.Fatalf("include-unstaged got %d files, want 2 (staged.js + clean.js)", len(got))
	}
	have := map[string]bool{}
	for _, p := range got {
		have[filepath.Base(p)] = true
	}
	if !have["staged.js"] || !have["clean.js"] {
		t.Errorf("merged list missing entries: %v", got)
	}
}

// TestPreCommitChangedFiles_DedupesOverlap - a file that's both staged AND
// has further working-tree edits appears once.
func TestPreCommitChangedFiles_DedupesOverlap(t *testing.T) {
	root := setupGitRepoWithBaseline(t)

	if err := os.WriteFile(filepath.Join(root, "dual.js"), []byte("// v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "dual.js")
	cmd.Dir = root
	_ = cmd.Run()

	// Stage v1, then edit on working tree (so it appears in both diffs).
	if err := os.WriteFile(filepath.Join(root, "dual.js"), []byte("// v1 → v2 in working tree\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := preCommitChangedFiles(root, true)
	count := 0
	for _, p := range got {
		if strings.HasSuffix(p, "dual.js") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dedup failed; dual.js appears %d times in %v", count, got)
	}
}

// TestE2E_CheckIncludeUnstagedFlagAccepted - end-to-end that the flag is
// wired through main → Check; exit code matches a clean project.
func TestE2E_CheckIncludeUnstagedFlagAccepted(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "check", "--trigger=pre-commit", "--include-unstaged")
	cmd.Dir = tmp
	_, err := cmd.CombinedOutput()
	// Acceptable: exit 0 (clean) or 1 (some frame fired). Exit 2 would mean
	// the flag was rejected.
	if err != nil {
		if cmd.ProcessState.ExitCode() == 2 {
			t.Errorf("--include-unstaged was rejected with exit 2 (flag parsing)")
		}
	}
}
