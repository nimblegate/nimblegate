// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestLint_FrameStatusTableShowsEnabledIcon - enabled stdlib frames
// appear with the ✓ icon; disabled frames appear with ⊘.
func TestLint_FrameStatusTableShowsEnabledIcon(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Overwrite the wildcard config produced by init with flat frame IDs.
	flatCfg := `[project]
name = "test"
version = "0.1"

[frames]
enabled = [
    "git/folder-branch-lock",
    "security/no-hardcoded-credentials",
    "filesystem/rm-rf-protected-paths",
]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lint clean project: %v\n%s", err, out)
	}
	for _, want := range []string{
		"Frame status:",
		"✓ ",
		"git/folder-branch-lock",
		"security/no-hardcoded-credentials",
		"filesystem/rm-rf-protected-paths",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q; got:\n%s", want, out)
		}
	}
}

// TestLint_DisabledIcon_ProjectFrameNotInEnabledList - a project frame
// whose ID is not in the flat enabled list appears as ⊘.
func TestLint_DisabledIcon_ProjectFrameNotInEnabledList(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Flat config that does NOT include documentation/my-rule.
	flatCfg := `[project]
name = "test"
version = "0.1"

[frames]
enabled = ["git/folder-branch-lock"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	// Drop a custom project frame in documentation/ (not in the enabled list).
	dir := filepath.Join(tmp, ".appframes", "documentation")
	_ = os.MkdirAll(dir, 0o755)
	body := `---
name: my-rule
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
---
project rule
`
	if err := os.WriteFile(filepath.Join(dir, "my-rule.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	// documentation/my-rule is not in the enabled list → must appear with ⊘.
	lines := strings.Split(string(out), "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "documentation/my-rule") && strings.Contains(line, "⊘") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("documentation/my-rule should appear with ⊘ (disabled) icon; got:\n%s", out)
	}
}

// TestLint_MismatchDetectionFlagsTypos - a flat ID in the enabled list
// that doesn't match any loaded frame fires the mismatch warning and
// exits non-zero.
func TestLint_MismatchDetectionFlagsTypos(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Flat config with one valid ID and one deliberate typo.
	flatCfg := `[project]
name = "test"
version = "0.1"

[frames]
enabled = [
    "git/folder-branch-lock",
    "security/no-tyop-frame",
]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if !strings.Contains(string(out), "enables 1 frame(s) that don't exist") {
		t.Errorf("expected mismatch warning; got:\n%s", out)
	}
	if !strings.Contains(string(out), "security/no-tyop-frame") {
		t.Errorf("mismatch should name the typo id; got:\n%s", out)
	}
	if cmd.ProcessState.ExitCode() == 0 {
		t.Error("mismatch should cause exit 1 (CI catches typos)")
	}
}

// TestLint_AcceptsInitConfig_Binary - init now writes flat frame IDs (core
// kit), so lint must accept the freshly-scaffolded config and exit zero.
func TestLint_AcceptsInitConfig_Binary(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() != 0 {
		t.Errorf("lint should accept flat config produced by init; got exit %d:\n%s",
			cmd.ProcessState.ExitCode(), out)
	}
	if strings.Contains(string(out), "removed syntax") {
		t.Errorf("lint reported migration error on fresh init config; got:\n%s", out)
	}
}

// TestLint_UnboundFrameShowsBomb - a project frame with no Go check
// function must be marked 💥 in the status table.
func TestLint_UnboundFrameShowsBomb(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	{
		cmd := exec.Command(bin, "init")
		cmd.Dir = tmp
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
	// Flat config that explicitly enables the unbound project frame.
	flatCfg := `[project]
name = "test"
version = "0.1"

[frames]
enabled = ["documentation/unbound"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Drop an unbound project frame in documentation/ (explicitly enabled above).
	dir := filepath.Join(tmp, ".appframes", "documentation")
	_ = os.MkdirAll(dir, 0o755)
	body := `---
name: unbound
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
---
no Go check function bound for this id
`
	if err := os.WriteFile(filepath.Join(dir, "unbound.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	lines := strings.Split(string(out), "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "documentation/unbound") && strings.Contains(line, "💥") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("documentation/unbound should appear with 💥 (no check bound) icon; got:\n%s", out)
	}
}
