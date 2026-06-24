// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"nimblegate/internal/engine"
)

func writeGoMod(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverGoModDirs_subdirAndNestedModules(t *testing.T) {
	root := t.TempDir()
	// myapp shape: no module at root; modules in subdirs incl. a nested one.
	writeGoMod(t, filepath.Join(root, "images", "api"))
	writeGoMod(t, filepath.Join(root, "images", "api", "stresstest")) // nested module
	writeGoMod(t, filepath.Join(root, "node_modules", "pkg"))         // excluded
	writeGoMod(t, filepath.Join(root, "_archive", "old"))             // excluded
	writeGoMod(t, filepath.Join(root, ".git", "weird"))               // .git always pruned

	got := discoverGoModDirs(root, []string{"node_modules", "_archive"})
	want := []string{
		filepath.Join(root, "images", "api"),
		filepath.Join(root, "images", "api", "stresstest"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("discoverGoModDirs = %v, want %v", got, want)
	}
}

func TestDiscoverGoModDirs_rootModule(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root)
	writeGoMod(t, filepath.Join(root, "tools"))

	got := discoverGoModDirs(root, nil)
	want := []string{root, filepath.Join(root, "tools")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("discoverGoModDirs = %v, want %v", got, want)
	}
}

func TestDiscoverGoModDirs_noModules(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := discoverGoModDirs(root, nil); len(got) != 0 {
		t.Errorf("expected no modules, got %v", got)
	}
}

func hit(file string, line int) engine.Hit {
	return engine.Hit{File: file, Line: line, Label: "printf: boom"}
}

func TestAggregateGoVet_hitsAcrossModulesMergeSorted(t *testing.T) {
	runs := []goVetModuleRun{
		{Dir: "images/api", Result: engine.CheckResult{Outcome: engine.OutcomeBlock, Hits: []engine.Hit{hit("images/api/h.go", 10)}}},
		{Dir: "tools", Result: engine.CheckResult{Outcome: engine.OutcomeBlock, Hits: []engine.Hit{hit("tools/a.go", 2)}}},
	}
	got := aggregateGoVet(runs, engine.OutcomeBlock, nil)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("Outcome = %q, want BLOCK", got.Outcome)
	}
	if len(got.Hits) != 2 || got.Hits[0].File != "images/api/h.go" || got.Hits[1].File != "tools/a.go" {
		t.Fatalf("hits not merged+sorted: %+v", got.Hits)
	}
	want := "go vet: images/api/h.go:10 - printf: boom; tools/a.go:2 - printf: boom"
	if got.Reason != want {
		t.Errorf("Reason = %q, want %q", got.Reason, want)
	}
}

func TestAggregateGoVet_severityHonoredForHits(t *testing.T) {
	runs := []goVetModuleRun{
		{Dir: "a", Result: engine.CheckResult{Outcome: engine.OutcomeWarn, Hits: []engine.Hit{hit("a/x.go", 1)}}},
	}
	if got := aggregateGoVet(runs, engine.OutcomeWarn, nil); got.Outcome != engine.OutcomeWarn {
		t.Errorf("Outcome = %q, want WARN", got.Outcome)
	}
}

func TestAggregateGoVet_buildErrorModuleWarnsWhenNoHits(t *testing.T) {
	runs := []goVetModuleRun{
		{Dir: "images/api", Result: engine.CheckResult{Outcome: engine.OutcomeWarn, Reason: "go vet: could not analyze ..."}},
		{Dir: "clean", Result: engine.CheckResult{Outcome: engine.OutcomePass, Reason: "go vet: no findings"}},
	}
	got := aggregateGoVet(runs, engine.OutcomeBlock, nil)
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("Outcome = %q, want WARN", got.Outcome)
	}
	if !contains(got.Reason, "images/api") {
		t.Errorf("Reason should name the failing module, got %q", got.Reason)
	}
}

func TestAggregateGoVet_allCleanIsPass(t *testing.T) {
	runs := []goVetModuleRun{
		{Dir: "a", Result: engine.CheckResult{Outcome: engine.OutcomePass}},
		{Dir: "b", Result: engine.CheckResult{Outcome: engine.OutcomePass}},
	}
	got := aggregateGoVet(runs, engine.OutcomeBlock, nil)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("Outcome = %q, want PASS", got.Outcome)
	}
}

func TestAggregateGoVet_noModulesIsSkip(t *testing.T) {
	got := aggregateGoVet(nil, engine.OutcomeBlock, nil)
	if got.Outcome != engine.OutcomeSkip {
		t.Errorf("Outcome = %q, want SKIP (no Go modules)", got.Outcome)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
