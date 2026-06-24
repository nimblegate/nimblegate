// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os/exec"
	"strings"
	"testing"
)

// TestE2E_CmdAptPurgeBlocks - the headline fix.
// `nimblegate cmd apt purge <pkg>` (without --simulate) must fire
// command-safety/apt-purge-preview and return exit 1, without ever execing
// the real apt.
func TestE2E_CmdAptPurgeBlocks(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "cmd", "apt", "purge", "rpcbind")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	code := cmd.ProcessState.ExitCode()

	if code != 1 {
		t.Errorf("exit=%d, want 1 (BLOCK), got output:\n%s", code, out)
	}
	if !strings.Contains(string(out), "apt-purge-preview") {
		t.Errorf("output missing frame id; got:\n%s", out)
	}
	if !strings.Contains(string(out), "apt purge") {
		t.Errorf("output missing command context; got:\n%s", out)
	}
}

// TestE2E_CmdAptPurgeSimulatePasses - `apt purge --simulate` is the
// approved preview path; should PASS and exec the real apt (which we
// don't actually have on a dev machine, so we accept either real-apt's
// exit code or "no apt binary").
func TestE2E_CmdAptPurgeSimulatePasses(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "cmd", "apt", "purge", "--simulate", "rpcbind")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	code := cmd.ProcessState.ExitCode()

	// The CHECK must pass - outcome is PASS. After that we try to exec apt.
	// If apt isn't installed in the test env, code will be 2 ("no apt binary
	// found on PATH"). Either way: the BLOCK path was NOT taken.
	if strings.Contains(string(out), "apt-purge-preview") &&
		strings.Contains(string(out), "BLOCK") {
		t.Errorf("apt purge --simulate was BLOCKed; should have passed. exit=%d output:\n%s", code, out)
	}
}

// TestE2E_CmdNonInterceptedCommandPassesThrough - `echo` doesn't match any
// frame, so the command runs straight through to the real echo.
func TestE2E_CmdNonInterceptedCommandPassesThrough(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "cmd", "echo", "nimblegate-cmd-passthrough")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("echo passthrough failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "nimblegate-cmd-passthrough") {
		t.Errorf("expected echo's output in stdout; got:\n%s", out)
	}
}

// TestE2E_CmdForceYesPrintsConfirmation
// --force-yes must print a one-line stderr confirmation so the user
// doesn't wonder if the override took effect.
func TestE2E_CmdForceYesPrintsConfirmation(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "cmd", "--force-yes", "--reason=test override", "apt", "purge", "rpcbind")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()

	// The strengthened message includes "BYPASS RECORDED" + the audit
	// analyzer warning. Earlier fix kept --force-yes audible;
	// this iteration makes it explicitly anti-bypass.
	if !strings.Contains(string(out), "BYPASS RECORDED") {
		t.Errorf("expected loud BYPASS RECORDED confirmation; got:\n%s", out)
	}
	if !strings.Contains(string(out), "test override") {
		t.Errorf("expected reason text in confirmation; got:\n%s", out)
	}
	if !strings.Contains(string(out), "audit analyze") {
		t.Errorf("expected reminder about audit analyze surfacing this; got:\n%s", out)
	}
}

// TestE2E_CmdMissingCommandArgFails - `nimblegate cmd` with no command word.
func TestE2E_CmdMissingCommandArgFails(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "cmd")
	cmd.Dir = tmp
	_, _ = cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("expected exit 2 for missing command, got %d", cmd.ProcessState.ExitCode())
	}
}

// TestE2E_GitForceYesPrintsConfirmation - Same fix applies to `nimblegate git`
// (both paths share interceptAndExec).
func TestE2E_GitForceYesPrintsConfirmation(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "git", "--force-yes", "--reason=audited bypass", "push", "--force", "origin", "main")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "BYPASS RECORDED") {
		t.Errorf("expected loud BYPASS RECORDED confirmation in stderr; got:\n%s", out)
	}
	if !strings.Contains(string(out), "audited bypass") {
		t.Errorf("expected reason text; got:\n%s", out)
	}
}
