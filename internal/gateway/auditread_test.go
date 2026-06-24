// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLog(t *testing.T, dir, repo string, recs []AuditRecord, extraRaw ...string) {
	t.Helper()
	d := filepath.Join(dir, repo)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(d, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range recs {
		b, _ := json.Marshal(r)
		f.Write(append(b, '\n'))
	}
	for _, raw := range extraRaw {
		f.WriteString(raw + "\n")
	}
}

func TestReadDecisions(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	writeLog(t, root, "api", []AuditRecord{
		{Time: now, Repo: "api", Accept: true},
		{Time: now, Repo: "api", Accept: false, Messages: []string{"x: BLOCK [f/y] z"}},
		{Time: now, Repo: "api", Accept: true},
	}, "{not valid json")
	writeLog(t, root, "web", []AuditRecord{{Time: now, Repo: "web", Accept: true}})
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := ReadDecisions(root, 500)
	if len(got) != 4 {
		t.Fatalf("got %d records, want 4 (3 api + 1 web; malformed skipped): %+v", len(got), got)
	}
}

func TestReadDecisions_tailsLastN(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	var recs []AuditRecord
	for i := 0; i < 10; i++ {
		recs = append(recs, AuditRecord{Time: now, Repo: "api", Accept: i >= 7}) // last 3 (i=7,8,9) are accepts
	}
	writeLog(t, root, "api", recs)
	got := ReadDecisions(root, 3)
	if len(got) != 3 {
		t.Fatalf("tail 3 = %d records, want 3", len(got))
	}
	for i, r := range got {
		if !r.Accept {
			t.Errorf("tail should keep the LAST 3 (all Accept=true); record %d Accept=false - got head, not tail", i)
		}
	}
}

func TestReadDecisions_missingRoot(t *testing.T) {
	if got := ReadDecisions(filepath.Join(t.TempDir(), "nope"), 500); len(got) != 0 {
		t.Errorf("missing root should yield 0, got %d", len(got))
	}
}

func TestReadDecisionsBefore_ReturnsOnlyOlder(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	ts := []time.Time{base, base.Add(1 * time.Hour), base.Add(2 * time.Hour), base.Add(3 * time.Hour)}
	writeLog(t, root, "r", []AuditRecord{
		{Time: ts[0], Repo: "r", Accept: true},
		{Time: ts[1], Repo: "r", Accept: true},
		{Time: ts[2], Repo: "r", Accept: true},
		{Time: ts[3], Repo: "r", Accept: true},
	})

	cut := base.Add(2 * time.Hour) // strictly-before -> base+0h and base+1h qualify
	got := ReadDecisionsBefore(root, cut, 500)
	if len(got) != 2 {
		t.Fatalf("want 2 records older than cut, got %d", len(got))
	}
	for _, r := range got {
		if !r.Time.Before(cut) {
			t.Fatalf("record %s not before cut %s", r.Time, cut)
		}
	}
}

func TestReadDecisionsBefore_ZeroBeforeMatchesReadDecisions(t *testing.T) {
	root := t.TempDir()
	base := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	writeLog(t, root, "r", []AuditRecord{
		{Time: base, Repo: "r", Accept: true},
		{Time: base.Add(time.Hour), Repo: "r", Accept: true},
	})
	if len(ReadDecisionsBefore(root, time.Time{}, 500)) != len(ReadDecisions(root, 500)) {
		t.Fatal("zero-before should match ReadDecisions count")
	}
}
