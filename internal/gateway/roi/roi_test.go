// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package roi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/analytics"
)

func seed(t *testing.T, root, repo, line string) {
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
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

func ingested(t *testing.T, root string) *analytics.DB {
	t.Helper()
	db, err := analytics.Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := analytics.Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestPreventedTimeDistinctAndDefault(t *testing.T) {
	root := t.TempDir()
	const key = "security/no-private-keys-in-repo" // tier-1, default 4.0h
	// Three rejected pushes of the SAME finding (deduped to one distinct issue)
	// + one accepted (observed/would-have).
	for _, ts := range []string{"00:00:00", "00:01:00", "00:02:00"} {
		seed(t, root, "repo-a", `{"time":"2026-05-26T`+ts+`Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	}
	seed(t, root, "repo-a", `{"time":"2026-05-26T00:03:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)

	res := PreventedTime(ingested(t, root), root, "repo-a", time.Time{})
	if res.ActualHours != 4.0 || res.ModeledHours != 4.0 {
		t.Errorf("actual=%v modeled=%v, want 4.0/4.0 (distinct, not inflated by re-push)", res.ActualHours, res.ModeledHours)
	}
	if len(res.Rows) != 1 || res.Rows[0].Rejected != 1 || res.Rows[0].Observed != 1 || res.Rows[0].HoursPerHit != 4.0 {
		t.Errorf("rows = %+v", res.Rows)
	}
}

func TestPreventedTimeRepoOverride(t *testing.T) {
	root := t.TempDir()
	const key = "security/no-private-keys-in-repo"
	seed(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	body := "[frames]\nenabled = [\"@tier-1\"]\n\n[time-estimates]\ntier-1 = 6.0\n"
	if err := os.WriteFile(filepath.Join(root, "repo-a", "appframes.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res := PreventedTime(ingested(t, root), root, "repo-a", time.Time{})
	if res.ActualHours != 6.0 {
		t.Errorf("actual=%v, want 6.0 (project-tier override)", res.ActualHours)
	}
	if res.Rows[0].Source != "project-tier" {
		t.Errorf("source = %q, want project-tier", res.Rows[0].Source)
	}
}

func TestStdlibFrameByIDPopulated(t *testing.T) {
	if _, ok := StdlibFrameByID()["security/no-private-keys-in-repo"]; !ok {
		t.Error("expected a known stdlib frame in the registry")
	}
}
