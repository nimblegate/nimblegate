// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeWebsiteIDsTable(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "website-ids.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCrossBranchID_MatchingIDPasses(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"example.com" = "abc-123"
"other.com" = "def-456"
`)
	writeFile(t, root, "index.html", `<script data-website-id="abc-123" src="..."></script>`)
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, reason = %q", got.Outcome, got.Reason)
	}
}

func TestCrossBranchID_UnknownIDWarns(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"example.com" = "abc-123"
`)
	writeFile(t, root, "index.html", `<script data-website-id="wrong-id" src="..."></script>`)
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN; reason = %q", got.Outcome, got.Reason)
	}
}

func TestCrossBranchID_NoTableSkips(t *testing.T) {
	root := t.TempDir()
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerCLI,
		ProjectRoot: root,
	})
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("outcome = %s, want SKIP", got.Outcome)
	}
}
