// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemovePATHFromRC_MissingFile_NoError(t *testing.T) {
	// Removing from a non-existent file should be a no-op, not an error.
	if err := removePATHFromRC(filepath.Join(t.TempDir(), "doesnotexist")); err != nil {
		t.Errorf("removePATHFromRC on missing file: %v", err)
	}
}

func TestRemovePATHFromRC_NoMarker_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	body := "# user config\nexport EDITOR=vim\nalias ll='ls -la'\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removePATHFromRC(rcPath); err != nil {
		t.Fatalf("removePATHFromRC: %v", err)
	}
	got, _ := os.ReadFile(rcPath)
	if string(got) != body {
		t.Errorf("file modified despite no marker:\nbefore: %q\nafter:  %q", body, string(got))
	}
}

func TestRemovePATHFromRC_RemovesBlockAndBlankLine(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	preexisting := "# user config\nexport EDITOR=vim\n"
	// First add via the real function so the format matches.
	if err := os.WriteFile(rcPath, []byte(preexisting), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addPATHToRC(rcPath, "/shim", time.Now()); err != nil {
		t.Fatal(err)
	}
	// Sanity: marker is present.
	withMarker, _ := os.ReadFile(rcPath)
	if !strings.Contains(string(withMarker), pathMarkerBegin) {
		t.Fatal("setup failed: marker not present after addPATHToRC")
	}

	// Now remove it and confirm we got back to the pre-existing content.
	if err := removePATHFromRC(rcPath); err != nil {
		t.Fatalf("removePATHFromRC: %v", err)
	}
	got, _ := os.ReadFile(rcPath)
	if string(got) != preexisting {
		t.Errorf("did not restore original content:\nwant: %q\ngot:  %q", preexisting, string(got))
	}
}

func TestRemovePATHFromRC_HalfBlock_Errors(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	// Begin marker but no end - simulates user manually editing.
	body := "# config\n" + pathMarkerBegin + " (added 2026-05-21) >>>\nexport PATH=\"/foo:$PATH\"\n# someone deleted the end marker\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	err := removePATHFromRC(rcPath)
	if err == nil {
		t.Error("expected error on half-block, got nil")
	}
	if !strings.Contains(err.Error(), "half a block") {
		t.Errorf("error doesn't mention half-block: %v", err)
	}
	// And the file should NOT have been modified.
	got, _ := os.ReadFile(rcPath)
	if string(got) != body {
		t.Error("file was modified despite refusing to remove half-block")
	}
}

func TestRemoveInstallPATHFromRC_RemovesBlock(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	body := "# user\nexport EDITOR=vim\n\n" +
		installPathMarkerBegin + " (added 2026-05-21) >>>\n" +
		"export PATH=\"$HOME/.appframes/bin:$PATH\"\n" +
		installPathMarkerEnd + " <<<\n" +
		"# more user content\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallPATHFromRC(rcPath); err != nil {
		t.Fatalf("removeInstallPATHFromRC: %v", err)
	}
	got, _ := os.ReadFile(rcPath)
	if strings.Contains(string(got), installPathMarkerBegin) {
		t.Errorf("install PATH marker still present:\n%s", got)
	}
	if !strings.Contains(string(got), "export EDITOR=vim") {
		t.Errorf("preserved user content lost:\n%s", got)
	}
	if !strings.Contains(string(got), "# more user content") {
		t.Errorf("post-block content lost:\n%s", got)
	}
}

func TestRemoveInstallPATHFromRC_Idempotent_NoMarker(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	body := "# nothing to remove\nexport EDITOR=vim\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallPATHFromRC(rcPath); err != nil {
		t.Fatalf("removeInstallPATHFromRC: %v", err)
	}
	got, _ := os.ReadFile(rcPath)
	if string(got) != body {
		t.Errorf("file modified despite no install marker:\n%s", got)
	}
}

func TestRemoveInstallPATHFromRC_HalfBlock_Errors(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	body := installPathMarkerBegin + " (added 2026-05-21) >>>\nexport PATH=\"/foo:$PATH\"\n# missing end marker\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removeInstallPATHFromRC(rcPath); err == nil {
		t.Error("expected error on half-block, got nil")
	}
}

func TestPurge_RemovesBothMarkerBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rcPath := filepath.Join(home, ".bashrc")

	// Pre-populate .bashrc with BOTH marker blocks (simulating install.sh
	// + setup having run).
	body := "# user\nexport EDITOR=vim\n\n" +
		installPathMarkerBegin + " (added 2026-05-21) >>>\n" +
		"export PATH=\"$HOME/.appframes/bin:$PATH\"\n" +
		installPathMarkerEnd + " <<<\n\n" +
		pathMarkerBegin + " (added 2026-05-21) >>>\n" +
		"export PATH=\"$HOME/.appframes/shims:$PATH\"\n" +
		pathMarkerEnd + " <<<\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if exit := Purge([]string{"--yes"}); exit != 0 {
		t.Fatalf("Purge: exit = %d", exit)
	}
	got, _ := os.ReadFile(rcPath)
	if strings.Contains(string(got), installPathMarkerBegin) {
		t.Error("install-PATH marker still present after purge")
	}
	if strings.Contains(string(got), pathMarkerBegin) {
		t.Error("setup-PATH marker still present after purge")
	}
	if !strings.Contains(string(got), "export EDITOR=vim") {
		t.Errorf("user content lost:\n%s", got)
	}
}

