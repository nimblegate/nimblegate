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

func TestRunTmpOrphanCleanup_coversEveryStagingPrefix(t *testing.T) {
	// The gate, scan-on-first-push and the dashboard preview all stage full
	// trees in the same dir under different prefixes. A sweeper that knew only
	// the gate's prefix would leave the other two to accumulate forever.
	tmp := t.TempDir()
	past := time.Now().Add(-48 * time.Hour)
	for _, name := range []string{"afgw-a", "nimblegate-scan-b", "nimblegate-preview-c"} {
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, past, past); err != nil {
			t.Fatal(err)
		}
	}
	// A neighbour that is not ours, equally old: never touched.
	keep := filepath.Join(tmp, "nimblegate-selection-d")
	if err := os.MkdirAll(keep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(keep, past, past); err != nil {
		t.Fatal(err)
	}

	res := runTmpOrphanCleanup(time.Now, tmp)
	if res.Err != nil {
		t.Fatalf("unexpected err: %v", res.Err)
	}
	if res.Removed != 3 {
		t.Errorf("Removed = %d; want 3 (one per staging prefix)", res.Removed)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("a dir with an unrelated prefix must survive: %v", err)
	}
}
