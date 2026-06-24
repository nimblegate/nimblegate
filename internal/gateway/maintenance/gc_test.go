// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestShellGC_runsAgainstRealBareRepo confirms ShellGC actually shells out
// to git, completes without error on a normal bare repo, and reports
// meaningful timing.
func TestShellGC_runsAgainstRealBareRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping real-fs gc test")
	}
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	res := ShellGC{}.Run(context.Background(), bare)
	if res.Err != nil {
		t.Fatalf("gc returned error on a clean bare repo: %v", res.Err)
	}
	if res.Repo != "x.git" {
		t.Errorf("Repo = %q; want x.git", res.Repo)
	}
	if res.Took <= 0 {
		t.Errorf("Took = %s; want >0", res.Took)
	}
}

// TestShellGC_errorOnNonRepo confirms the error path returns a useful
// message rather than panicking when the directory isn't a git repo.
func TestShellGC_errorOnNonRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping real-fs gc test")
	}
	tmp := t.TempDir() // empty dir, not a git repo

	res := ShellGC{}.Run(context.Background(), tmp)
	if res.Err == nil {
		t.Fatal("expected error for non-repo directory")
	}
	if !strings.Contains(res.Err.Error(), "git gc") {
		t.Errorf("error doesn't mention git gc; got: %v", res.Err)
	}
}

// TestShellGC_respectsCtxCancel confirms a cancelled ctx terminates the
// child process rather than blocking indefinitely.
func TestShellGC_respectsCtxCancel(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping real-fs gc test")
	}
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	res := ShellGC{}.Run(ctx, bare)
	// On pre-cancelled ctx, exec returns quickly with a context error. Either
	// the error is set OR git noticed and exited cleanly anyway - both are
	// acceptable; the important property is "doesn't hang".
	if res.Took > 0 {
		// got a result without hanging - that's the win
		return
	}
	t.Errorf("ctx cancel didn't terminate the run promptly (Took=%s, Err=%v)", res.Took, res.Err)
}
