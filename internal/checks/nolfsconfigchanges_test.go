// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestNoLFSConfigChanges_blocksOnAddedFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".lfsconfig")
	if err := os.WriteFile(path, []byte("[lfs]\nurl = https://attacker.example.com/lfs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, ".lfsconfig change") {
		t.Errorf("reason should describe the violation; got: %s", got.Reason)
	}
	if !strings.Contains(got.Fix, "disable marker") {
		t.Errorf("fix should mention the disable marker; got: %s", got.Fix)
	}
}

func TestNoLFSConfigChanges_blocksOnNestedLfsconfig(t *testing.T) {
	// Repos can have multiple .lfsconfig files; any change anywhere fires.
	root := t.TempDir()
	nested := filepath.Join(root, "subdir", "nested-area")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, ".lfsconfig")
	if err := os.WriteFile(path, []byte("[lfs]\nurl = https://x.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK", got.Outcome)
	}
}

func TestNoLFSConfigChanges_passesWhenNoLfsconfigInStage(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := os.WriteFile(path, []byte("# project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no .lfsconfig in stage)", got.Outcome)
	}
}

func TestNoLFSConfigChanges_overrideMarkerSkips(t *testing.T) {
	root := t.TempDir()
	lfs := filepath.Join(root, ".lfsconfig")
	if err := os.WriteFile(lfs, []byte("[lfs]\nurl = https://new.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The disable marker lives in the commit message file (or any other
	// staged file). Simulate it via a COMMIT_EDITMSG-style staged file.
	msg := filepath.Join(root, "COMMIT_EDITMSG")
	body := "Move LFS to new server\n\nappframes:disable git/no-lfsconfig-changes - reason: planned migration\n"
	if err := os.WriteFile(msg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{lfs, msg},
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s; want SKIP (override active)", got.Outcome)
	}
}

func TestNoLFSConfigChanges_emptyStagePreCommitPasses(t *testing.T) {
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:     engine.TriggerPreCommit,
		ProjectRoot: t.TempDir(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file-scan scope contract)", got.Outcome)
	}
}

func TestNoLFSConfigChanges_emptyStageCliSkips(t *testing.T) {
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: t.TempDir(),
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s; want SKIP", got.Outcome)
	}
}

func TestNoLFSConfigChanges_hitsPopulatedForAuditLog(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".lfsconfig")
	if err := os.WriteFile(path, []byte("[lfs]\nurl = https://x.example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoLFSConfigChanges(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{path},
	})
	if len(got.Hits) != 1 {
		t.Fatalf("expected 1 hit; got %d", len(got.Hits))
	}
	if got.Hits[0].File != path {
		t.Errorf("hit.File = %q; want %q", got.Hits[0].File, path)
	}
	if !strings.Contains(got.Hits[0].Label, ".lfsconfig staged") {
		t.Errorf("hit.Label should describe what's staged; got: %s", got.Hits[0].Label)
	}
}
