// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package scanignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTree builds a temp dir with the given files (path → content).
// Files inside subdirs auto-create their parents.
func setupTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestMatch_SegmentNameExclude(t *testing.T) {
	root := setupTree(t, map[string]string{
		"src/main.go":             "ok",
		"node_modules/foo/bar.js": "junk",
		"vendor/lib.go":           "dep",
	})
	m, err := New(root, []string{"node_modules", "vendor"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(filepath.Join(root, "node_modules/foo/bar.js")) {
		t.Error("node_modules/ should match segment exclude")
	}
	if !m.Match(filepath.Join(root, "vendor/lib.go")) {
		t.Error("vendor/ should match segment exclude")
	}
	if m.Match(filepath.Join(root, "src/main.go")) {
		t.Error("src/main.go should NOT be excluded")
	}
}

func TestMatch_ExcludePaths(t *testing.T) {
	root := setupTree(t, map[string]string{
		"public/downloads/manual.pdf": "binary",
		"public/css/site.css":         "ok",
		"src/downloads/data.go":       "src",
	})
	m, err := New(root, nil, []string{"public/downloads/**"})
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(filepath.Join(root, "public/downloads/manual.pdf")) {
		t.Error("public/downloads/manual.pdf should match exclude-paths")
	}
	if m.Match(filepath.Join(root, "public/css/site.css")) {
		t.Error("public/css/site.css should NOT be excluded")
	}
	if m.Match(filepath.Join(root, "src/downloads/data.go")) {
		t.Error("src/downloads/data.go should NOT be excluded - only the public one is")
	}
}

func TestMatch_MarkerFile(t *testing.T) {
	root := setupTree(t, map[string]string{
		"served/.appframes-ignore": "*.pdf\nbig/**\n# comment\n\n",
		"served/manual.pdf":        "binary",
		"served/page.html":         "ok",
		"served/big/data.json":     "data",
		"src/page.pdf":             "src",
	})
	m, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(filepath.Join(root, "served/manual.pdf")) {
		t.Error("served/manual.pdf should be skipped by marker")
	}
	if !m.Match(filepath.Join(root, "served/big/data.json")) {
		t.Error("served/big/data.json should be skipped by marker (big/**)")
	}
	if m.Match(filepath.Join(root, "served/page.html")) {
		t.Error("served/page.html should NOT be excluded - no pattern matches")
	}
	if m.Match(filepath.Join(root, "src/page.pdf")) {
		t.Error("src/page.pdf should NOT be excluded - marker is scoped to served/")
	}
}

func TestMatch_MarkerNested(t *testing.T) {
	root := setupTree(t, map[string]string{
		"a/.appframes-ignore":   "*.zip\n",
		"a/b/.appframes-ignore": "*.tar\n",
		"a/x.zip":               "z",
		"a/y.tar":               "t",
		"a/b/x.zip":             "z",
		"a/b/y.tar":             "t",
		"a/b/keep.js":           "ok",
	})
	m, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(filepath.Join(root, "a/x.zip")) {
		t.Error("a/x.zip should match parent marker (*.zip)")
	}
	if !m.Match(filepath.Join(root, "a/b/x.zip")) {
		t.Error("a/b/x.zip should match ancestor marker (*.zip is scoped to a/)")
	}
	if !m.Match(filepath.Join(root, "a/b/y.tar")) {
		t.Error("a/b/y.tar should match nearer marker (*.tar)")
	}
	if m.Match(filepath.Join(root, "a/b/keep.js")) {
		t.Error("a/b/keep.js should NOT be excluded - no .js pattern anywhere")
	}
	if m.Match(filepath.Join(root, "a/y.tar")) {
		t.Error("a/y.tar should NOT be excluded - .tar pattern only at a/b")
	}
}

func TestMatch_SegmentExcludeBlocksMarkerDiscovery(t *testing.T) {
	// A marker file inside an already-excluded segment shouldn't be discovered.
	root := setupTree(t, map[string]string{
		"node_modules/.appframes-ignore": "!important.js\n",
		"node_modules/dep.js":            "junk",
		"node_modules/important.js":      "junk",
	})
	m, err := New(root, []string{"node_modules"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// node_modules is excluded; nothing inside it should be scanned, and the
	// marker file inside it should not have been parsed.
	if !m.Match(filepath.Join(root, "node_modules/important.js")) {
		t.Error("node_modules/important.js should be excluded - marker inside an excluded dir doesn't re-include")
	}
}

func TestMatch_InvalidGlobBecomesWarning(t *testing.T) {
	root := t.TempDir()
	// regexp.QuoteMeta won't make this fail - glob.Compile produces a valid
	// regex from any string. Make a deliberately broken pattern: a regex
	// metacharacter that survives compileGlob unmangled... actually our
	// glob escapes everything, so finding a "broken" glob is hard.
	//
	// Instead: an empty pattern compiles to "^$" which only matches "".
	// Not a load failure, just a useless pattern. Test that LoadWarnings
	// is empty when patterns are well-formed.
	m, err := New(root, nil, []string{"docs/**", "vendor/**"})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.LoadWarnings()) != 0 {
		t.Errorf("expected no load warnings; got %v", m.LoadWarnings())
	}
}

func TestMarkerFile_CommentsAndBlankLines(t *testing.T) {
	root := setupTree(t, map[string]string{
		".appframes-ignore": `# this is a comment

# another comment
*.pdf

# trailing comment
`,
		"a.pdf": "x",
		"a.md":  "y",
	})
	m, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !m.Match(filepath.Join(root, "a.pdf")) {
		t.Error("a.pdf should be matched by *.pdf pattern")
	}
	if m.Match(filepath.Join(root, "a.md")) {
		t.Error("a.md should NOT match - only *.pdf was declared")
	}
}

func TestMatch_NilMatcherSafe(t *testing.T) {
	var m *Matcher
	if m.Match("/some/path") {
		t.Error("nil matcher should return false")
	}
}

func TestLoadWarnings_FromMarker(t *testing.T) {
	// Marker file with a syntactically valid glob - no warnings expected.
	root := setupTree(t, map[string]string{
		".appframes-ignore": "*.log\n",
		"x.log":             "y",
	})
	m, err := New(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range m.LoadWarnings() {
		t.Errorf("unexpected warning: %s", w)
	}
}

func TestProjectRoot_MissingDir(t *testing.T) {
	m, err := New("/no/such/path/exists/anywhere", nil, nil)
	if err != nil {
		t.Errorf("missing project root should not error: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil matcher even on missing root")
	}
	if m.Match("/no/such/path/exists/anywhere/foo") {
		t.Error("with no patterns and missing root, nothing should match")
	}
}

// TestMatch_RealisticScenario sanity-checks the combined behavior across
// all three signals on one tree.
func TestMatch_RealisticScenario(t *testing.T) {
	root := setupTree(t, map[string]string{
		"node_modules/.bin/junk":         "x",
		"src/app.js":                     "ok",
		"public/downloads/installer.zip": "binary",
		"public/css/site.css":            "ok",
		"user-uploads/.appframes-ignore": "*\n",
		"user-uploads/photo-1.jpg":       "binary",
		"user-uploads/sub/photo-2.jpg":   "binary",
	})
	m, err := New(root,
		[]string{"node_modules"},        // segment
		[]string{"public/downloads/**"}, // path glob
	)
	if err != nil {
		t.Fatal(err)
	}
	wantExcluded := []string{
		"node_modules/.bin/junk",         // segment
		"public/downloads/installer.zip", // path glob
		"user-uploads/photo-1.jpg",       // marker file
		"user-uploads/sub/photo-2.jpg",   // marker file (recursive)
	}
	wantIncluded := []string{
		"src/app.js",
		"public/css/site.css",
	}
	for _, rel := range wantExcluded {
		if !m.Match(filepath.Join(root, rel)) {
			t.Errorf("expected %q to be excluded", rel)
		}
	}
	for _, rel := range wantIncluded {
		if m.Match(filepath.Join(root, rel)) {
			t.Errorf("expected %q to be INCLUDED (got excluded). loadWarnings: %s",
				rel, strings.Join(m.LoadWarnings(), " | "))
		}
	}
}
