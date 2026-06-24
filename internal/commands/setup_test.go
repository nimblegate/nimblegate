// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddPATHToRC_NewFile(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	shimsDir := "/home/user/.appframes/shims"
	now := time.Date(2026, 5, 21, 0, 0, 0, 0, time.UTC)

	if err := addPATHToRC(rcPath, shimsDir, now); err != nil {
		t.Fatalf("addPATHToRC: %v", err)
	}
	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, pathMarkerBegin) {
		t.Error("missing pathMarkerBegin")
	}
	if !strings.Contains(out, pathMarkerEnd) {
		t.Error("missing pathMarkerEnd")
	}
	if !strings.Contains(out, "export PATH=\""+shimsDir+":$PATH\"") {
		t.Errorf("missing PATH export line:\n%s", out)
	}
	if !strings.Contains(out, "2026-05-21") {
		t.Errorf("missing date in marker:\n%s", out)
	}
}

func TestAddPATHToRC_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	now := time.Now()

	if err := addPATHToRC(rcPath, "/shim", now); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(rcPath)
	if err := addPATHToRC(rcPath, "/shim", now); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(rcPath)
	if !bytes.Equal(first, second) {
		t.Error("second call modified the file: should be idempotent")
	}
}

func TestAddPATHToRC_PreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	existing := "# user's existing config\nexport EDITOR=vim\nalias ll='ls -la'\n"
	if err := os.WriteFile(rcPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := addPATHToRC(rcPath, "/shim", time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rcPath)
	out := string(data)
	if !strings.Contains(out, existing) {
		t.Error("existing content lost")
	}
	if !strings.Contains(out, pathMarkerBegin) {
		t.Error("marker not appended")
	}
}

func TestAddPATHToRC_AppendsNewlineIfMissing(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".bashrc")
	// File ends WITHOUT a trailing newline.
	if err := os.WriteFile(rcPath, []byte("export EDITOR=vim"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := addPATHToRC(rcPath, "/shim", time.Now()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(rcPath)
	out := string(data)
	// The existing line and the marker should NOT be on the same line.
	if strings.Contains(out, "export EDITOR=vim# >>>") {
		t.Errorf("missing newline between existing content and marker:\n%s", out)
	}
}

func TestDetectSetupState_FreshSystem(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	st, err := detectSetupState("bash")
	if err != nil {
		t.Fatalf("detectSetupState: %v", err)
	}
	if st.shimsInstalled {
		t.Error("shimsInstalled = true on fresh system")
	}
	if st.pathOnRc {
		t.Error("pathOnRc = true on fresh system")
	}
	if st.snippetInstalled {
		t.Error("snippetInstalled = true on fresh system")
	}
	if st.complete() {
		t.Error("complete() = true on fresh system")
	}
}

func TestDetectSetupState_AfterPATHEdit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rcPath := filepath.Join(home, ".bashrc")
	if err := addPATHToRC(rcPath, filepath.Join(home, ".appframes/shims"), time.Now()); err != nil {
		t.Fatal(err)
	}

	st, err := detectSetupState("bash")
	if err != nil {
		t.Fatal(err)
	}
	if !st.pathOnRc {
		t.Error("pathOnRc = false after addPATHToRC")
	}
}

func TestSetup_CheckFlag_ReportsAndExits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// On a fresh fake home, --check should report incomplete and exit 1.
	exit := Setup([]string{"--check"})
	if exit != 1 {
		t.Errorf("Setup --check on fresh home: exit = %d, want 1", exit)
	}
}

func TestSetup_DryRun_NoSideEffects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exit := Setup([]string{"--dry-run", "--yes"})
	if exit != 0 {
		t.Errorf("Setup --dry-run --yes: exit = %d, want 0", exit)
	}
	// Confirm nothing was written.
	if _, err := os.Stat(filepath.Join(home, ".appframes")); err == nil {
		t.Error(".appframes/ was created during --dry-run")
	}
	if _, err := os.Stat(filepath.Join(home, ".bashrc")); err == nil {
		t.Error(".bashrc was created during --dry-run")
	}
}

func TestSetup_Yes_FullInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	exit := Setup([]string{"--yes"})
	if exit != 0 {
		t.Errorf("Setup --yes: exit = %d, want 0", exit)
	}
	// Shims dir should exist with files.
	entries, err := os.ReadDir(filepath.Join(home, ".appframes/shims"))
	if err != nil {
		t.Fatalf("shims dir missing: %v", err)
	}
	if len(entries) == 0 {
		t.Error("shims dir empty after --yes")
	}
	// .bashrc should have the PATH marker.
	data, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatalf(".bashrc missing: %v", err)
	}
	if !strings.Contains(string(data), pathMarkerBegin) {
		t.Errorf(".bashrc missing PATH marker:\n%s", string(data))
	}
}

func TestSetup_Idempotent_SecondRunIsNoOp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("first setup failed")
	}
	first, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if exit := Setup([]string{"--yes"}); exit != 0 {
		t.Fatal("second setup failed")
	}
	second, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	if !bytes.Equal(first, second) {
		t.Error("second setup changed .bashrc: should be idempotent")
	}
}
