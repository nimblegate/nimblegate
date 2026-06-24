// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func tableExists(t *testing.T, db *DB, name string) bool {
	t.Helper()
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	return n == 1
}

func TestOpenCreatesSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	for _, tbl := range []string{"schema_meta", "decisions", "findings", "ingest_state"} {
		if !tableExists(t, db, tbl) {
			t.Errorf("missing table %q", tbl)
		}
	}
	var v int
	if err := db.sql.QueryRow(`SELECT version FROM schema_meta`).Scan(&v); err != nil {
		t.Fatalf("schema_meta: %v", err)
	}
	if v != schemaVersion {
		t.Errorf("version = %d, want %d", v, schemaVersion)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	var n int
	if err := db2.sql.QueryRow(`SELECT COUNT(*) FROM schema_meta`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("schema_meta rows = %d, want 1", n)
	}
}

func TestOpenRebuildsOnVersionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analytics.db")
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`CREATE TABLE schema_meta(version INTEGER NOT NULL); INSERT INTO schema_meta(version) VALUES(999999);
		CREATE TABLE decisions(id INTEGER PRIMARY KEY); INSERT INTO decisions DEFAULT VALUES;`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var v, rows int
	if err := db.sql.QueryRow(`SELECT version FROM schema_meta`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Errorf("version = %d, want %d (rebuild)", v, schemaVersion)
	}
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("decisions rows = %d, want 0 (stale data dropped on rebuild)", rows)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("db file missing after rebuild: %v", err)
	}
}
