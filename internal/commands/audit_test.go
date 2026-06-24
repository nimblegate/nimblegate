// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_AuditReset_BackupPreservesFamily - the headline path: --backup
// renames audit.log and any rotated siblings to a timestamped prefix.
func TestE2E_AuditReset_BackupPreservesFamily(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Seed audit.log + one rotated sibling so we can verify both move.
	logDir := filepath.Join(tmp, ".appframes")
	if err := os.WriteFile(filepath.Join(logDir, "audit.log"), []byte(`{"ts":"now"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "audit.log.1"), []byte(`{"ts":"earlier"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "audit", "reset", "--backup")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("audit reset --backup: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Audit log preserved") {
		t.Errorf("expected success message; got: %s", out)
	}

	// audit.log + audit.log.1 must be gone; renamed versions must exist.
	if _, err := os.Stat(filepath.Join(logDir, "audit.log")); err == nil {
		t.Error("audit.log still present after --backup; should have been renamed")
	}
	if _, err := os.Stat(filepath.Join(logDir, "audit.log.1")); err == nil {
		t.Error("audit.log.1 still present after --backup; should have been renamed")
	}
	// Verify at least one .reset-* file exists.
	entries, _ := os.ReadDir(logDir)
	resetCount := 0
	for _, e := range entries {
		if strings.Contains(e.Name(), ".reset-") {
			resetCount++
		}
	}
	if resetCount != 2 {
		t.Errorf("expected 2 .reset-* files (audit.log + audit.log.1), got %d (entries: %v)", resetCount, entries)
	}
}

// TestE2E_AuditReset_NoBackupRequiresYes - destructive path is gated.
func TestE2E_AuditReset_NoBackupRequiresYes(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(tmp, ".appframes", "audit.log"), []byte("{}\n"), 0o644)

	// Without --backup and without --yes → refusal.
	cmd := exec.Command(bin, "audit", "reset")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 0 {
		t.Error("expected non-zero exit when neither --backup nor --yes given")
	}
	if !strings.Contains(string(out), "refusing to destroy") {
		t.Errorf("expected refusal message; got: %s", out)
	}
	// audit.log must still exist.
	if _, err := os.Stat(filepath.Join(tmp, ".appframes", "audit.log")); err != nil {
		t.Errorf("audit.log unexpectedly removed during refusal path: %v", err)
	}
}

// TestE2E_AuditReset_YesDestroys - confirmation flag does the destructive thing.
func TestE2E_AuditReset_YesDestroys(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(tmp, ".appframes", "audit.log"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(tmp, ".appframes", "audit.log.1"), []byte("{}\n"), 0o644)

	cmd := exec.Command(bin, "audit", "reset", "--yes")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("audit reset --yes: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Removed 2") {
		t.Errorf("expected 'Removed 2' confirmation; got: %s", out)
	}
	for _, name := range []string{"audit.log", "audit.log.1"} {
		if _, err := os.Stat(filepath.Join(tmp, ".appframes", name)); err == nil {
			t.Errorf("%s still present after --yes destroy", name)
		}
	}
}

// TestE2E_AuditReset_NoLogYet - graceful when there's nothing to reset.
func TestE2E_AuditReset_NoLogYet(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// No audit.log written.
	cmd := exec.Command(bin, "audit", "reset", "--yes")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 0 {
		t.Errorf("expected exit 0 when no log exists; got %d, out: %s",
			cmd.ProcessState.ExitCode(), out)
	}
	if !strings.Contains(string(out), "no audit log to reset") {
		t.Errorf("expected polite no-op message; got: %s", out)
	}
}

// TestE2E_AuditResetWithoutInit - must error cleanly when invoked
// outside a project.
func TestE2E_AuditResetWithoutInit(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "audit", "reset", "--yes")
	cmd.Dir = tmp
	_, _ = cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 0 {
		t.Error("expected non-zero exit when no project root found")
	}
}

// TestE2E_AuditUnknownSubcommand - `audit foo` should error with help.
func TestE2E_AuditUnknownSubcommand(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "audit", "foo")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("expected exit 2 for unknown subcommand; got %d", cmd.ProcessState.ExitCode())
	}
	if !strings.Contains(string(out), "unknown subcommand") {
		t.Errorf("expected 'unknown subcommand' message; got: %s", out)
	}
}
