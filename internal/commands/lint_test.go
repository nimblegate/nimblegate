// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLint_CleanProject(t *testing.T) {
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
    "git/no-force-push-main",
    "security/no-hardcoded-credentials",
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
	if !strings.Contains(string(out), "All frames parsed cleanly") {
		t.Errorf("expected clean-status line; got: %s", out)
	}
}

func TestLint_PartialFailure(t *testing.T) {
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
enabled = ["git/folder-branch-lock"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Drop a broken frame into .appframes/.
	bad := `---
name: broken
severity: BLOCK
triggers: [cli]
---
missing category
`
	badPath := filepath.Join(tmp, ".appframes", "broken.md")
	if err := os.WriteFile(badPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, _ := cmd.CombinedOutput()
	code := cmd.ProcessState.ExitCode()

	if code != 1 {
		t.Errorf("lint exit=%d, want 1 (broken frame present)", code)
	}
	if !strings.Contains(string(out), "Problems") {
		t.Errorf("expected 'Problems' section; got: %s", out)
	}
	if !strings.Contains(string(out), "broken.md") {
		t.Errorf("expected broken frame path in output; got: %s", out)
	}
}

func TestLint_DetectsProjectOverride(t *testing.T) {
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
enabled = ["git/folder-branch-lock"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Project frame that overrides a stdlib frame ID. Must use the same
	// category/name as the stdlib frame so the IDs match (category: git, name: folder-branch-lock).
	override := `---
name: folder-branch-lock
category: git
subcategory: branch-discipline
severity: WARN
triggers: [cli, git-wrap]
---
project override
`
	dir := filepath.Join(tmp, ".appframes", "git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder-branch-lock.md"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lint with override: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Project overrides of stdlib frames") {
		t.Errorf("expected override section; got: %s", out)
	}
	if !strings.Contains(string(out), "git/folder-branch-lock") {
		t.Errorf("expected overridden frame id; got: %s", out)
	}
}

func TestLint_WithoutInit(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	_, _ = cmd.CombinedOutput()
	if cmd.ProcessState.ExitCode() == 0 {
		t.Error("expected non-zero exit when no project root")
	}
}

// TestLint_FlagsSeverityDowngrade - a project frame that overrides a
// stdlib frame with weaker severity must be loudly reported. This is the
// most realistic "malicious frame" attack surface.
func TestLint_FlagsSeverityDowngrade(t *testing.T) {
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
enabled = ["security/no-innerHTML-user-input"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stdlib security/no-innerHTML-user-input is BLOCK. Override to INFO.
	weak := `---
name: no-innerHTML-user-input
category: security
subcategory: content-safety
severity: INFO
triggers: [cli, pre-commit]
---
weakened
`
	dir := filepath.Join(tmp, ".appframes", "security")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "no-innerHTML-user-input.md"), []byte(weak), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lint with downgrade: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "SEVERITY DOWNGRADED") {
		t.Errorf("expected severity-downgrade flag; got: %s", out)
	}
	if !strings.Contains(string(out), "weaken stdlib severity") {
		t.Errorf("expected summary line about weakened severities; got: %s", out)
	}
}

func TestLintRejectsAtPrefixedConfig(t *testing.T) {
	tmp := t.TempDir()
	cfg := tmp + "/appframes.toml"
	if err := os.MkdirAll(filepath.Join(tmp, ".appframes"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[project]
name = "test"
version = "0.1"

[frames]
enabled = ["@tier-1", "security/no-hardcoded-credentials"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rc := lintAt(tmp, &buf)
	if rc == 0 {
		t.Fatalf("lint returned 0; expected non-zero for @-prefixed config")
	}
	if !strings.Contains(buf.String(), "nimblegate kits apply core") {
		t.Errorf("expected migration table in output; got: %s", buf.String())
	}
}

func TestLintRejectsWildcardConfig(t *testing.T) {
	tmp := t.TempDir()
	cfg := tmp + "/appframes.toml"
	if err := os.MkdirAll(filepath.Join(tmp, ".appframes"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[project]
name = "test"
version = "0.1"

[frames]
enabled = ["security/*"]
`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	rc := lintAt(tmp, &buf)
	if rc == 0 {
		t.Fatal("lint returned 0; expected non-zero for wildcard config")
	}
}

// TestLint_OverrideWithSameSeverityNotFlagged - a project frame that
// overrides without changing severity is not a downgrade.
func TestLint_OverrideWithSameSeverityNotFlagged(t *testing.T) {
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
enabled = ["security/no-innerHTML-user-input"]

[triggers]
cli = true
`
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte(flatCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	same := `---
name: no-innerHTML-user-input
category: security
subcategory: content-safety
severity: BLOCK
triggers: [cli, pre-commit]
---
same severity, custom body
`
	dir := filepath.Join(tmp, ".appframes", "security")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "no-innerHTML-user-input.md"), []byte(same), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "lint")
	cmd.Dir = tmp
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lint with same-severity override should succeed: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "SEVERITY DOWNGRADED") {
		t.Errorf("override with same severity should not be flagged; got: %s", out)
	}
}
