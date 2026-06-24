// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFramePolicy_RoundTrip(t *testing.T) {
	root := t.TempDir()
	fp := FramePolicy{
		Enabled:  []string{"@tier-1", "@web"},
		Severity: map[string]string{"security/no-private-keys-in-repo": "WARN"},
	}
	if err := fp.Save(root, "demo"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "appframes.toml")); err != nil {
		t.Fatalf("policy file not written: %v", err)
	}
	got, err := LoadFramePolicy(root, "demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got.Enabled) != 2 || got.Enabled[0] != "@tier-1" || got.Enabled[1] != "@web" {
		t.Errorf("enabled round-trip wrong: %v", got.Enabled)
	}
	if got.Severity["security/no-private-keys-in-repo"] != "WARN" {
		t.Errorf("severity override (id with '/') did not round-trip: %v", got.Severity)
	}
}

func TestFramePolicy_WithSeverity_preserves(t *testing.T) {
	fp := FramePolicy{Enabled: []string{"@tier-1"}, Severity: map[string]string{"a/x": "WARN"}}
	out := fp.WithSeverity("b/y", "INFO")
	if out.Severity["a/x"] != "WARN" || out.Severity["b/y"] != "INFO" {
		t.Errorf("WithSeverity must preserve other overrides: %v", out.Severity)
	}
	if len(out.Enabled) != 1 || out.Enabled[0] != "@tier-1" {
		t.Errorf("WithSeverity must preserve enabled: %v", out.Enabled)
	}
	if _, ok := fp.Severity["b/y"]; ok {
		t.Error("WithSeverity mutated the receiver's map")
	}
}

func TestLoadFramePolicy_missing(t *testing.T) {
	got, err := LoadFramePolicy(t.TempDir(), "nope")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got.Enabled) != 0 || len(got.Severity) != 0 {
		t.Errorf("missing should yield empty policy: %+v", got)
	}
}

func TestLoadTimeEstimates(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repoA")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "[frames]\nenabled = [\"@tier-1\"]\n\n[time-estimates]\ntier-1 = 6.0\n"
	if err := os.WriteFile(filepath.Join(dir, "appframes.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	te, err := LoadTimeEstimates(root, "repoA")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := te.Lookup(1); !ok || v != 6.0 {
		t.Errorf("tier-1 override = %v,%v want 6.0,true", v, ok)
	}

	// Missing policy file → zero value, no error (matches LoadFramePolicy).
	te2, err := LoadTimeEstimates(root, "nope")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if _, ok := te2.Lookup(1); ok {
		t.Error("missing policy must yield no tier-1 override")
	}
}
