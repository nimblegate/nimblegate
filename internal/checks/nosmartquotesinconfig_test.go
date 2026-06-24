// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runSmartQuotesCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoSmartQuotesInConfig(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoSmartQuotesInConfig_TOMLBlocks(t *testing.T) {
	body := "key = “something”\n"
	got := runSmartQuotesCheck(t, "config.toml", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK; reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoSmartQuotesInConfig_YAMLBlocks(t *testing.T) {
	body := "name: ‘alice’\n"
	got := runSmartQuotesCheck(t, "vals.yaml", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoSmartQuotesInConfig_ComposeBasenameMatched(t *testing.T) {
	body := "services:\n  app:\n    image: “foo”\n"
	got := runSmartQuotesCheck(t, "compose.yaml", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("compose.yaml should match; got %s", got.Outcome)
	}
}

func TestNoSmartQuotesInConfig_PythonIgnored(t *testing.T) {
	// .py is out of scope - strings may legitimately use curly quotes.
	body := "msg = “hi”\n"
	got := runSmartQuotesCheck(t, "app.py", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf(".py is out of scope; got %s", got.Outcome)
	}
}

func TestNoSmartQuotesInConfig_CleanPasses(t *testing.T) {
	body := "key = \"value\"\n"
	got := runSmartQuotesCheck(t, "config.toml", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}
