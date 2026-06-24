// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runYAMLNoTabsCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return YAMLNoTabs(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestYAMLNoTabs_LeadingTabBlocks(t *testing.T) {
	body := "services:\n\tapp:\n\t\timage: foo\n"
	got := runYAMLNoTabsCheck(t, "compose.yaml", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestYAMLNoTabs_TabAfterSpacesBlocks(t *testing.T) {
	body := "services:\n  \tapp:\n"
	got := runYAMLNoTabsCheck(t, "x.yml", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestYAMLNoTabs_TabInValueIgnored(t *testing.T) {
	// Tab inside a quoted scalar after the colon - not in indentation.
	body := "key: \"value\\twith tab\"\n"
	got := runYAMLNoTabsCheck(t, "x.yaml", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("tab in value should be ignored; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestYAMLNoTabs_SpacesPasses(t *testing.T) {
	body := "services:\n  app:\n    image: foo\n"
	got := runYAMLNoTabsCheck(t, "compose.yml", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestYAMLNoTabs_NonYAMLIgnored(t *testing.T) {
	body := "\tdef x():\n\t\tpass\n"
	got := runYAMLNoTabsCheck(t, "app.py", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".py is out of scope; got %s", got.Outcome)
	}
}
