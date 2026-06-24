// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestReadFileBounded_returnsContentForSmallFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := ReadFileBounded(path, DefaultMaxFileBytes)
	if !ok {
		t.Fatal("ok = false; expected successful read")
	}
	if string(got) != "hello" {
		t.Errorf("got %q; want hello", got)
	}
}

func TestReadFileBounded_skipsOversizeFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "big.txt")
	// Just above the cap so we exercise the size guard without writing 1 MiB.
	if err := os.WriteFile(path, make([]byte, DefaultMaxFileBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok := ReadFileBounded(path, DefaultMaxFileBytes)
	if ok {
		t.Error("oversize file should be skipped (ok = false)")
	}
}

func TestReadFileBounded_skipsMissingFile(t *testing.T) {
	_, ok := ReadFileBounded("/does/not/exist/at/all", DefaultMaxFileBytes)
	if ok {
		t.Error("missing file should produce ok = false")
	}
}

// TestNoUnboundedReadInChecks is the regression-prevention gate. Every
// check file that calls os.ReadFile MUST either:
//
//   - be checkcommon.go itself (which implements the helper), OR
//   - also call info.Size() in the same file (the inline cap pattern used
//     by existing frames), OR
//   - call ReadFileBounded (the new canonical pattern).
//
// New frames that add raw os.ReadFile without one of these guards will
// fail this test at PR-review time. That's how the security pattern gets
// enforced "automatically" rather than depending on every author
// remembering - per .appframes/_design.md "Frame file-reading rule."
func TestNoUnboundedReadInChecks(t *testing.T) {
	rawReadFile := regexp.MustCompile(`\bos\.ReadFile\b`)
	sizeCheck := regexp.MustCompile(`\binfo\.Size\b`)
	bounded := regexp.MustCompile(`\bReadFileBounded\b`)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "checkcommon.go" {
			continue // the helper itself is allowed raw os.ReadFile
		}
		body, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !rawReadFile.Match(body) {
			continue // file doesn't read content at all
		}
		if sizeCheck.Match(body) || bounded.Match(body) {
			continue // bounded by inline Size() check or via helper
		}
		t.Errorf("%s: uses raw os.ReadFile without a size cap. "+
			"Replace with ReadFileBounded(path, DefaultMaxFileBytes) - see "+
			"checkcommon.go for the canonical pattern.", name)
	}
}
