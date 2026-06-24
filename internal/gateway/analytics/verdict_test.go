// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedVerdict writes an audit JSONL with two pushes to "demo": an accepted push
// (tip SHA aaa1111) carrying a WARN finding, and a branch-deletion (zero
// NewRev, no tip). Then ingests.
func seedVerdict(t *testing.T) *DB {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"time":"2026-05-26T00:00:00Z","repo":"demo","refs":["refs/heads/main"],"ref_updates":[{"Name":"refs/heads/main","OldRev":"old","NewRev":"aaa1111"}],"accept":true,"findings":[{"id":"app-correctness/shellcheck","severity":"WARN","message":"x"}]}`,
		`{"time":"2026-05-26T00:01:00Z","repo":"demo","refs":["refs/heads/tmp"],"ref_updates":[{"Name":"refs/heads/tmp","OldRev":"old","NewRev":"0000000000000000000000000000000000000000"}],"accept":true}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestVerdictForSHAs(t *testing.T) {
	db := seedVerdict(t)
	v, err := VerdictForSHAs(db, "demo", []string{"aaa1111", "unknownsha"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v["aaa1111"]
	if !ok || !got.Accept {
		t.Fatalf("aaa1111 should have an accepted verdict: %+v", v)
	}
	if len(got.TopFindings) != 1 || !strings.Contains(got.TopFindings[0], "app-correctness/shellcheck (WARN)") {
		t.Errorf("findings wrong: %+v", got.TopFindings)
	}
	if _, ok := v["unknownsha"]; ok {
		t.Error("unknown sha must not appear")
	}
	if _, ok := v["0000000000000000000000000000000000000000"]; ok {
		t.Error("branch-deletion tip must not be recorded")
	}
}

func TestVerdictScopedByRepo(t *testing.T) {
	db := seedVerdict(t)
	v, err := VerdictForSHAs(db, "other", []string{"aaa1111"})
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 0 {
		t.Errorf("sha from repo demo must not match repo other: %+v", v)
	}
}

func TestVerdictEmptyInput(t *testing.T) {
	db := seedVerdict(t)
	v, err := VerdictForSHAs(db, "demo", nil)
	if err != nil || len(v) != 0 {
		t.Errorf("empty shas → empty map, no query: %v %v", v, err)
	}
}
