// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

func runEnDashCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoEnDashInCommands(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoEnDashInCommands_EnDashFlagBlocks(t *testing.T) {
	body := "#!/bin/bash\ncurl –verbose https://example.com\n"
	got := runEnDashCheck(t, "deploy.sh", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoEnDashInCommands_EmDashFlagBlocks(t *testing.T) {
	body := "RUN apt-get install —yes curl\n"
	got := runEnDashCheck(t, "Dockerfile", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoEnDashInCommands_ProseDashPasses(t *testing.T) {
	// En-dash with whitespace on both sides - prose, not a flag.
	body := "# foo – bar – baz\necho hi\n"
	got := runEnDashCheck(t, "x.sh", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("prose en-dash should pass; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoEnDashInCommands_AsciiDashPasses(t *testing.T) {
	body := "curl --verbose https://example.com\n"
	got := runEnDashCheck(t, "deploy.sh", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestNoEnDashInCommands_PythonIgnored(t *testing.T) {
	body := "msg = \"–verbose\"\n"
	got := runEnDashCheck(t, "app.py", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".py is out of scope; got %s", got.Outcome)
	}
}

func TestNoEnDashInCommands_WorkflowYAMLMatched(t *testing.T) {
	body := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: curl –silent\n"
	got := runEnDashCheck(t, filepath.Join(".github", "workflows", "ci.yml"), body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("workflow YAML should match; got %s reason=%s", got.Outcome, got.Reason)
	}
}
