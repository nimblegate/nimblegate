// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"nimblegate/internal/frames"
)

// TestAuditRotate_TriggersAtThreshold - writer rotates its own part file
// when its size crosses maxBytes. Each rotation creates a `.N` sibling
// alongside the active part file.
func TestAuditRotate_TriggersAtThreshold(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")

	t.Setenv("APPFRAMES_AUDIT_MAX_BYTES", "200")
	t.Setenv("APPFRAMES_AUDIT_MAX_FILES", "3")

	a, err := OpenAudit(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	for i := 0; i < 5; i++ {
		err := a.Write(
			CheckContext{Trigger: TriggerCLI, Command: "rotation test command line padded"},
			CheckResult{
				FrameID:  "git-safety/test-" + strconv.Itoa(i),
				Category: frames.CategoryGitSafety,
				Outcome:  OutcomePass,
			},
		)
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	partPath := a.partPath
	_ = a.Close()

	if _, err := os.Stat(partPath); err != nil {
		t.Errorf("part file missing after rotation: %v", err)
	}
	if _, err := os.Stat(partPath + ".1"); err != nil {
		t.Errorf("part.log.1 missing after rotation: %v", err)
	}
}

// TestAuditRotate_EnforcesMaxFiles - past N rotations, the oldest sibling
// of the part file is dropped.
func TestAuditRotate_EnforcesMaxFiles(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")
	t.Setenv("APPFRAMES_AUDIT_MAX_BYTES", "150")
	t.Setenv("APPFRAMES_AUDIT_MAX_FILES", "2") // keep part + .1 + .2

	a, err := OpenAudit(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		_ = a.Write(
			CheckContext{Trigger: TriggerCLI, Command: "lots of writes here please trigger many rotations"},
			CheckResult{
				FrameID:  "git-safety/cap-" + strconv.Itoa(i),
				Category: frames.CategoryGitSafety,
				Outcome:  OutcomePass,
			},
		)
	}
	partPath := a.partPath
	_ = a.Close()

	for _, suffix := range []string{"", ".1", ".2"} {
		if _, err := os.Stat(partPath + suffix); err != nil {
			t.Errorf("expected file %s to exist: %v", partPath+suffix, err)
		}
	}
	if _, err := os.Stat(partPath + ".3"); err == nil {
		t.Errorf("part.log.3 unexpectedly exists; maxFiles=2 should have dropped it")
	}
}

// TestRotatedFiles_OldestFirstOrdering - RotatedFiles returns the audit log
// family ordered from oldest (highest numeric suffix) to newest (current).
func TestRotatedFiles_OldestFirstOrdering(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")
	for _, suffix := range []string{"", ".1", ".2", ".3"} {
		if err := os.WriteFile(logPath+suffix, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := RotatedFiles(logPath)
	if len(got) != 4 {
		t.Fatalf("got %d, want 4", len(got))
	}
	// oldest first: .3, .2, .1, current
	wantOrder := []string{".3", ".2", ".1", ""}
	for i, suffix := range wantOrder {
		if !strings.HasSuffix(got[i], "audit.log"+suffix) {
			t.Errorf("position %d: got %q, expected suffix %q", i, got[i], suffix)
		}
	}
}

// TestRotatedFiles_NonNumericSuffixIgnored - `audit.log.prearchive` (a
// non-numeric suffix) shouldn't be picked up.
func TestRotatedFiles_NonNumericSuffixIgnored(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")
	_ = os.WriteFile(logPath, []byte("x"), 0o644)
	_ = os.WriteFile(logPath+".prearchive", []byte("x"), 0o644)
	_ = os.WriteFile(logPath+".bak", []byte("x"), 0o644)

	got := RotatedFiles(logPath)
	if len(got) != 1 {
		t.Errorf("got %d files, want only the current audit.log; result = %v", len(got), got)
	}
}
