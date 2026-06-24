// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"nimblegate/internal/gateway"
)

// IngestResult reports a pass's outcome.
type IngestResult struct {
	Inserted int // new decision rows
	Skipped  int // malformed JSONL lines
}

// Ingest scans every <policyRoot>/*/audit.log and indexes new decisions into
// the DB. Incremental (per-file byte offset) + idempotent (sha256 dedup), with
// a shrink-guard for rotated/truncated logs. Read-only w.r.t. the JSONL.
func Ingest(d *DB, policyRoot string) (IngestResult, error) {
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "audit.log"))
	sort.Strings(matches)
	var res IngestResult
	for _, path := range matches {
		fr, err := ingestFile(d, path)
		if err != nil {
			return res, err
		}
		res.Inserted += fr.Inserted
		res.Skipped += fr.Skipped
	}
	return res, nil
}

func ingestFile(d *DB, path string) (IngestResult, error) {
	var res IngestResult
	offset, err := loadOffset(d, path)
	if err != nil {
		return res, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return res, nil // vanished → fail-soft
	}
	if fi.Size() < offset {
		offset = 0 // rotation/truncation guard
	}
	f, err := os.Open(path)
	if err != nil {
		return res, nil
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return res, err
	}

	tx, err := d.sql.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	reader := bufio.NewReader(f)
	pos := offset
	for {
		line, rerr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			// Complete, newline-terminated line: process and advance offset.
			if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
				ins, parseErr, dbErr := ingestLine(tx, trimmed)
				switch {
				case dbErr != nil:
					return res, dbErr // hard fail; defer'd Rollback discards the file's tx, offset not advanced
				case parseErr != nil:
					res.Skipped++
				case ins:
					res.Inserted++
				}
			}
			pos += int64(len(line))
		}
		// A partial trailing line (no newline yet - writer mid-append) is left
		// unconsumed; offset is not advanced past it, so it's re-read next pass.
		if rerr != nil {
			break
		}
	}

	if err := saveOffset(tx, path, pos); err != nil {
		return res, err
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// ingestLine parses one JSONL record and inserts it.
//
//	parseErr != nil → malformed line (caller counts as Skipped, keeps going)
//	dbErr   != nil  → database failure (caller must abort; never silently skipped)
//	inserted=false, both nil → duplicate (ignored)
func ingestLine(tx *sql.Tx, line []byte) (inserted bool, parseErr, dbErr error) {
	var r gateway.AuditRecord
	if err := json.Unmarshal(line, &r); err != nil {
		return false, err, nil
	}
	refsJSON, _ := json.Marshal(r.Refs)
	res, err := tx.Exec(
		`INSERT OR IGNORE INTO decisions(ts,repo,accept,refs,max_severity,dedup) VALUES(?,?,?,?,?,?)`,
		r.Time.Unix(), r.Repo, b2i(r.Accept), string(refsJSON), maxSeverity(r.Findings), dedupHash(line))
	if err != nil {
		return false, nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil, nil // dup → ignored; do not insert findings
	}
	id, err := res.LastInsertId()
	if err != nil {
		return false, nil, err
	}
	for _, fnd := range r.Findings {
		if _, err := tx.Exec(`INSERT INTO findings(decision_id,frame_id,severity,message,fingerprint) VALUES(?,?,?,?,?)`,
			id, fnd.ID, fnd.Severity, fnd.Message, fingerprint(fnd.ID, fnd.Message)); err != nil {
			return false, nil, err
		}
	}
	for _, ru := range r.RefUpdates {
		if ru.NewRev == "" || ru.IsDelete() {
			continue // branch deletion - no tip
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO push_tips(sha, decision_id) VALUES(?,?)`,
			ru.NewRev, id); err != nil {
			return false, nil, err
		}
	}
	return true, nil, nil
}

func loadOffset(d *DB, source string) (int64, error) {
	var off int64
	err := d.sql.QueryRow(`SELECT offset FROM ingest_state WHERE source=?`, source).Scan(&off)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return off, err
}

func saveOffset(tx *sql.Tx, source string, off int64) error {
	_, err := tx.Exec(
		`INSERT INTO ingest_state(source,offset,updated_ts) VALUES(?,?,?)
		 ON CONFLICT(source) DO UPDATE SET offset=excluded.offset, updated_ts=excluded.updated_ts`,
		source, off, time.Now().Unix())
	return err
}
