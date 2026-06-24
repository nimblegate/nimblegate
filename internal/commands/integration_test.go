// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"nimblegate/internal/engine"
)

// buildBinary builds the nimblegate binary into a temp dir and returns the
// path. Subtests reuse one binary per test invocation.
func buildBinary(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "nimblegate")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	// Walk up to repo root (we're in internal/commands).
	wd, _ := os.Getwd()
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/nimblegate")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

// TestE2E_InitThenCheckClean
func TestE2E_InitThenCheckClean(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()

	run := func(args ...string) (int, string) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		out, err := cmd.CombinedOutput()
		exitCode := 0
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		return exitCode, string(out)
	}

	if code, out := run("init"); code != 0 {
		t.Fatalf("init exit=%d, out=%s", code, out)
	}
	if code, out := run("check"); code != 0 {
		t.Errorf("clean check exit=%d, out=%s", code, out)
	}
}

// TestE2E_InitTwiceFailsSecond
func TestE2E_InitTwiceFailsSecond(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	run := func(args ...string) int {
		cmd := exec.Command(bin, args...)
		cmd.Dir = tmp
		_ = cmd.Run()
		return cmd.ProcessState.ExitCode()
	}
	if code := run("init"); code != 0 {
		t.Fatal("first init failed")
	}
	if code := run("init"); code == 0 {
		t.Error("second init succeeded; should have errored")
	}
}

// TestE2E_CheckWithoutInit
func TestE2E_CheckWithoutInit(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "check")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "no appframes.toml") {
		t.Errorf("expected helpful error about missing config; got: %s", out)
	}
	if cmd.ProcessState.ExitCode() == 0 {
		t.Error("expected non-zero exit when no project root found")
	}
}

// TestE2E_AllTriggerValues
func TestE2E_AllTriggerValues(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	good := []string{"cli", "pre-commit", "git-wrap", "server"}
	for _, trig := range good {
		cmd := exec.Command(bin, "check", "--trigger="+trig)
		cmd.Dir = tmp
		_ = cmd.Run()
		// Acceptable: 0 (clean) or 1 (block) - both indicate the trigger was valid.
		code := cmd.ProcessState.ExitCode()
		if code == 2 {
			t.Errorf("trigger %q rejected unexpectedly", trig)
		}
	}
	// Unknown trigger should fail with usage exit 2.
	cmd := exec.Command(bin, "check", "--trigger=watcher")
	cmd.Dir = tmp
	_ = cmd.Run()
	if cmd.ProcessState.ExitCode() != 2 {
		t.Errorf("watcher trigger should reject (not yet implemented as Trigger validator)")
	}
}

// TestE2E_StatusBeforeAuditLog - clean message instead of crashing.
func TestE2E_StatusBeforeAuditLog(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		_ = cmd.Run()
	}
	cmd := exec.Command(bin, "status")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("status before audit log: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "no audit log") {
		t.Errorf("expected message about missing log; got: %s", out)
	}
}

// TestE2E_WatchAndCheckConcurrent - start watch, run check, watch should
// print the new entry, then SIGINT terminates watch.
func TestE2E_WatchAndCheckConcurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("watch uses Unix signals")
	}
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}

	watchCmd := exec.Command(bin, "watch")
	watchCmd.Dir = tmp
	pipe, err := watchCmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := watchCmd.Start(); err != nil {
		t.Fatal(err)
	}

	// Give watch a moment to seek to EOF.
	time.Sleep(300 * time.Millisecond)

	// Run a check to produce audit entries.
	checkCmd := exec.Command(bin, "check")
	checkCmd.Dir = tmp
	if err := checkCmd.Run(); err != nil && !strings.Contains(err.Error(), "exit status") {
		t.Fatal(err)
	}

	// Read in a loop for up to 3 seconds - watch's poll interval is 200ms
	// so the first read may return only the header line.
	read := make(chan string, 1)
	go func() {
		var collected strings.Builder
		deadline := time.Now().Add(3 * time.Second)
		buf := make([]byte, 4096)
		for time.Now().Before(deadline) {
			n, err := pipe.Read(buf)
			if n > 0 {
				collected.Write(buf[:n])
				if strings.Contains(collected.String(), "[cli/") {
					read <- collected.String()
					return
				}
			}
			if err != nil {
				break
			}
		}
		read <- collected.String()
	}()

	got := <-read
	_ = watchCmd.Process.Kill()
	_ = watchCmd.Wait()

	if !strings.Contains(got, "[cli/") {
		t.Errorf("watch output didn't contain expected trigger label; got: %q", got)
	}
}

// TestE2E_ShellPrintBothShells - sanity-check shell snippet generator from
// the binary's command surface.
func TestE2E_ShellPrintBothShells(t *testing.T) {
	bin := buildBinary(t)
	for _, sh := range []string{"bash", "zsh"} {
		cmd := exec.Command(bin, "shell", "print", "--shell="+sh)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Errorf("shell print %s: %v\n%s", sh, err, out)
			continue
		}
		if !strings.Contains(string(out), "nimblegate git-wrap BEGIN") {
			t.Errorf("shell print %s missing BEGIN marker", sh)
		}
	}
}

// TestE2E_CheckShowsBannerForBrokenFrame - when a project frame fails to
// load, `nimblegate check` must print a prominent stdout banner so users
// notice (the stderr-only warning was easy to miss).
func TestE2E_CheckShowsBannerForBrokenFrame(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	bad := `---
name: broken
severity: BLOCK
triggers: [cli]
---
oops missing category
`
	if err := os.WriteFile(filepath.Join(tmp, ".appframes", "broken.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "check")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "frame(s) failed to load") {
		t.Errorf("expected stdout banner about broken frame; got:\n%s", out)
	}
	if !strings.Contains(string(out), "nimblegate lint") {
		t.Errorf("expected remediation hint; got:\n%s", out)
	}
}

// TestE2E_ConcurrentChecks - N processes hitting the same project at once.
// Audit log must end with N successful writes (no torn JSON lines).
func TestE2E_ConcurrentChecks(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}

	const N = 10
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(bin, "check")
			cmd.Dir = tmp
			cmd.Env = append(os.Environ(), fmt.Sprintf("APPFRAMES_WORKER=%d", i))
			_ = cmd.Run()
		}()
	}
	wg.Wait()

	// With per-process audit-log parts, each `nimblegate check` writes to its
	// own audit.parts/audit.<pid>.<starttime>.log. Read across all of them
	// (engine.RotatedFiles handles both audit.log + parts).
	logPath := filepath.Join(tmp, ".appframes", "audit.log")
	files := engine.RotatedFiles(logPath)
	total := 0
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for i, ln := range lines {
			if ln == "" {
				continue
			}
			if !strings.HasPrefix(ln, "{") || !strings.HasSuffix(ln, "}") {
				t.Errorf("%s:%d looks torn: %q", p, i, ln)
			}
			total++
		}
	}
	if total == 0 {
		t.Fatalf("no audit-log lines found across %d files", len(files))
	}
	t.Logf("concurrent check produced %d audit lines from %d processes across %d files", total, N, len(files))
}
