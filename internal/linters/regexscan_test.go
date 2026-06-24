// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanRegexContent_matchesByGlobAndLine(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "ok\nTODO(no-owner) fix\nok\n")
	writeFile(t, root, "sub/b.go", "TODO(no-owner)\n")
	writeFile(t, root, "c.txt", "TODO(no-owner)\n") // excluded by glob

	re := regexp.MustCompile(`TODO\(no-owner\)`)
	hits, err := ScanRegexContent(root, []string{"*.go"}, re, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want 2: %+v", len(hits), hits)
	}
	// a.go line 2, sub/b.go line 1 (sorted by file then line)
	if hits[0].File != "a.go" || hits[0].Line != 2 {
		t.Errorf("hit0 = %s:%d, want a.go:2", hits[0].File, hits[0].Line)
	}
	if hits[1].File != filepath.Join("sub", "b.go") || hits[1].Line != 1 {
		t.Errorf("hit1 = %s:%d, want sub/b.go:1", hits[1].File, hits[1].Line)
	}
}

func TestScanRegexContent_excludedDirsSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "x.go", "MATCH\n")
	writeFile(t, root, "vendor/y.go", "MATCH\n")
	re := regexp.MustCompile(`MATCH`)
	hits, err := ScanRegexContent(root, []string{"*.go"}, re, []string{"vendor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].File != "x.go" {
		t.Fatalf("got %+v, want only x.go", hits)
	}
}

func TestScanRegexContent_emptyPatternsMatchesAll(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.go", "Z\n")
	writeFile(t, root, "b.txt", "Z\n")
	re := regexp.MustCompile(`Z`)
	hits, err := ScanRegexContent(root, nil, re, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d, want 2 (empty patterns = all files)", len(hits))
	}
}

func TestRunEnabled_regexKindRuns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "x := 1 // FIXME\n")
	lc := map[string]config.LinterConfig{
		"no-fixme": {Enabled: true, Kind: "regex", Severity: "warn", Patterns: []string{"*.go"}, Regex: "FIXME"},
	}
	results, ran := RunEnabled(lc, root, nil)
	// RunEnabled returns lint.ID() which is "app-correctness/<name>"
	if len(ran) != 1 || ran[0] != "app-correctness/no-fixme" {
		t.Fatalf("ranIDs = %v, want [app-correctness/no-fixme]", ran)
	}
	if len(results) != 1 || results[0].Outcome != engine.OutcomeWarn {
		t.Fatalf("results = %+v, want one OutcomeWarn", results)
	}
}

func TestScanRegexContent_scannerErrorPropagated(t *testing.T) {
	root := t.TempDir()
	// A line longer than the 1 MiB scanner cap triggers bufio.ErrTooLong.
	writeFile(t, root, "big.go", strings.Repeat("X", 1024*1024+1)+"\n")
	re := regexp.MustCompile(`X`)
	_, err := ScanRegexContent(root, []string{"*.go"}, re, nil)
	if err == nil {
		t.Fatal("expected error for oversized line, got nil")
	}
}

func TestRunEnabled_commandKindUnaffected(t *testing.T) {
	root := t.TempDir()
	// kind unset + no command → existing customLinter SKIP path (no crash, no regex scan)
	lc := map[string]config.LinterConfig{
		"mytool": {Enabled: true, Severity: "block"},
	}
	results, _ := RunEnabled(lc, root, nil)
	if len(results) != 1 || results[0].Outcome != engine.OutcomeSkip {
		t.Fatalf("results = %+v, want one OutcomeSkip (no command)", results)
	}
}
