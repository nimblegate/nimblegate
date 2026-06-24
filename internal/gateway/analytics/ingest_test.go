// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

// writeLog appends JSONL AuditRecords to <root>/<repo>/audit.log.
func writeLog(t *testing.T, root, repo string, recs ...gateway.AuditRecord) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "audit.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func rec(ts int64, repo string, accept bool, sevs ...string) gateway.AuditRecord {
	var fs []gateway.Finding
	for i, s := range sevs {
		fs = append(fs, gateway.Finding{ID: fmt.Sprintf("frame/%d", i), Severity: s})
	}
	return gateway.AuditRecord{Time: time.Unix(ts, 0).UTC(), Repo: repo, Refs: []string{"refs/heads/main"}, Accept: accept, Findings: fs}
}

func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.sql.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIngestBackfillAndIdempotent(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA", rec(100, "repoA", true), rec(200, "repoA", false, "BLOCK"))
	writeLog(t, root, "repoB", rec(150, "repoB", true, "WARN"))
	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()

	r1, err := Ingest(db, root)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if r1.Inserted != 3 {
		t.Errorf("first ingest inserted = %d, want 3", r1.Inserted)
	}
	if got := countRows(t, db, "decisions"); got != 3 {
		t.Errorf("decisions = %d, want 3", got)
	}
	if got := countRows(t, db, "findings"); got != 2 {
		t.Errorf("findings = %d, want 2", got)
	}

	r2, err := Ingest(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Inserted != 0 {
		t.Errorf("second ingest inserted = %d, want 0 (idempotent)", r2.Inserted)
	}
	if got := countRows(t, db, "decisions"); got != 3 {
		t.Errorf("decisions after re-ingest = %d, want 3", got)
	}
	if got := countRows(t, db, "findings"); got != 2 {
		t.Errorf("findings after re-ingest = %d, want 2 (no dup findings)", got)
	}
}

func TestIngestIncremental(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA", rec(100, "repoA", true))
	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	writeLog(t, root, "repoA", rec(200, "repoA", false, "BLOCK"), rec(300, "repoA", true))
	r, err := Ingest(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 2 {
		t.Errorf("incremental inserted = %d, want 2", r.Inserted)
	}
	if got := countRows(t, db, "decisions"); got != 3 {
		t.Errorf("decisions = %d, want 3", got)
	}
}

func TestIngestRotationGuard(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA", rec(100, "repoA", true), rec(200, "repoA", true))
	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "repoA", "audit.log")); err != nil {
		t.Fatal(err)
	}
	writeLog(t, root, "repoA", rec(300, "repoA", false, "ERROR"))
	r, err := Ingest(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inserted != 1 {
		t.Errorf("after rotation inserted = %d, want 1 (re-scan from 0, dedup ignores old)", r.Inserted)
	}
	if got := countRows(t, db, "decisions"); got != 3 {
		t.Errorf("decisions = %d, want 3 (2 original + 1 new, no dups)", got)
	}
}

func TestIngestSkipsMalformed(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repoA")
	os.MkdirAll(dir, 0o755)
	good, _ := json.Marshal(rec(100, "repoA", true))
	content := append(good, '\n')
	content = append(content, []byte("this is not json\n")...)
	good2, _ := json.Marshal(rec(200, "repoA", true))
	content = append(content, append(good2, '\n')...)
	os.WriteFile(filepath.Join(dir, "audit.log"), content, 0o644)

	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()
	r, err := Ingest(db, root)
	if err != nil {
		t.Fatalf("ingest must not fail on a bad line: %v", err)
	}
	if r.Inserted != 2 || r.Skipped != 1 {
		t.Errorf("inserted=%d skipped=%d, want 2 and 1", r.Inserted, r.Skipped)
	}
}

func TestIngestDBErrorPropagates(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA", rec(100, "repoA", false, "BLOCK"))
	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()
	// Force the findings INSERT to fail so ingest hits a DB error mid-record.
	if _, err := db.sql.Exec(`DROP TABLE findings`); err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(db, root); err == nil {
		t.Error("expected a hard error when the findings insert fails, got nil (DB error must not be swallowed as Skipped)")
	}
}

// TestIngestPartialTrailingLine pins the concurrent-append safety: a line still
// being written (no trailing newline yet) must NOT be consumed - the offset
// stops before it, it counts as neither inserted nor skipped, and once the
// writer finishes the line it ingests cleanly with no duplication.
func TestIngestPartialTrailingLine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "repoA")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "audit.log")

	complete, _ := json.Marshal(rec(100, "repoA", true))
	partial, _ := json.Marshal(rec(200, "repoA", false, "BLOCK"))
	// One complete (newline-terminated) record followed by a partial line with
	// NO trailing newline - i.e. a writer caught mid-append.
	content := append(append([]byte{}, complete...), '\n')
	content = append(content, partial...)
	if err := os.WriteFile(logPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	db, _ := Open(filepath.Join(root, "analytics.db"))
	defer db.Close()

	r1, err := Ingest(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Inserted != 1 {
		t.Errorf("first ingest inserted = %d, want 1 (partial line deferred)", r1.Inserted)
	}
	if r1.Skipped != 0 {
		t.Errorf("first ingest skipped = %d, want 0 (a partial line is not malformed)", r1.Skipped)
	}
	if got := countRows(t, db, "decisions"); got != 1 {
		t.Errorf("decisions after pass 1 = %d, want 1", got)
	}

	// The writer finishes the line (just the newline arrives).
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	f.Close()

	r2, err := Ingest(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Inserted != 1 {
		t.Errorf("second ingest inserted = %d, want 1 (now-complete line)", r2.Inserted)
	}
	if got := countRows(t, db, "decisions"); got != 2 {
		t.Errorf("decisions after pass 2 = %d, want 2 (partial completed, no dup)", got)
	}
	if got := countRows(t, db, "findings"); got != 1 {
		t.Errorf("findings = %d, want 1 (BLOCK on the completed record)", got)
	}
}

func TestIngestPersistsFingerprint(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA",
		gateway.AuditRecord{Time: time.Unix(100, 0).UTC(), Repo: "repoA", Refs: []string{"refs/heads/main"}, Accept: false,
			Findings: []gateway.Finding{{ID: "security/x", Severity: "BLOCK", Message: "a.pem:1"}}})
	db := openIngest(t, root)
	defer db.Close()

	var fp, msg string
	if err := db.sql.QueryRow(`SELECT fingerprint, message FROM findings LIMIT 1`).Scan(&fp, &msg); err != nil {
		t.Fatal(err)
	}
	if msg != "a.pem:1" {
		t.Errorf("message = %q, want a.pem:1", msg)
	}
	if fp != fingerprint("security/x", "a.pem:1") {
		t.Errorf("stored fingerprint %q != recomputed", fp)
	}
}
