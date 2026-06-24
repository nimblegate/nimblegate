// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"testing"

	"nimblegate/internal/config"
)

func TestLinterPolicy_addLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	lp, err := LoadLinterPolicy(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	lp = lp.With("no-fixme", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*.go"}, Regex: "FIXME"})
	if err := lp.Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLinterPolicy(root, "r")
	if err != nil {
		t.Fatal(err)
	}
	c, ok := got.Linters["no-fixme"]
	if !ok || c.Kind != "regex" || c.Regex != "FIXME" || c.Severity != "WARN" || !c.Enabled {
		t.Fatalf("round-trip lost data: %+v", got.Linters)
	}
}

func TestLinterPolicy_coexistsWithFrames(t *testing.T) {
	root := t.TempDir()
	// Save a frame severity override first.
	fp := FramePolicy{Enabled: []string{"security"}, Severity: map[string]string{"security/no-eval": "WARN"}}
	if err := fp.Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	// Now add a linter - must preserve the frames section.
	lp, _ := LoadLinterPolicy(root, "r")
	lp = lp.With("no-fixme", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*.go"}, Regex: "FIXME"})
	if err := lp.Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	// Frames survived?
	gotFP, _ := LoadFramePolicy(root, "r")
	if gotFP.Severity["security/no-eval"] != "WARN" || len(gotFP.Enabled) != 1 {
		t.Fatalf("linter save clobbered frames: %+v", gotFP)
	}
	// And a subsequent frame save must preserve the linter.
	gotFP = gotFP.WithSeverity("security/no-eval", "INFO")
	if err := gotFP.Save(root, "r"); err != nil {
		t.Fatal(err)
	}
	gotLP, _ := LoadLinterPolicy(root, "r")
	if _, ok := gotLP.Linters["no-fixme"]; !ok {
		t.Fatalf("frame save clobbered linter: %+v", gotLP.Linters)
	}
}

func TestLinterPolicy_deleteSeverityEnabled(t *testing.T) {
	root := t.TempDir()
	lp, _ := LoadLinterPolicy(root, "r")
	lp = lp.With("a", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "WARN", Patterns: []string{"*"}, Regex: "x"})
	lp = lp.With("b", config.LinterConfig{Enabled: true, Kind: "regex", Severity: "BLOCK", Patterns: []string{"*"}, Regex: "y"})
	lp.Save(root, "r")

	lp, _ = LoadLinterPolicy(root, "r")
	lp = lp.SetSeverity("a", "INFO").SetEnabled("b", false).Delete("a")
	// Delete("a") after SetSeverity("a") → "a" gone; "b" present, disabled.
	lp.Save(root, "r")

	got, _ := LoadLinterPolicy(root, "r")
	if _, ok := got.Linters["a"]; ok {
		t.Errorf("a should be deleted")
	}
	if c := got.Linters["b"]; c.Enabled {
		t.Errorf("b should be disabled")
	}
}
