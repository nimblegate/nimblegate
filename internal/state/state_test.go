// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_GlobalPauseAndResume_RoundTrip(t *testing.T) {
	home := t.TempDir()
	s := NewStoreAt(home)
	now := time.Date(2026, 5, 21, 18, 0, 0, 0, time.UTC)

	if err := s.PauseGlobal("evaluation week", now); err != nil {
		t.Fatalf("PauseGlobal: %v", err)
	}

	st, err := s.IsPaused("")
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if !st.GlobalPaused {
		t.Fatal("GlobalPaused = false, want true")
	}
	if !st.GlobalPausedAt.Equal(now) {
		t.Errorf("GlobalPausedAt = %v, want %v", st.GlobalPausedAt, now)
	}
	if st.GlobalReason != "evaluation week" {
		t.Errorf("GlobalReason = %q, want %q", st.GlobalReason, "evaluation week")
	}
	if !st.AnyPaused() {
		t.Error("AnyPaused = false, want true after PauseGlobal")
	}

	if err := s.ResumeGlobal(); err != nil {
		t.Fatalf("ResumeGlobal: %v", err)
	}
	st, err = s.IsPaused("")
	if err != nil {
		t.Fatalf("IsPaused after resume: %v", err)
	}
	if st.GlobalPaused {
		t.Error("GlobalPaused = true after ResumeGlobal, want false")
	}
}

func TestStore_ResumeGlobal_Idempotent(t *testing.T) {
	s := NewStoreAt(t.TempDir())
	// Resume without ever pausing - must be a no-op, not an error.
	if err := s.ResumeGlobal(); err != nil {
		t.Errorf("ResumeGlobal on fresh state: %v", err)
	}
	// Pause, resume, resume again.
	if err := s.PauseGlobal("x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeGlobal(); err != nil {
		t.Fatal(err)
	}
	if err := s.ResumeGlobal(); err != nil {
		t.Errorf("second ResumeGlobal: %v", err)
	}
}

func TestStore_PauseGlobal_Idempotent_UpdatesTimestamp(t *testing.T) {
	s := NewStoreAt(t.TempDir())
	t1 := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)

	if err := s.PauseGlobal("first", t1); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseGlobal("second", t2); err != nil {
		t.Fatal(err)
	}

	st, _ := s.IsPaused("")
	if !st.GlobalPausedAt.Equal(t2) {
		t.Errorf("PausedAt = %v, want %v (re-pause should update)", st.GlobalPausedAt, t2)
	}
	if st.GlobalReason != "second" {
		t.Errorf("Reason = %q, want %q", st.GlobalReason, "second")
	}
}

func TestStore_ProjectPauseAndResume_RoundTrip(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	s := NewStoreAt(home)
	now := time.Date(2026, 5, 21, 18, 0, 0, 0, time.UTC)

	if err := s.PauseProject(project, "this project misfiring", now); err != nil {
		t.Fatalf("PauseProject: %v", err)
	}

	st, err := s.IsPaused(project)
	if err != nil {
		t.Fatalf("IsPaused: %v", err)
	}
	if st.GlobalPaused {
		t.Error("GlobalPaused = true, want false (project pause shouldn't affect global)")
	}
	if !st.ProjectPaused {
		t.Fatal("ProjectPaused = false, want true")
	}
	if st.ProjectReason != "this project misfiring" {
		t.Errorf("ProjectReason = %q", st.ProjectReason)
	}
	if !st.ProjectPausedAt.Equal(now) {
		t.Errorf("ProjectPausedAt = %v, want %v", st.ProjectPausedAt, now)
	}

	if err := s.ResumeProject(project); err != nil {
		t.Fatalf("ResumeProject: %v", err)
	}
	st, _ = s.IsPaused(project)
	if st.ProjectPaused {
		t.Error("ProjectPaused = true after resume, want false")
	}
}

func TestStore_IsPaused_NoStateFiles(t *testing.T) {
	s := NewStoreAt(t.TempDir())
	st, err := s.IsPaused(t.TempDir())
	if err != nil {
		t.Fatalf("IsPaused on fresh state: %v", err)
	}
	if st.AnyPaused() {
		t.Error("AnyPaused = true on fresh state, want false")
	}
}

func TestStore_IsPaused_BothScopes(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	s := NewStoreAt(home)
	now := time.Now()

	if err := s.PauseGlobal("global reason", now); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseProject(project, "project reason", now); err != nil {
		t.Fatal(err)
	}

	st, _ := s.IsPaused(project)
	if !st.GlobalPaused {
		t.Error("expected GlobalPaused")
	}
	if !st.ProjectPaused {
		t.Error("expected ProjectPaused")
	}
	if st.GlobalReason != "global reason" || st.ProjectReason != "project reason" {
		t.Errorf("reasons crossed: global=%q project=%q", st.GlobalReason, st.ProjectReason)
	}
}

func TestStore_IsPaused_CorruptStateFile_ReturnsError(t *testing.T) {
	home := t.TempDir()
	s := NewStoreAt(home)
	if err := os.MkdirAll(filepath.Join(home, ".appframes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.GlobalStateFile(), []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := s.IsPaused("")
	if err == nil {
		t.Error("expected error for corrupt state.json, got nil")
	}
	// Status should remain unpaused (fail-closed: corrupt → enforcement on)
	if st.AnyPaused() {
		t.Error("AnyPaused = true on corrupt state, want false (fail-closed)")
	}
	if !strings.Contains(err.Error(), "state.json") {
		t.Errorf("error doesn't mention state.json: %v", err)
	}
}

func TestStore_ProjectMarker_EmptyRoot_Errors(t *testing.T) {
	s := NewStoreAt(t.TempDir())
	if err := s.PauseProject("", "reason", time.Now()); err == nil {
		t.Error("PauseProject with empty root should error")
	}
	if err := s.ResumeProject(""); err == nil {
		t.Error("ResumeProject with empty root should error")
	}
}

func TestStore_IsPaused_OnlyProject_NoProjectRootGiven(t *testing.T) {
	// Caller outside an nimblegate project: IsPaused("") should not look for
	// project markers (there's nothing to look in). Only global counts.
	home := t.TempDir()
	s := NewStoreAt(home)
	if err := s.PauseGlobal("g", time.Now()); err != nil {
		t.Fatal(err)
	}
	st, _ := s.IsPaused("")
	if !st.GlobalPaused {
		t.Error("expected GlobalPaused")
	}
	if st.ProjectPaused {
		t.Error("ProjectPaused should be false when no projectRoot given")
	}
}

func TestProjectMarkerFile_EmptyRoot(t *testing.T) {
	if got := ProjectMarkerFile(""); got != "" {
		t.Errorf("ProjectMarkerFile(\"\") = %q, want empty", got)
	}
	if got := ProjectMarkerFile("/p"); got != "/p/.appframes/paused" {
		t.Errorf("ProjectMarkerFile = %q", got)
	}
}
