// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProject_LintersTable(t *testing.T) {
	tmp := t.TempDir()
	configContent := `
[linters.go-vet]
enabled = true
severity = "warn"
`
	path := filepath.Join(tmp, "appframes.toml")
	if err := os.WriteFile(path, []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if !cfg.Linters["go-vet"].Enabled {
		t.Error("expected [linters.go-vet] enabled = true")
	}
	if cfg.Linters["go-vet"].Severity != "warn" {
		t.Errorf("severity = %q, want \"warn\"", cfg.Linters["go-vet"].Severity)
	}
}

func TestLoadProject_LintersAbsentDefaultsOff(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "appframes.toml")
	if err := os.WriteFile(path, []byte("[project]\nname = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}
	if cfg.Linters["go-vet"].Enabled {
		t.Error("go vet should default to disabled when [linters] is absent")
	}
}

func TestLoadProject_FullExample(t *testing.T) {
	tmp := t.TempDir()
	configContent := `
[project]
name = "test-project"
version = "0.1"

[frames]
enabled = ["git-safety/*", "security/no-innerHTML-user-input"]

[triggers]
git-wrap = true
watcher = false
pre-commit = true
cli = true
server = false

[frames.security.no-innerHTML-user-input]
severity = "WARN"

[canonical]
folder-branch-map = ".appframes/_canonical/folder-branch-map.toml"
`
	path := filepath.Join(tmp, "appframes.toml")
	if err := os.WriteFile(path, []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadProject(path)
	if err != nil {
		t.Fatalf("LoadProject error: %v", err)
	}

	if cfg.Project.Name != "test-project" {
		t.Errorf("Name = %q", cfg.Project.Name)
	}
	if len(cfg.Frames.Enabled) != 2 {
		t.Errorf("Enabled = %v", cfg.Frames.Enabled)
	}
	if !cfg.Triggers["git-wrap"] || cfg.Triggers["watcher"] {
		t.Errorf("Triggers = %v", cfg.Triggers)
	}
	override, ok := cfg.FrameOverrides["security/no-innerHTML-user-input"]
	if !ok {
		t.Fatal("no override for security/no-innerHTML-user-input")
	}
	if override.Severity != "WARN" {
		t.Errorf("Severity override = %q, want WARN", override.Severity)
	}
}

func TestLoadProject_MissingFileReturnsDefaults(t *testing.T) {
	cfg, err := LoadProject(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatalf("expected nil error for missing file (defaults), got %v", err)
	}
	if cfg.Triggers == nil {
		t.Error("expected non-nil Triggers map even on missing file")
	}
}
