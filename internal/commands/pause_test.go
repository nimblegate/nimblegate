// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/state"
)

// setupFakeHome redirects HOME to a temp dir so state.NewStore() sees an
// isolated filesystem. Returns the home path so tests can inspect state.json.
func setupFakeHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

// setupFakeProject creates a directory with appframes.toml and chdirs into
// it. paths.FindProjectRoot will succeed at the returned path.
func setupFakeProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "appframes.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	prevCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevCwd) })
	return dir
}

func TestPause_NoScope_Errors(t *testing.T) {
	setupFakeHome(t)
	if exit := Pause([]string{}); exit != 2 {
		t.Errorf("Pause with no scope: exit = %d, want 2", exit)
	}
}

func TestPause_BothScopes_Errors(t *testing.T) {
	setupFakeHome(t)
	if exit := Pause([]string{"--global", "--project"}); exit != 2 {
		t.Errorf("Pause --global --project: exit = %d, want 2", exit)
	}
}

func TestPause_Global_WritesStateFile(t *testing.T) {
	home := setupFakeHome(t)
	exit := Pause([]string{"--global", "--reason", "evaluation"})
	if exit != 0 {
		t.Fatalf("Pause --global: exit = %d, want 0", exit)
	}
	statePath := filepath.Join(home, ".appframes", "state.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("state.json not written: %v", err)
	}

	// Verify the round-trip through IsPaused so we know the file is also readable.
	s := state.NewStoreAt(home)
	st, err := s.IsPaused("")
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if !st.GlobalPaused {
		t.Error("GlobalPaused = false after Pause --global")
	}
	if st.GlobalReason != "evaluation" {
		t.Errorf("GlobalReason = %q, want %q", st.GlobalReason, "evaluation")
	}
}

func TestPause_Project_NoProjectRoot_Errors(t *testing.T) {
	setupFakeHome(t)
	// cwd is wherever t.TempDir() doesn't include appframes.toml ancestors -
	// in CI it'll be inside the test workdir which has appframes.toml at the
	// repo root. To genuinely test "no project", chdir into a tmp dir.
	prev, _ := os.Getwd()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	if exit := Pause([]string{"--project"}); exit != 2 {
		t.Errorf("Pause --project outside project: exit = %d, want 2", exit)
	}
}

func TestPause_Project_WritesMarker(t *testing.T) {
	setupFakeHome(t)
	root := setupFakeProject(t)

	exit := Pause([]string{"--project", "--reason", "frame misfiring"})
	if exit != 0 {
		t.Fatalf("Pause --project: exit = %d, want 0", exit)
	}
	marker := filepath.Join(root, ".appframes", "paused")
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker not written: %v", err)
	}
}

func TestResume_NothingPaused_Succeeds(t *testing.T) {
	setupFakeHome(t)
	// No flags, nothing paused - should print "nothing is paused" and exit 0.
	if exit := Resume([]string{}); exit != 0 {
		t.Errorf("Resume with nothing paused: exit = %d, want 0", exit)
	}
}

func TestResume_GlobalAndProjectFlags_Conflict(t *testing.T) {
	setupFakeHome(t)
	if exit := Resume([]string{"--global", "--project"}); exit != 2 {
		t.Errorf("Resume --global --project: exit = %d, want 2", exit)
	}
}

func TestResume_Global_ClearsState(t *testing.T) {
	home := setupFakeHome(t)
	// Pause first
	if exit := Pause([]string{"--global"}); exit != 0 {
		t.Fatal("setup pause failed")
	}
	if exit := Resume([]string{"--global"}); exit != 0 {
		t.Fatalf("Resume --global: exit = %d, want 0", exit)
	}
	s := state.NewStoreAt(home)
	st, _ := s.IsPaused("")
	if st.GlobalPaused {
		t.Error("GlobalPaused = true after Resume --global")
	}
}

func TestResume_Default_PicksProjectOverGlobal(t *testing.T) {
	home := setupFakeHome(t)
	root := setupFakeProject(t)

	// Pause both scopes.
	if exit := Pause([]string{"--global"}); exit != 0 {
		t.Fatal("setup global pause failed")
	}
	if exit := Pause([]string{"--project"}); exit != 0 {
		t.Fatal("setup project pause failed")
	}

	// Default resume should clear project, leaving global paused.
	if exit := Resume([]string{}); exit != 0 {
		t.Fatalf("Resume default: exit = %d, want 0", exit)
	}
	s := state.NewStoreAt(home)
	st, _ := s.IsPaused(root)
	if st.ProjectPaused {
		t.Error("ProjectPaused = true after default resume (should clear project first)")
	}
	if !st.GlobalPaused {
		t.Error("GlobalPaused cleared too: default should leave global alone")
	}
}

func TestResume_All_ClearsBoth(t *testing.T) {
	home := setupFakeHome(t)
	root := setupFakeProject(t)

	if exit := Pause([]string{"--global"}); exit != 0 {
		t.Fatal("setup global pause failed")
	}
	if exit := Pause([]string{"--project"}); exit != 0 {
		t.Fatal("setup project pause failed")
	}

	if exit := Resume([]string{"--all"}); exit != 0 {
		t.Fatalf("Resume --all: exit = %d, want 0", exit)
	}
	s := state.NewStoreAt(home)
	st, _ := s.IsPaused(root)
	if st.AnyPaused() {
		t.Error("AnyPaused after Resume --all (should clear everything)")
	}
}
