// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"path/filepath"
	"testing"
	"time"
)

func seed(t *testing.T) *DB {
	t.Helper()
	root := t.TempDir()
	writeLog(t, root, "repoA",
		rec(100, "repoA", true),
		rec(200, "repoA", false, "BLOCK"),
		rec(300, "repoA", false, "WARN", "ERROR"))
	writeLog(t, root, "repoB",
		rec(150, "repoB", true, "WARN"))
	db, err := Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStatsTotals(t *testing.T) {
	db := seed(t)
	defer db.Close()
	s, err := Stats(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Decisions != 4 || s.Accepts != 2 || s.Rejects != 2 {
		t.Errorf("totals: decisions=%d accepts=%d rejects=%d, want 4/2/2", s.Decisions, s.Accepts, s.Rejects)
	}
	if s.Repos != 2 {
		t.Errorf("repos = %d, want 2", s.Repos)
	}
}

func TestStatsFilterByRepo(t *testing.T) {
	db := seed(t)
	defer db.Close()
	s, err := Stats(db, StatsQuery{Repo: "repoA"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Decisions != 3 || s.Rejects != 2 {
		t.Errorf("repoA: decisions=%d rejects=%d, want 3/2", s.Decisions, s.Rejects)
	}
}

func TestStatsConsistencyCheck(t *testing.T) {
	db := seed(t)
	defer db.Close()
	s, err := Stats(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Consistent {
		t.Errorf("seeded data should be consistent: %d decisions vs %d+%d", s.Decisions, s.Accepts, s.Rejects)
	}
	// Inject a glitch row (accept value not 0/1): counted in decisions but in
	// neither accepts nor rejects → the sanity check must trip.
	if _, err := db.sql.Exec(`INSERT INTO decisions(ts,repo,accept,refs,max_severity,dedup) VALUES(1,'x',2,'[]','','glitch')`); err != nil {
		t.Fatal(err)
	}
	bad, err := Stats(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if bad.Consistent {
		t.Errorf("a stray accept value should trip the check: %d decisions vs %d+%d", bad.Decisions, bad.Accepts, bad.Rejects)
	}
}

func TestStatsTimeWindow(t *testing.T) {
	db := seed(t)
	defer db.Close()
	s, err := Stats(db, StatsQuery{Since: time.Unix(150, 0), Until: time.Unix(250, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if s.Decisions != 2 {
		t.Errorf("windowed decisions = %d, want 2", s.Decisions)
	}
}

func TestStatsTopFrames(t *testing.T) {
	db := seed(t)
	defer db.Close()
	s, err := Stats(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.TopFrames) == 0 {
		t.Fatal("no top frames")
	}
	total := 0
	for _, f := range s.TopFrames {
		total += f.Count
	}
	if total != 4 {
		t.Errorf("total finding count across frames = %d, want 4", total)
	}
}

func TestStatsConcurrentReadDuringWrite(t *testing.T) {
	db := seed(t)
	defer db.Close()
	tx, err := db.sql.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO decisions(ts,repo,accept,refs,max_severity,dedup) VALUES(999,'repoC',1,'[]','','zz')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Stats(db, StatsQuery{}); err != nil {
		t.Errorf("read during open write tx failed (WAL should allow it): %v", err)
	}
}
