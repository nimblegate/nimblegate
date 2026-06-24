// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunTmpOrphanCleanup_removesOldAfgwDirs(t *testing.T) {
	tmp := t.TempDir()
	old := filepath.Join(tmp, "afgw-old-xyz")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	// Push mtime into the past.
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(tmp, "afgw-fresh-abc")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(tmp, "not-afgw")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(other, past, past); err != nil {
		t.Fatal(err)
	}

	now := func() time.Time { return time.Now() }
	res := runTmpOrphanCleanup(now, tmp)
	if res.Err != nil {
		t.Errorf("unexpected err: %v", res.Err)
	}
	if res.Scanned != 2 {
		t.Errorf("Scanned = %d; want 2 (only afgw-* counted)", res.Scanned)
	}
	if res.Removed != 1 {
		t.Errorf("Removed = %d; want 1", res.Removed)
	}

	// Old dir gone, fresh + other still present.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old afgw dir should be gone; stat err = %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh afgw dir should remain; stat err = %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("non-afgw dir should never be touched; stat err = %v", err)
	}
}

func TestRunTmpOrphanCleanup_missingTmpDirIsNoop(t *testing.T) {
	now := func() time.Time { return time.Now() }
	res := runTmpOrphanCleanup(now, "/path/does/not/exist/anywhere")
	if res.Err != nil {
		t.Errorf("missing dir should be no-op; got err=%v", res.Err)
	}
	if res.Scanned != 0 || res.Removed != 0 {
		t.Errorf("missing dir produced counts: %+v", res)
	}
}
