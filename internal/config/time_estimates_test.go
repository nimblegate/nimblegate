// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTimeEstimates_Lookup(t *testing.T) {
	v2 := 1.5
	v6 := 0.05
	te := TimeEstimates{Tier2: &v2, Tier6: &v6}

	if got, ok := te.Lookup(2); !ok || got != 1.5 {
		t.Errorf("Lookup(2) = (%v, %v); want (1.5, true)", got, ok)
	}
	if got, ok := te.Lookup(6); !ok || got != 0.05 {
		t.Errorf("Lookup(6) = (%v, %v); want (0.05, true)", got, ok)
	}
	if _, ok := te.Lookup(1); ok {
		t.Error("Lookup(1) should be unset")
	}
	if _, ok := te.Lookup(0); ok {
		t.Error("Lookup(0) should be unset (out of range)")
	}
	if _, ok := te.Lookup(7); ok {
		t.Error("Lookup(7) should be unset (out of range)")
	}
}

func TestLoadProject_TimeEstimatesSection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "appframes.toml")
	content := `[project]
name = "test"
[frames]
enabled = ["security/*"]
[time-estimates]
tier-1 = 6.0
tier-3 = 1.0
`
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProject(cfgPath)
	if err != nil {
		t.Fatalf("LoadProject: %v", err)
	}
	if v, ok := cfg.TimeEstimates.Lookup(1); !ok || v != 6.0 {
		t.Errorf("tier-1 override = (%v, %v); want (6.0, true)", v, ok)
	}
	if v, ok := cfg.TimeEstimates.Lookup(3); !ok || v != 1.0 {
		t.Errorf("tier-3 override = (%v, %v); want (1.0, true)", v, ok)
	}
	if _, ok := cfg.TimeEstimates.Lookup(2); ok {
		t.Error("tier-2 not declared but Lookup returned a value")
	}
}

func TestProjectConfig_Validate(t *testing.T) {
	negativeOne := -1.0
	cfg := ProjectConfig{TimeEstimates: TimeEstimates{Tier1: &negativeOne}}
	if err := cfg.Validate(); err == nil {
		t.Error("negative tier-1 should fail Validate")
	}

	clean := ProjectConfig{}
	if err := clean.Validate(); err != nil {
		t.Errorf("clean config should pass Validate, got %v", err)
	}

	pos := 2.5
	withOverride := ProjectConfig{TimeEstimates: TimeEstimates{Tier2: &pos}}
	if err := withOverride.Validate(); err != nil {
		t.Errorf("positive override should pass Validate, got %v", err)
	}
}