func TestRemovePATHFromRC_PreservesContentAroundBlock(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	// Block in the middle of the file.
	body := "line1\nline2\n\n" + pathMarkerBegin + " (added 2026-05-21) >>>\nexport PATH=\"/shim:$PATH\"\n" + pathMarkerEnd + " <<<\nline_after_1\nline_after_2\n"
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := removePATHFromRC(rcPath); err != nil {
		t.Fatalf("removePATHFromRC: %v", err)
	}
	got, _ := os.ReadFile(rcPath)
	// Both before-block and after-block content must survive.
	if !strings.Contains(string(got), "line1") || !strings.Contains(string(got), "line_after_1") {
		t.Errorf("lost content around the block:\n%q", string(got))
	}
	if strings.Contains(string(got), pathMarkerBegin) {
		t.Error("marker still present after removal")
	}
}

func TestPurge_NothingInstalled_NoOps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exit := Purge([]string{"--yes"})
	if exit != 0 {
		t.Errorf("Purge on empty home: exit = %d, want 0", exit)
	}
}

func TestPurge_DryRun_PreservesInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Install first
	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("setup failed")
	}
	beforeRc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))

	if exit := Purge([]string{"--yes", "--dry-run"}); exit != 0 {
		t.Fatalf("Purge --dry-run: exit = %d, want 0", exit)
	}
	// Install should be intact
	if _, err := os.Stat(filepath.Join(home, ".appframes/shims/git")); err != nil {
		t.Error("shims removed during --dry-run")
	}
	afterRc, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if string(beforeRc) != string(afterRc) {
		t.Error(".bashrc modified during --dry-run")
	}
}

func TestPurge_FullRoundTrip_WithSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Capture the empty pre-state of .bashrc (won't exist)
	originalRcExists := false
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		originalRcExists = true
	}

	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("setup failed")
	}
	// After setup, .bashrc exists with the marker.
	rcAfterSetup, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if !strings.Contains(string(rcAfterSetup), pathMarkerBegin) {
		t.Fatal("setup did not add PATH marker")
	}

	if exit := Purge([]string{"--yes"}); exit != 0 {
		t.Fatalf("Purge --yes: exit = %d, want 0", exit)
	}

	// Shims dir gone
	if _, err := os.Stat(filepath.Join(home, ".appframes/shims")); err == nil {
		t.Error("shims dir still exists after purge")
	}
	// PATH marker gone from .bashrc
	rcAfterPurge, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if strings.Contains(string(rcAfterPurge), pathMarkerBegin) {
		t.Error("PATH marker still present after purge")
	}
	// ~/.appframes/ gone (default no --keep-config)
	if _, err := os.Stat(filepath.Join(home, ".appframes")); err == nil {
		t.Error("~/.appframes/ still exists after purge (default should remove)")
	}
	_ = originalRcExists // just for self-doc
}

func TestPurge_KeepConfig_PreservesHomeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("setup failed")
	}
	// Drop a fake state file to confirm --keep-config preserves it.
	statePath := filepath.Join(home, ".appframes/state.json")
	if err := os.WriteFile(statePath, []byte(`{"paused":false}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if exit := Purge([]string{"--yes", "--keep-config"}); exit != 0 {
		t.Fatalf("Purge --yes --keep-config: exit = %d", exit)
	}
	// Shims gone (PATH ed was for the shim layer)
	if _, err := os.Stat(filepath.Join(home, ".appframes/shims")); err == nil {
		t.Error("shims dir still exists after purge")
	}
	// But state.json preserved
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state.json removed despite --keep-config: %v", err)
	}
}

func TestPurge_AbortedByUser_NoChanges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("setup failed")
	}
	rcBefore, _ := os.ReadFile(filepath.Join(home, ".bashrc"))

	// Simulate user answering "no" by piping empty stdin → reads EOF → falls
	// back to defaultYes which is FALSE for the main purge confirmation.
	// Without --yes, the Stdio prompter reads from os.Stdin which is the
	// test process's stdin. We can't easily mock that here, so instead we
	// rely on the fact that t.TempDir + no --yes + no piped input means
	// EOF on stdin → defaultYes=false → "aborted".
	r, _, _ := os.Pipe()
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = origStdin })
	_ = r.Close() // close so the read returns EOF immediately

	exit := Purge([]string{})
	if exit != 0 {
		t.Errorf("Purge aborted: exit = %d, want 0", exit)
	}
	rcAfter, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if string(rcBefore) != string(rcAfter) {
		t.Error(".bashrc was modified despite user aborting")
	}
	if _, err := os.Stat(filepath.Join(home, ".appframes/shims/git")); err != nil {
		t.Error("shims removed despite user aborting")
	}
}
