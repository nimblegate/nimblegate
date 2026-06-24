// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeFolderBranchMap(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder-branch-map.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFolderBranchLock_MatchPasses(t *testing.T) {
	root := t.TempDir()
	writeFolderBranchMap(t, root, `
[folders]
"infra/" = "infra"
"landing/" = "landing"
`)
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    filepath.Join(root, "infra"),
		Command:       "git push origin infra",
		CurrentBranch: "infra",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, reason = %q", got.Outcome, got.Reason)
	}
}

func TestFolderBranchLock_MismatchBlocks(t *testing.T) {
	root := t.TempDir()
	writeFolderBranchMap(t, root, `
[folders]
"infra/" = "infra"
"landing/" = "landing"
`)
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    filepath.Join(root, "infra"),
		Command:       "git push origin landing",
		CurrentBranch: "landing",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK; reason = %q", got.Outcome, got.Reason)
	}
	// Regression guard: fix message must mention BOTH remedies -
	// `git checkout <expected>` for single-checkout layouts and `cd into
	// the folder` for multi-folder-per-branch layouts.
	for _, want := range []string{"git checkout infra", "cd into the folder"} {
		if !strings.Contains(got.Fix, want) {
			t.Errorf("fix missing %q; got: %s", want, got.Fix)
		}
	}
}

func TestFolderBranchLock_NoCanonicalTableSkips(t *testing.T) {
	root := t.TempDir()
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    filepath.Join(root, "infra"),
		CurrentBranch: "anything",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP", got.Outcome)
	}
}

func TestFolderBranchLock_FolderNotInMapSkips(t *testing.T) {
	root := t.TempDir()
	writeFolderBranchMap(t, root, `
[folders]
"infra/" = "infra"
`)
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    filepath.Join(root, "unrelated"),
		CurrentBranch: "main",
	}
	got := FolderBranchLock(ctx)
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP (folder not in map)", got.Outcome)
	}
}
