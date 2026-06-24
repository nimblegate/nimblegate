// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestScaleStatsLatency asserts queries stay fast with a large history.
func TestScaleStatsLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scale test in -short")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "repoA")
	os.MkdirAll(dir, 0o755)
	f, err := os.Create(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	const n = 50000
	for i := 0; i < n; i++ {
		accept := "true"
		fnd := ""
		if i%7 == 0 {
			accept = "false"
			fnd = `,"findings":[{"id":"security/no-private-keys-in-repo","severity":"BLOCK"}]`
		}
		fmt.Fprintf(f, `{"time":"%s","repo":"repoA","refs":["refs/heads/main"],"accept":%s%s}`+"\n",
			time.Unix(int64(1_700_000_000+i), 0).UTC().Format(time.RFC3339), accept, fnd)
	}
	f.Close()

	db, err := Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, db, "decisions"); got != n {
		t.Fatalf("decisions = %d, want %d", got, n)
	}

	start := time.Now()
	s, err := Stats(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Stats over %d rows took %v, want < 2s", n, elapsed)
	}
	if s.Decisions != n {
		t.Errorf("decisions = %d, want %d", s.Decisions, n)
	}
}
