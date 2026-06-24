// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package analytics maintains a SQLite index of gateway decisions, derived from
// the JSONL audit logs. The JSONL remains the source of truth; this DB is
// rebuildable. modernc.org/sqlite (pure-Go) is imported only here.
package analytics

import (
	"database/sql"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

const schemaVersion = 3

// DB wraps the analytics SQLite handle. Not safe for concurrent use across
// goroutines beyond what database/sql provides; ingest/stats open it per call.
type DB struct{ sql *sql.DB }

const ddl = `
CREATE TABLE IF NOT EXISTS schema_meta (version INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS decisions (
  id           INTEGER PRIMARY KEY,
  ts           INTEGER NOT NULL,
  repo         TEXT    NOT NULL,
  accept       INTEGER NOT NULL,
  refs         TEXT,
  max_severity TEXT    NOT NULL DEFAULT '',
  dedup        TEXT    NOT NULL UNIQUE
);
CREATE INDEX IF NOT EXISTS ix_dec_repo_ts ON decisions(repo, ts);
CREATE INDEX IF NOT EXISTS ix_dec_ts       ON decisions(ts);
CREATE INDEX IF NOT EXISTS ix_dec_sev      ON decisions(max_severity);
CREATE TABLE IF NOT EXISTS findings (
  id          INTEGER PRIMARY KEY,
  decision_id INTEGER NOT NULL REFERENCES decisions(id),
  frame_id    TEXT    NOT NULL,
  severity    TEXT    NOT NULL,
  message     TEXT    NOT NULL DEFAULT '',
  fingerprint TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS ix_find_frame ON findings(frame_id);
CREATE INDEX IF NOT EXISTS ix_find_dec   ON findings(decision_id);
CREATE INDEX IF NOT EXISTS ix_find_fp    ON findings(fingerprint);
CREATE TABLE IF NOT EXISTS ingest_state (
  source     TEXT PRIMARY KEY,
  offset     INTEGER NOT NULL,
  updated_ts INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS push_tips (
  sha         TEXT PRIMARY KEY,
  decision_id INTEGER NOT NULL REFERENCES decisions(id)
);
CREATE INDEX IF NOT EXISTS ix_pt_dec ON push_tips(decision_id);`

func dsn(path string) string {
	return "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
}

// Open opens (creating if needed) the analytics DB. If the file exists but its
// schema version differs (or is unreadable), it is deleted and rebuilt - safe
// because the DB is derived from the JSONL logs.
func Open(path string) (*DB, error) {
	mismatch, err := schemaMismatch(path)
	if err != nil {
		return nil, err
	}
	if mismatch {
		for _, p := range []string{path, path + "-wal", path + "-shm"} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("analytics: rebuild remove %s: %w", p, err)
			}
		}
	}
	sdb, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	if err := sdb.Ping(); err != nil {
		sdb.Close()
		return nil, err
	}
	if err := applySchema(sdb); err != nil {
		sdb.Close()
		return nil, err
	}
	return &DB{sql: sdb}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

// schemaMismatch reports whether an existing DB has a version != schemaVersion
// (or is missing/unreadable). A non-existent file is NOT a mismatch.
func schemaMismatch(path string) (bool, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	sdb, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return false, err
	}
	defer sdb.Close()
	var v int
	if err := sdb.QueryRow(`SELECT version FROM schema_meta LIMIT 1`).Scan(&v); err != nil {
		return true, nil // table absent / corrupt → rebuild
	}
	return v != schemaVersion, nil
}

func applySchema(sdb *sql.DB) error {
	if _, err := sdb.Exec(ddl); err != nil {
		return fmt.Errorf("analytics: apply schema: %w", err)
	}
	var n int
	if err := sdb.QueryRow(`SELECT COUNT(*) FROM schema_meta`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := sdb.Exec(`INSERT INTO schema_meta(version) VALUES(?)`, schemaVersion); err != nil {
			return err
		}
	}
	return nil
}
