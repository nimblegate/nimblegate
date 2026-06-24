// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/analytics"
)

func TestTimePreventedDistinctSingleRepo(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo" // tier-1, default 4.0h
	for _, ts := range []string{"00:00:00", "00:01:00", "00:02:00"} {
		writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T`+ts+`Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	}
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:03:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	db, err := analytics.Open(analyticsDBPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := analytics.Ingest(db, root); err != nil {
		t.Fatal(err)
	}

	actual, modeled, rows := timePrevented(db, root, "repo-a", time.Time{})
	if actual != 4.0 || modeled != 4.0 {
		t.Errorf("actual=%v modeled=%v, want 4.0/4.0 (distinct, not inflated by re-push)", actual, modeled)
	}
	if len(rows) != 1 || rows[0].Rejected != 1 || rows[0].Observed != 1 || rows[0].HoursPerHit != 4.0 {
		t.Errorf("rows = %+v", rows)
	}
}

func TestTimePreventedRepoOverride(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo"
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	body := "[frames]\nenabled = [\"@tier-1\"]\n\n[time-estimates]\ntier-1 = 6.0\n"
	if err := os.WriteFile(filepath.Join(root, "repo-a", "appframes.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := analytics.Open(analyticsDBPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := analytics.Ingest(db, root); err != nil {
		t.Fatal(err)
	}

	actual, _, rows := timePrevented(db, root, "repo-a", time.Time{})
	if actual != 6.0 {
		t.Errorf("actual=%v, want 6.0 (project-tier override)", actual)
	}
	if rows[0].Source != "project-tier" {
		t.Errorf("source = %q, want project-tier", rows[0].Source)
	}
}
