// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// setupGitRepo creates a git repo at `dir` with one initial commit and
// optionally a fake "origin" bare repo plus a push so the commit is
// reachable from origin/<branch>.
//
// pushed=false → unpushed local commit (amend is safe; check returns PASS)
// pushed=true  → commit is on origin/main (amend rewrites shared history; BLOCK)
func setupGitRepo(t *testing.T, pushed bool) string {
	t.Helper()
	work := t.TempDir()
	bare := filepath.Join(work, "..", "origin.git")

	mustGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	mustGit(work, "init", "-b", "main")
	mustGit(work, "config", "user.email", "test@example.com")
	mustGit(work, "config", "user.name", "Test")
	mustGit(work, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(work, "add", "README.md")
	mustGit(work, "commit", "-m", "initial")

	if pushed {
		if err := os.MkdirAll(bare, 0o755); err != nil {
			t.Fatal(err)
		}
		mustGit(bare, "init", "--bare", "-b", "main")
		mustGit(work, "remote", "add", "origin", bare)
		mustGit(work, "push", "-u", "origin", "main")
	}
	return work
}

func TestNoAmendPushedCommits_SkipsNonCommitCommands(t *testing.T) {
	for _, cmd := range []string{
		"",
		"git status",
		"git push origin main",
		"npm install",
	} {
		ctx := engine.CheckContext{Trigger: engine.TriggerGitWrap, Command: cmd}
		got := NoAmendPushedCommits(ctx)
		if got.Outcome != engine.OutcomeSkip {
			t.Errorf("Command=%q: outcome = %s, want SKIP", cmd, got.Outcome)
		}
	}
}

func TestNoAmendPushedCommits_CommitWithoutAmendPasses(t *testing.T) {
	root := setupGitRepo(t, true)
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "git commit -m updated",
	}
	got := NoAmendPushedCommits(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("non-amend should PASS; got %s (%s)", got.Outcome, got.Reason)
	}
}

func TestNoAmendPushedCommits_UnpushedCommitPasses(t *testing.T) {
	// Local-only repo, no remote. Amending here is safe.
	root := setupGitRepo(t, false)
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "git commit --amend",
	}
	got := NoAmendPushedCommits(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("unpushed amend should PASS; got %s (%s)", got.Outcome, got.Reason)
	}
}

func TestNoAmendPushedCommits_PushedCommitBlocks(t *testing.T) {
	root := setupGitRepo(t, true)
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "git commit --amend",
	}
	got := NoAmendPushedCommits(ctx)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("pushed amend should BLOCK; got %s (%s)", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "origin/main") {
		t.Errorf("Reason should name the remote branch; got %q", got.Reason)
	}
	if !strings.Contains(got.Fix, "--force-yes") {
		t.Errorf("Fix should mention the audited bypass; got %q", got.Fix)
	}
}

func TestNoAmendPushedCommits_AmendWithMessageStillBlocks(t *testing.T) {
	// `git commit --amend -m "new"` - flag is still --amend.
	root := setupGitRepo(t, true)
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     `git commit --amend -m new-message`,
	}
	got := NoAmendPushedCommits(ctx)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("amend with -m should still BLOCK; got %s", got.Outcome)
	}
}

func TestNoAmendPushedCommits_NotAGitRepoPasses(t *testing.T) {
	// Empty temp dir, no .git. Falls through to PASS - git itself will
	// reject the amend cleanly when the user runs it.
	root := t.TempDir()
	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "git commit --amend",
	}
	got := NoAmendPushedCommits(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("non-git-repo should PASS; got %s (%s)", got.Outcome, got.Reason)
	}
}

func TestNoAmendPushedCommits_HeadShaInReason(t *testing.T) {
	// Verify the 7-char short SHA appears in the BLOCK reason.
	root := setupGitRepo(t, true)
	// Read the HEAD sha so we know what to expect.
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := strings.TrimSpace(string(out))[:7]

	ctx := engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "git commit --amend",
	}
	got := NoAmendPushedCommits(ctx)
	if !strings.Contains(got.Reason, wantPrefix) {
		t.Errorf("Reason should contain SHA prefix %q; got %q", wantPrefix, got.Reason)
	}
}
