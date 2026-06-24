// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedRun(t *testing.T) *DB {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"time":"2026-05-26T00:00:00Z","repo":"demo","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"security/no-hardcoded-credentials","severity":"BLOCK","message":"config.js:3 token"},{"id":"app-correctness/shellcheck","severity":"WARN","message":"run.sh:1"}]}`,
		`{"time":"2026-05-26T00:05:00Z","repo":"demo","refs":["refs/heads/main"],"accept":true}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestRunPushesChronologicalWithAllFindings(t *testing.T) {
	db := seedRun(t)
	pushes, err := RunPushes(db, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(pushes) != 2 {
		t.Fatalf("want 2 pushes, got %d: %+v", len(pushes), pushes)
	}
	if pushes[0].TS >= pushes[1].TS {
		t.Errorf("pushes not oldest-first: %+v", pushes)
	}
	if len(pushes[0].Findings) != 2 {
		t.Fatalf("first push should have ALL 2 findings, got %d", len(pushes[0].Findings))
	}
	f := pushes[0].Findings[0]
	if f.FrameID == "" || f.Severity == "" || f.Message == "" {
		t.Errorf("finding fields missing: %+v", f)
	}
	if len(pushes[1].Findings) != 0 {
		t.Errorf("second push should be clean, got %d findings", len(pushes[1].Findings))
	}
}

func TestRunPushesUnknownRepoEmpty(t *testing.T) {
	db := seedRun(t)
	pushes, err := RunPushes(db, "nope")
	if err != nil || len(pushes) != 0 {
		t.Fatalf("unknown repo → empty, no error: %v %v", pushes, err)
	}
}
