// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nimblegate/internal/frames"
)

// makePartFile creates a part file with the given content and an mtime
// at `mtime`. Returns the full path.
func makePartFile(t *testing.T, root string, suffix string, content string, mtime time.Time) string {
	t.Helper()
	partsDir := filepath.Join(root, ".appframes", AuditPartsDirName)
	if err := os.MkdirAll(partsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(partsDir, "audit."+suffix+".log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCompactAudit_MergesQuiescentParts(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	p1 := makePartFile(t, root, "1000.111", `{"ts":"2026-05-18T10:00:00Z","frame":"a","result":"PASS"}`+"\n", old)
	p2 := makePartFile(t, root, "2000.222", `{"ts":"2026-05-18T10:01:00Z","frame":"b","result":"BLOCK"}`+"\n", old)
	_ = p1
	_ = p2

	res, err := CompactAudit(root, time.Minute)
	if err != nil {
		t.Fatalf("CompactAudit: %v", err)
	}
	if res.PartsConsumed != 2 {
		t.Errorf("consumed = %d; want 2", res.PartsConsumed)
	}
	if res.BytesAppended == 0 {
		t.Error("expected nonzero BytesAppended")
	}

	// audit.log should now exist with both lines.
	logPath := filepath.Join(root, ".appframes", "audit.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("audit.log not created: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"frame":"a"`) || !strings.Contains(got, `"frame":"b"`) {
		t.Errorf("audit.log missing entries; got: %s", got)
	}
	// Part files should be gone.
	if _, err := os.Stat(p1); err == nil {
		t.Errorf("part %s should have been removed", p1)
	}
	if _, err := os.Stat(p2); err == nil {
		t.Errorf("part %s should have been removed", p2)
	}
}

func TestCompactAudit_SkipsRecentParts(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	p := makePartFile(t, root, "9999.333", `{"ts":"2026-05-18T10:00:00Z","frame":"a","result":"PASS"}`+"\n", now)

	res, err := CompactAudit(root, 1*time.Minute)
	if err != nil {
		t.Fatalf("CompactAudit: %v", err)
	}
	if res.PartsConsumed != 0 {
		t.Errorf("consumed = %d; want 0 (file too recent)", res.PartsConsumed)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("recent part should still exist; got: %v", err)
	}
}

func TestCompactAudit_NoPartsDir(t *testing.T) {
	root := t.TempDir()
	// No audit.parts/ - should be a clean no-op.
	res, err := CompactAudit(root, time.Minute)
	if err != nil {
		t.Errorf("CompactAudit: %v", err)
	}
	if res.PartsConsumed != 0 || res.PartsConsidered != 0 {
		t.Errorf("expected empty result; got %+v", res)
	}
}

func TestCompactAudit_PreservesOrderAcrossParts(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	// p1 starts at nanoseconds 1000; p2 at 2000. Compaction sorts by
	// filename → chronological merge.
	makePartFile(t, root, "1000.aaa", `line-p1-1`+"\n"+`line-p1-2`+"\n", old)
	makePartFile(t, root, "2000.bbb", `line-p2-1`+"\n"+`line-p2-2`+"\n", old)

	if _, err := CompactAudit(root, time.Minute); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(root, ".appframes", "audit.log"))
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	want := []string{"line-p1-1", "line-p1-2", "line-p2-1", "line-p2-2"}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines; want %d. raw: %q", len(lines), len(want), string(data))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("line %d = %q; want %q", i, lines[i], w)
		}
	}
}

func TestCompactAudit_ConcurrentLock(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	makePartFile(t, root, "1000.x", "x\n", old)

	// Manually grab the sentinel lock so the next CompactAudit must skip.
	partsDir := filepath.Join(root, ".appframes", AuditPartsDirName)
	lockPath := filepath.Join(partsDir, ".compact.lock")
	lf, err := acquireCompactLock(lockPath)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer releaseCompactLock(lf, lockPath)

	res, err := CompactAudit(root, time.Minute)
	if err != nil {
		t.Errorf("CompactAudit returned error: %v; want clean skip", err)
	}
	if res.PartsConsumed != 0 {
		t.Errorf("expected 0 consumed (lock held); got %d", res.PartsConsumed)
	}
}

// TestPerProcessWriters_NoContention has two writers (simulating two
// agents) appending to their own part files in parallel. After both
// finish, compaction merges them.
func TestPerProcessWriters_NoContention(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, ".appframes", "audit.log")

	a1, err := OpenAudit(logPath)
	if err != nil {
		t.Fatalf("open writer 1: %v", err)
	}
	// Force a different part path for writer 2 by sleeping enough for
	// time.Now().UnixNano() to differ. 1ns suffices but we sleep 1ms
	// to keep this test stable on every platform.
	time.Sleep(time.Millisecond)
	a2, err := OpenAudit(logPath)
	if err != nil {
		t.Fatalf("open writer 2: %v", err)
	}
	if a1.partPath == a2.partPath {
		t.Fatalf("two writers shouldn't share a part path; got %q for both", a1.partPath)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	for i, a := range []*Audit{a1, a2} {
		go func(i int, a *Audit) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = a.Write(CheckContext{Trigger: TriggerCLI}, CheckResult{
					FrameID:  "test/concurrent",
					Category: frames.CategoryDocumentation,
					Outcome:  OutcomePass,
					Reason:   "from writer",
				})
			}
		}(i, a)
	}
	wg.Wait()
	_ = a1.Close()
	_ = a2.Close()

	// Backdate the part files so they're eligible.
	partsDir := filepath.Join(root, ".appframes", AuditPartsDirName)
	entries, _ := os.ReadDir(partsDir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(partsDir, e.Name())
		_ = os.Chtimes(full, time.Now().Add(-1*time.Hour), time.Now().Add(-1*time.Hour))
	}

	res, err := CompactAudit(root, time.Minute)
	if err != nil {
		t.Fatalf("CompactAudit: %v", err)
	}
	if res.PartsConsumed != 2 {
		t.Errorf("consumed = %d; want 2", res.PartsConsumed)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit.log: %v", err)
	}
	got := strings.Count(string(data), `"frame":"test/concurrent"`)
	if got != 20 {
		t.Errorf("audit.log has %d entries; want 20 (2 writers × 10)", got)
	}
}

func TestRotatedFiles_IncludesPartFiles(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	makePartFile(t, root, "1000.aa", "x\n", old)
	makePartFile(t, root, "2000.bb", "y\n", old)
	logPath := filepath.Join(root, ".appframes", "audit.log")

	got := RotatedFiles(logPath)
	var partsSeen int
	for _, p := range got {
		if strings.Contains(p, AuditPartsDirName) {
			partsSeen++
		}
	}
	if partsSeen != 2 {
		t.Errorf("RotatedFiles returned %d part files; want 2. all: %v", partsSeen, got)
	}
}

func TestCompactAudit_EmptyPartFileSilentlyConsumed(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-1 * time.Hour)
	p := makePartFile(t, root, "1000.empty", "", old)
	res, err := CompactAudit(root, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if res.PartsConsumed != 1 {
		t.Errorf("consumed = %d; want 1 (empty part)", res.PartsConsumed)
	}
	if _, err := os.Stat(p); err == nil {
		t.Errorf("empty part should have been removed")
	}
}
