// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runBOMCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoBOM(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoBOM_LeadingBOMBlocks(t *testing.T) {
	body := "\uFEFFpackage main\n"
	got := runBOMCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoBOM_CSVExempt(t *testing.T) {
	body := "\uFEFFname,age\nAlice,30\n"
	got := runBOMCheck(t, "data.csv", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("CSV BOM should be tolerated; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoBOM_NoBOMPasses(t *testing.T) {
	body := "package main\n"
	got := runBOMCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestNoBOM_FileLevelMarkerSuppresses(t *testing.T) {
	body := "\uFEFF// appframes:disable encoding/no-bom\npackage main\n"
	got := runBOMCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s", got.Outcome)
	}
}
