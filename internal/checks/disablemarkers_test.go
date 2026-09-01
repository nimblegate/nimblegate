// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A marker naming a frame that does not exist silences nothing. The shipped
// corpus for doc-touches-with-code carried `convention/doc-touches-with-code`
// while the frame declares `category: documentation`, and it went unnoticed
// from the first public release because nothing looked at marker IDs.
func TestUnknownDisableMarkers(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("src/good.go", "// appframes:disable security/no-hardcoded-credentials\npackage a\n")
	write("src/disabled-frame.go", "// appframes:disable encoding/no-bom\npackage a\n")
	write("src/typo.go", "// appframes:disable convention/doc-touches-with-code\npackage a\n")
	write("src/nextline.go", "// appframes:disable-next-line security/no-such-frame\npackage a\n")
	write("node_modules/pkg/index.js", "// appframes:disable made/up\n")
	write("docs/notes.md", "Write `appframes:disable` with the frame id shown in the report.\n")

	known := map[string]bool{
		"security/no-hardcoded-credentials":   true,
		"encoding/no-bom":                     true, // exists but switched off here
		"documentation/doc-touches-with-code": true,
	}
	got := UnknownDisableMarkers(root, known)

	if len(got) != 2 {
		t.Fatalf("want 2 unknown markers, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"src/typo.go:1", "convention/doc-touches-with-code", "src/nextline.go:1", "security/no-such-frame"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	// A frame that exists but is disabled in this repo is a legitimate
	// suppression; a prose mention is not a marker; vendored code is not ours.
	for _, unwanted := range []string{"encoding/no-bom", "node_modules", "docs/notes.md"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("should not report %q:\n%s", unwanted, joined)
		}
	}
}
