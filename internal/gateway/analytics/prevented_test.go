// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

// recF builds an audit record from explicit findings (with messages, so
// fingerprints differ by content).
func recF(ts int64, repo string, accept bool, fs ...gateway.Finding) gateway.AuditRecord {
	return gateway.AuditRecord{Time: time.Unix(ts, 0).UTC(), Repo: repo, Refs: []string{"refs/heads/main"}, Accept: accept, Findings: fs}
}

func TestPreventedBreakdownDistinct(t *testing.T) {
	root := t.TempDir()
	key := gateway.Finding{ID: "security/key", Severity: "BLOCK", Message: "a.pem:1"}
	other := gateway.Finding{ID: "security/key", Severity: "BLOCK", Message: "b.pem:2"}
	writeLog(t, root, "repoA",
		recF(100, "repoA", false, key),   // same blocking issue rejected...
		recF(200, "repoA", false, key),   // ...re-pushed (must NOT double-count)
		recF(300, "repoA", false, other), // a different location → distinct
		recF(400, "repoA", true, key),    // observe would-block of the same issue
		recF(500, "repoA", true, gateway.Finding{ID: "security/key", Severity: "WARN", Message: "c:3"})) // excluded
	db := openIngest(t, root)
	defer db.Close()

	bd, err := PreventedBreakdown(db, StatsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(bd) != 1 {
		t.Fatalf("rows = %d, want 1 frame: %+v", len(bd), bd)
	}
	if bd[0].Rejected != 2 || bd[0].Observed != 1 {
		t.Errorf("got %+v, want rejected=2 observed=1", bd[0])
	}
}

func TestPreventedBreakdownWindow(t *testing.T) {
	root := t.TempDir()
	writeLog(t, root, "repoA",
		rec(100, "repoA", false, "BLOCK"),
		rec(900, "repoA", false, "BLOCK"))
	db := openIngest(t, root)
	defer db.Close()

	bd, err := PreventedBreakdown(db, StatsQuery{Since: time.Unix(500, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if len(bd) != 1 || bd[0].Rejected != 1 {
		t.Errorf("windowed breakdown = %+v, want one row rejected=1", bd)
	}
}

// openIngest opens a fresh DB under root and ingests its audit logs.
func openIngest(t *testing.T, root string) *DB {
	t.Helper()
	db, err := Open(root + "/analytics.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}
