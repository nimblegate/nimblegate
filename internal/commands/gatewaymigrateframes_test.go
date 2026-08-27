package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
	"nimblegate/internal/kits"
)

func seedRepoPolicy(t *testing.T, policyRoot, repo string, enabled []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", repo), 0o755); err != nil {
		t.Fatal(err)
	}
	fp := gateway.FramePolicy{Enabled: enabled, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, repo); err != nil {
		t.Fatal(err)
	}
}

// A repo whose allowlist held only kit names was running NO frames. Repair must
// expand them, not merely report them.
func TestRepairFrameAllowlists(t *testing.T) {
	policyRoot := t.TempDir()
	seedRepoPolicy(t, policyRoot, "poisoned", []string{"core", "web-app"})
	seedRepoPolicy(t, policyRoot, "mixed", []string{"security/no-hardcoded-credentials", "core"})
	seedRepoPolicy(t, policyRoot, "clean", []string{"security/no-hardcoded-credentials"})
	seedRepoPolicy(t, policyRoot, "empty", nil)

	repaired, err := RepairFrameAllowlists(policyRoot)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(repaired) != 2 {
		t.Errorf("expected 2 repaired repos (poisoned, mixed), got %v", repaired)
	}

	ks, _ := kits.LoadStdlib()
	core, _ := ks.Get("core")

	fp, _ := gateway.LoadFramePolicy(policyRoot, "poisoned")
	if len(gateway.UnresolvableFrameEntries(fp.Enabled)) != 0 {
		t.Errorf("poisoned still has dead entries: %v", fp.Enabled)
	}
	if len(fp.Enabled) < len(core.Frames) {
		t.Errorf("poisoned should hold at least core's %d frames, got %d", len(core.Frames), len(fp.Enabled))
	}

	// The real frame the mixed repo already had must survive the rewrite.
	fp2, _ := gateway.LoadFramePolicy(policyRoot, "mixed")
	var kept bool
	for _, e := range fp2.Enabled {
		if e == "security/no-hardcoded-credentials" {
			kept = true
		}
	}
	if !kept {
		t.Errorf("mixed lost its pre-existing frame: %v", fp2.Enabled)
	}

	// Untouched repos must be left exactly as they were.
	fp3, _ := gateway.LoadFramePolicy(policyRoot, "clean")
	if len(fp3.Enabled) != 1 || fp3.Enabled[0] != "security/no-hardcoded-credentials" {
		t.Errorf("clean repo was modified: %v", fp3.Enabled)
	}
	fp4, _ := gateway.LoadFramePolicy(policyRoot, "empty")
	if len(fp4.Enabled) != 0 {
		t.Errorf("empty repo must stay empty (it runs every frame): %v", fp4.Enabled)
	}

	// The kit name is recorded where it belongs.
	doc, _ := os.ReadFile(filepath.Join(policyRoot, "poisoned", "appframes.toml"))
	if !strings.Contains(string(doc), "applied_kits") {
		t.Errorf("expected applied_kits recorded, got:\n%s", doc)
	}
}
