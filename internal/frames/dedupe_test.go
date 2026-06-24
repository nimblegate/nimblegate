// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadFromDir_DuplicateProjectFramesReported - two .md files in the same
// project declaring the same category/name must surface a load error, even
// though both are individually well-formed.
func TestLoadFromDir_DuplicateProjectFramesReported(t *testing.T) {
	tmp := t.TempDir()
	frame := func(name string) string {
		return `---
name: ` + name + `
category: security
subcategory: credentials
severity: WARN
triggers: [cli]
---
body
`
	}

	if err := os.WriteFile(filepath.Join(tmp, "a.md"), []byte(frame("the-same")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.md"), []byte(frame("the-same")), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 1 {
		t.Errorf("want 1 surviving frame (first wins), got %d", len(loaded))
	}
	if len(errs) != 1 {
		t.Fatalf("want 1 duplicate error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "duplicate frame id") {
		t.Errorf("error doesn't mention duplicate id: %v", errs[0])
	}
	if !strings.Contains(errs[0].Error(), "security/the-same") {
		t.Errorf("error doesn't mention the colliding id: %v", errs[0])
	}
}

// TestLoadFromDir_DuplicatesAcrossSubdirsCaught - frames in different
// category subdirectories that still produce the same category/name ID
// (because category is from frontmatter, not the path).
func TestLoadFromDir_DuplicatesAcrossSubdirsCaught(t *testing.T) {
	tmp := t.TempDir()
	subA := filepath.Join(tmp, "subA")
	subB := filepath.Join(tmp, "subB")
	for _, dir := range []string{subA, subB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := `---
name: same-name
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
---
body
`
	if err := os.WriteFile(filepath.Join(subA, "x.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subB, "y.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 1 {
		t.Errorf("want 1 surviving frame, got %d", len(loaded))
	}
	if len(errs) != 1 {
		t.Errorf("want 1 duplicate error across subdirs, got %d", len(errs))
	}
}

// TestLoadFromDir_ThreeDuplicatesProduceTwoErrors - N copies of the same
// frame ID produce one winner and N-1 errors, each pointing at the
// surviving file by name.
func TestLoadFromDir_ThreeDuplicatesProduceTwoErrors(t *testing.T) {
	tmp := t.TempDir()
	body := `---
name: triple
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
---
body
`
	// Names chosen so alphabetical walk order is a.md, b.md, c.md → a wins.
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 1 {
		t.Errorf("want 1 surviving frame from 3 duplicates, got %d", len(loaded))
	}
	if len(errs) != 2 {
		t.Errorf("want 2 errors (one per loser), got %d: %v", len(errs), errs)
	}
	// Confirm a.md is the winner.
	if !strings.HasSuffix(loaded[0].SourcePath, "a.md") {
		t.Errorf("expected a.md to win (alphabetical first); winner = %s", loaded[0].SourcePath)
	}
	// Both errors must name a.md as the winner.
	for _, e := range errs {
		if !strings.Contains(e.Error(), "a.md") {
			t.Errorf("error %q doesn't reference the winning file a.md", e)
		}
	}
}

// TestLoadFromDir_FiveDuplicatesProduceFourErrors - make sure the algorithm
// stays linear and doesn't compare N×N.
func TestLoadFromDir_FiveDuplicatesProduceFourErrors(t *testing.T) {
	tmp := t.TempDir()
	body := `---
name: many
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
---
body
`
	for _, name := range []string{"01.md", "02.md", "03.md", "04.md", "05.md"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 1 {
		t.Errorf("want 1 surviving frame from 5 duplicates, got %d", len(loaded))
	}
	if len(errs) != 4 {
		t.Errorf("want 4 errors, got %d", len(errs))
	}
}

// TestLoadFromDir_NoDuplicatesNoError - sanity: no spurious duplicate
// reports when frames have distinct IDs.
func TestLoadFromDir_NoDuplicatesNoError(t *testing.T) {
	tmp := t.TempDir()
	frame := func(name string) string {
		return `---
name: ` + name + `
category: security
subcategory: credentials
severity: INFO
triggers: [cli]
---
body
`
	}
	if err := os.WriteFile(filepath.Join(tmp, "a.md"), []byte(frame("one")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.md"), []byte(frame("two")), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 2 {
		t.Errorf("want 2 frames, got %d", len(loaded))
	}
	if len(errs) != 0 {
		t.Errorf("want no errors, got: %v", errs)
	}
}
