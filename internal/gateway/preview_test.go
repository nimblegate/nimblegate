// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// makeBareWithFiles builds a bare repo with one commit containing the given
// files, and returns the bare dir. (Named differently from worktree_test.go's
// makeBareWithCommit which returns (dir, sha) with fixed content.)
func makeBareWithFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run(work, "init", "-q", "-b", "main")
	for rel, body := range files {
		p := filepath.Join(work, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	run(work, "add", "-A")
	run(work, "commit", "-qm", "init")
	bare := filepath.Join(t.TempDir(), "r.git")
	if out, err := exec.Command("git", "clone", "-q", "--bare", work, bare).CombinedOutput(); err != nil {
		t.Fatalf("clone bare: %v\n%s", err, out)
	}
	return bare
}

func TestPreviewTree_materializesLatestTip(t *testing.T) {
	bare := makeBareWithFiles(t, map[string]string{"a.go": "TODO(no-owner)\n", "b.txt": "x\n"})
	dir, cleanup, err := PreviewTree(bare)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	got, err := os.ReadFile(filepath.Join(dir, "a.go"))
	if err != nil || string(got) != "TODO(no-owner)\n" {
		t.Fatalf("a.go not materialized: %q err=%v", got, err)
	}
}

func TestPreviewTree_noCommits(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.git")
	exec.Command("git", "init", "-q", "--bare", empty).Run()
	_, _, err := PreviewTree(empty)
	if err == nil {
		t.Fatal("want error for a bare repo with no commits")
	}
}
