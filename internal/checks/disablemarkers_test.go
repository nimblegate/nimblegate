// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// A marker naming a frame that does not exist silences nothing. The shipped
// corpus for doc-touches-with-code carried `convention/doc-touches-with-code`
// while the frame declares `category: documentation`, and it went unnoticed
// from the first public release because nothing looked at marker IDs.
func TestUnknownDisableMarkers(t *testing.T) {
	// Assembled at runtime: a literal marker in this file would be a finding
	// when nimblegate scans its own repo, which is the frame working, not a bug.
	mark := func(id string) string { return "// appframes" + ":disable " + id + "\n" }
	markNext := func(id string) string { return "// appframes" + ":disable-next-line " + id + "\n" }
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
	write("src/good.go", mark("security/no-hardcoded-credentials")+"package a\n")
	// Frame names are not all lowercase; a lowercase-only ID class truncated
	// this one and reported a valid marker as naming no frame.
	write("src/mixed-case.js", mark("security/no-innerHTML-user-input")+"const a = 1;\n")
	write("src/disabled-frame.go", mark("encoding/no-bom")+"package a\n")
	write("src/typo.go", mark("convention/doc-touches-with-code")+"package a\n")
	write("src/nextline.go", markNext("security/no-such-frame")+"package a\n")
	write("node_modules/pkg/index.js", mark("made/up"))
	write("docs/notes.md", "Write the marker with the frame id shown in the report.\n")
	// Frame IDs appear as DATA in audit logs and dashboard snapshots, abutted
	// by whatever text surrounds them, so the regex would report a mangled ID.
	write(".appframes/audit.parts/audit.1.log", "reason: "+mark("security/no-hardcoded-credentialsappframes"))
	write("deploy/demo-static/frames/index.html", "<p>"+mark("made/up-in-a-snapshot")+"</p>\n")

	ctx := engine.CheckContext{ProjectRoot: root}
	known := map[string]bool{
		"security/no-hardcoded-credentials":   true,
		"encoding/no-bom":                     true, // exists but switched off here
		"documentation/doc-touches-with-code": true,
		"security/no-innerHTML-user-input":    true,
	}
	got := UnknownDisableMarkers(ctx, known)

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
	for _, unwanted := range []string{"encoding/no-bom", "node_modules", "docs/notes.md", "audit.parts", "demo-static", "no-innerHTML"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("should not report %q:\n%s", unwanted, joined)
		}
	}
}
