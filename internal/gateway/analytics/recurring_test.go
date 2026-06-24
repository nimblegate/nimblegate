// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"testing"

	"nimblegate/internal/gateway"
)

func TestRecurringCollapsesAndCounts(t *testing.T) {
	root := t.TempDir()
	key := gateway.Finding{ID: "security/key", Severity: "BLOCK", Message: "a.pem:1"}
	writeLog(t, root, "repoA",
		recF(100, "repoA", false, key),
		recF(200, "repoA", false, key),
		recF(300, "repoA", false, key))
	db := openIngest(t, root)
	defer db.Close()

	rf, err := Recurring(db, StatsQuery{Repo: "repoA"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rf) != 1 {
		t.Fatalf("rows = %d, want 1 deduped finding: %+v", len(rf), rf)
	}
	if rf[0].Seen != 3 || rf[0].FirstSeen != 100 || rf[0].LastSeen != 300 {
		t.Errorf("got %+v, want seen=3 first=100 last=300", rf[0])
	}
}
