// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

// plantESLint commits what a hostile push would: an executable named
// node_modules/.bin/eslint that leaves a marker when run.
func plantESLint(t *testing.T, root string) (marker string) {
	t.Helper()
	marker = filepath.Join(t.TempDir(), "ran")
	bin := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ntouch " + marker + "\necho '[]'\n"
	if err := os.WriteFile(filepath.Join(bin, "eslint"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return marker
}

func TestRunEnabledUntrusted_neverRunsRepoCodeLinters(t *testing.T) {
	root := t.TempDir()
	marker := plantESLint(t, root)
	lc := map[string]config.LinterConfig{
		"eslint": {Enabled: true},
		"go-vet": {Enabled: true},
	}
	results, ran := RunEnabledUntrusted(lc, root, nil)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("the pushed node_modules/.bin/eslint was executed")
	}
	if len(results) != 2 || len(ran) != 2 {
		t.Fatalf("want a result per enabled linter, got %d results, %d ids", len(results), len(ran))
	}
	for _, r := range results {
		if r.Outcome != engine.OutcomeSkip || !strings.Contains(r.Reason, "not run at the gateway") {
			t.Errorf("%s: outcome %v reason %q; want a SKIP saying it is not run at the gateway", r.FrameID, r.Outcome, r.Reason)
		}
	}
}

// Locally, running the project's own tools is the point; only the gateway
// treats the tree as untrusted.
func TestRunEnabled_trustedTreeStillUsesProjectESLint(t *testing.T) {
	root := t.TempDir()
	marker := plantESLint(t, root)
	RunEnabled(map[string]config.LinterConfig{"eslint": {Enabled: true}}, root, nil)
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("local runs should use the project's own eslint")
	}
}

// A regex check named like a built-in linter stays a regex check: before this,
// a dashboard regex check named "eslint" ran the eslint adapter instead.
func TestRunEnabled_regexKindWinsOverBuiltinName(t *testing.T) {
	root := t.TempDir()
	marker := plantESLint(t, root)
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("TODO here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lc := map[string]config.LinterConfig{
		"eslint": {Enabled: true, Kind: "regex", Regex: "TODO", Patterns: []string{"*.txt"}, Severity: "WARN"},
	}
	for _, run := range []func(map[string]config.LinterConfig, string, []string) ([]engine.CheckResult, []string){RunEnabled, RunEnabledUntrusted} {
		results, _ := run(lc, root, nil)
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("a regex check named eslint ran the eslint adapter")
		}
		if len(results) != 1 || results[0].Outcome != engine.OutcomeWarn {
			t.Fatalf("want one WARN from the regex check, got %+v", results)
		}
	}
}

func TestRunEnabledUntrusted_customCommandMustNotPointIntoTree(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	if err := os.WriteFile(filepath.Join(root, "lint.sh"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lc := map[string]config.LinterConfig{
		"mine": {Enabled: true, Command: "./lint.sh", Regex: `(?P<file>\S+):(?P<line>\d+): (?P<msg>.*)`},
	}
	results, _ := RunEnabledUntrusted(lc, root, nil)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a custom linter command inside the pushed tree was executed")
	}
	if len(results) != 1 || results[0].Outcome != engine.OutcomeSkip || !strings.Contains(results[0].Reason, "points into the pushed tree") {
		t.Fatalf("want a SKIP naming the problem, got %+v", results)
	}

	// A command found on PATH still runs.
	lc["mine"] = config.LinterConfig{Enabled: true, Command: "echo", Args: []string{"a.txt:1: from echo"}, Regex: `(?P<file>\S+):(?P<line>\d+): (?P<msg>.*)`}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, _ = RunEnabledUntrusted(lc, root, nil)
	if len(results) != 1 || results[0].Outcome == engine.OutcomeSkip {
		t.Fatalf("a command on PATH should run, got %+v", results)
	}
}
