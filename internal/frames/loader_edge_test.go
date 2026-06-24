// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestLoadFromDir_OneInvalidFrameNoLongerAborts - V0.5: one bad frame is
// reported as an error but the rest of the walk continues. Previously this
// test asserted the opposite ("fails loudly"); the new behaviour gives users
// partial-load with explicit error reporting.
func TestLoadFromDir_OneInvalidFrameNoLongerAborts(t *testing.T) {
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
		t.Errorf("want 1 frame surviving, got %d", len(loaded))
	}
	if len(errs) != 1 {
		t.Errorf("want 1 error reported, got %d: %v", len(errs), errs)
	}
}

// TestLoadFromDir_SymlinksRefused - V0.5 security: any symlinked .md is
// reported as an error and never followed. Valid neighbors still load.
func TestLoadFromDir_SymlinksRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows")
	}
	tmp := t.TempDir()
	good := `---
name: g
category: security
subcategory: credentials
severity: INFO
triggers: [cli]
---
ok
`
	target := filepath.Join(tmp, "real-target.md")
	if err := os.WriteFile(target, []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte(good), 0o644); err != nil {
		// good.md and real-target.md both declare the same ID 'security/g' -
		// avoid by giving the real target a unique name and not also having
		// good.md. Simplify:
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(tmp, "good.md")); err != nil {
		t.Fatal(err)
	}
	// Symlink to a valid real file. Even though the target parses, the
	// loader must refuse to follow.
	if err := os.Symlink(target, filepath.Join(tmp, "linked.md")); err != nil {
		t.Fatal(err)
	}

	got, errs := LoadFromDir(tmp)
	// real-target.md should load (it's a real file).
	if len(got) != 1 {
		t.Errorf("want 1 frame from the real file, got %d", len(got))
	}
	// linked.md should produce an error.
	if len(errs) == 0 {
		t.Error("want an error for the symlinked frame, got none")
	}
	var sawSymlinkErr bool
	for _, e := range errs {
		if strings.Contains(e.Error(), "symlink frames are not followed") {
			sawSymlinkErr = true
		}
	}
	if !sawSymlinkErr {
		t.Errorf("expected symlink-refused error; got: %v", errs)
	}
}

// TestLoadFromDir_BrokenSymlinkRefusedNotCrash - a dangling symlink also
// produces an error, doesn't try to read the (nonexistent) target.
func TestLoadFromDir_BrokenSymlinkRefusedNotCrash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows")
	}
	tmp := t.TempDir()
	good := `---
name: g
category: security
subcategory: credentials
severity: INFO
triggers: [cli]
---
ok
`
	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nonexistent/path/dangling.md", filepath.Join(tmp, "broken.md")); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadFromDir(tmp)
	if len(got) != 1 {
		t.Errorf("want 1 good frame loaded, got %d", len(got))
	}
	if len(errs) == 0 {
		t.Errorf("want at least 1 error reported for broken symlink, got none")
	}
}

// TestLoadFromDir_SymlinkCycleDoesNotHang - a directory symlink loop must
// not cause infinite walk. filepath.WalkDir uses lstat and does NOT follow
// directory symlinks by default, so this should pass quickly.
func TestLoadFromDir_SymlinkCycleDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require admin on Windows")
	}
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(tmp, filepath.Join(sub, "back")); err != nil {
		t.Fatal(err)
	}
	good := `---
name: g
category: security
subcategory: credentials
severity: INFO
triggers: [cli]
---
ok
`
	if err := os.WriteFile(filepath.Join(tmp, "good.md"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var got []Frame
	var loadErrs []error
	go func() {
		got, loadErrs = LoadFromDir(tmp)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("LoadFromDir hung on symlink cycle")
	}
	if len(got) < 1 {
		t.Errorf("expected at least the good frame, got %d (errors: %v)", len(got), loadErrs)
	}
}

// TestLoadFromDir_UnreadableFile - frame file the process can't read.
// V0.5: this is now reported as an error entry, and the rest of the walk
// continues. Skipped when running as root (perm 000 is no obstacle to root).
func TestLoadFromDir_UnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("perm-000 has no effect for root")
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secret.md")
	if err := os.WriteFile(path, []byte("---\nname: x\ncategory: security\nseverity: INFO\ntriggers: [cli]\n---\nbody\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)

	_, errs := LoadFromDir(tmp)
	if len(errs) == 0 {
		t.Fatal("expected at least one error for permission-denied frame")
	}
}

// TestLoadFromDir_VeryDeepNesting - frame at 50 levels deep must still load.
func TestLoadFromDir_VeryDeepNesting(t *testing.T) {
	tmp := t.TempDir()
	dir := tmp
	for i := 0; i < 50; i++ {
		dir = filepath.Join(dir, "lvl")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	content := "---\nname: deep\ncategory: security\nsubcategory: credentials\nseverity: INFO\ntriggers: [cli]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "deep.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadFromDir(tmp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors at 50 levels deep: %v", errs)
	}
	if len(got) != 1 {
		t.Errorf("got %d frames, want 1 at 50 levels deep", len(got))
	}
}

// TestLoadFromDir_NonFrameDotMdReportsError - README/notes.md without
// frontmatter now appears as one entry in errs (rest of project unaffected).
func TestLoadFromDir_NonFrameDotMdReportsError(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "notes.md"), []byte("# Just a doc, not a frame\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadFromDir(tmp)
	if len(got) != 0 {
		t.Errorf("expected 0 frames, got %d", len(got))
	}
	if len(errs) != 1 {
		t.Errorf("expected 1 error reported for non-frame .md, got %d: %v", len(errs), errs)
	}
}

// TestLoadFromDir_HiddenDotPrefixedFileSkipped - V0.5: `.draft.md` and other
// dotfiles are now skipped (previously loaded).
func TestLoadFromDir_HiddenDotPrefixedFileSkipped(t *testing.T) {
	tmp := t.TempDir()
	content := "---\nname: hidden\ncategory: security\nseverity: INFO\ntriggers: [cli]\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(tmp, ".hidden.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, errs := LoadFromDir(tmp)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(got) != 0 {
		t.Errorf("dotfile frame should be skipped; got %d frames", len(got))
	}
}
