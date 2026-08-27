package commands

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/gateway"
)

// The bug: "Apply recommended" passed KIT names to mergeFramePolicyGroups, which
// appended them to the frame-ID allowlist. On a fresh repo (empty list = every
// frame) that left a non-empty list matching nothing, i.e. gating off.
func TestMergeFramePolicyGroupsExpandsKits(t *testing.T) {
	policyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "fresh"), 0o755); err != nil {
		t.Fatal(err)
	}
	fp := gateway.FramePolicy{Enabled: nil, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, "fresh"); err != nil {
		t.Fatal(err)
	}

	out, err := mergeFramePolicyGroups(policyRoot, "fresh", []string{"core", "web-app"})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if dead := gateway.UnresolvableFrameEntries(out); len(dead) != 0 {
		t.Errorf("kit names leaked into the allowlist: %v", dead)
	}
	if len(out) < 18 {
		t.Errorf("expected core+web-app expanded to many frames, got %d: %v", len(out), out)
	}
}
