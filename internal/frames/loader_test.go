// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromDir_RecursivelyReadsMarkdownFrames(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "security")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `---
name: test-frame
category: security
subcategory: credentials
severity: BLOCK
triggers: [cli]
---

# Test frame body
`
	if err := os.WriteFile(filepath.Join(dir, "test-frame.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, errs := LoadFromDir(tmp)
	if len(errs) != 0 {
		t.Fatalf("LoadFromDir() errors: %v", errs)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(loaded))
	}
	if loaded[0].ID() != "security/test-frame" {
		t.Errorf("ID = %q, want %q", loaded[0].ID(), "security/test-frame")
	}
}

func TestLoadFromDir_SkipsNonMarkdownFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "notes.txt"), []byte("ignore me"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, errs := LoadFromDir(tmp)
	if len(errs) != 0 {
		t.Fatalf("LoadFromDir() errors: %v", errs)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 frames, got %d", len(loaded))
	}
}

func TestLoadFromDir_SkipsCanonicalSubdir(t *testing.T) {
	tmp := t.TempDir()
	canon := filepath.Join(tmp, "_canonical")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canon, "ignored.md"), []byte("not a frame"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, errs := LoadFromDir(tmp)
	if len(errs) != 0 {
		t.Fatalf("LoadFromDir() errors: %v", errs)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 frames, got %d", len(loaded))
	}
}

func TestLoadFromDir_MissingDirReturnsEmpty(t *testing.T) {
	loaded, errs := LoadFromDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(errs) != 0 {
		t.Fatalf("LoadFromDir() errors on missing dir: %v", errs)
	}
	if len(loaded) != 0 {
		t.Errorf("expected 0 frames, got %d", len(loaded))
	}
}

// TestLoadFromDir_PartialLoad - one bad frame must not drop the good ones.
// This is the V0.5 behaviour change: previously LoadFromDir aborted the entire
// walk on first parse error.
func TestLoadFromDir_PartialLoad(t *testing.T) {
	tmp := t.TempDir()
	good := `---
name: good
category: security
subcategory: credentials
severity: INFO
triggers: [cli]
---
ok
`
	bad := `---
name: bad
severity: INFO
triggers: [cli]
---
missing category
`
	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "bad.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, errs := LoadFromDir(tmp)
	if len(loaded) != 1 {
		t.Errorf("expected 1 valid frame loaded despite the bad one, got %d", len(loaded))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error reported, got %d: %v", len(errs), errs)
	}
}
