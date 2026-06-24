// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeDeadletter writes N records to a deadletter file, each with queued_at
// offset from baseTime. Returns the file path.
func writeDeadletter(t *testing.T, dir string, baseTime time.Time, offsets ...time.Duration) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pr-comment-deadletter.jsonl")
	var b strings.Builder
	for i, off := range offsets {
		ts := baseTime.Add(off).Format(time.RFC3339Nano)
		fmt.Fprintf(&b, "{\"id\":\"rec-%d\",\"queued_at\":\"%s\",\"notification\":{}}\n", i, ts)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunDeadletterPrune_dropsOldRecords(t *testing.T) {
	policyRoot := t.TempDir()
	baseTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// One repo, 4 records: -40d, -20d, -10d, -1d ago.
	repoDir := filepath.Join(policyRoot, "demo")
	writeDeadletter(t, repoDir, baseTime,
		-40*24*time.Hour, -20*24*time.Hour, -10*24*time.Hour, -1*24*time.Hour,
	)

	now := func() time.Time { return baseTime }
	results := runDeadletterPrune(now, policyRoot, 30*24*time.Hour)
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1 (one repo with file)", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	if r.Scanned != 4 || r.Kept != 3 || r.Pruned != 1 {
		t.Errorf("counts = scanned %d kept %d pruned %d; want 4/3/1", r.Scanned, r.Kept, r.Pruned)
	}

	// File should now contain 3 lines.
	body, _ := os.ReadFile(filepath.Join(repoDir, "pr-comment-deadletter.jsonl"))
	lines := strings.Count(string(body), "\n")
	if lines != 3 {
		t.Errorf("file lines = %d; want 3 after prune", lines)
	}
}

func TestRunDeadletterPrune_skipsRepoWithoutFile(t *testing.T) {
	policyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(policyRoot, "no-deadletter"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Now() }
	results := runDeadletterPrune(now, policyRoot, time.Hour)
	if len(results) != 0 {
		t.Errorf("results = %d; want 0 (no file = no work)", len(results))
	}
}

func TestRunDeadletterPrune_skipsUnderscoreDirs(t *testing.T) {
	policyRoot := t.TempDir()
	// _events, _auth.db, _archive - these shouldn't be scanned as repos.
	for _, name := range []string{"_events", "_archive"} {
		writeDeadletter(t, filepath.Join(policyRoot, name), time.Now(), -time.Hour)
	}
	results := runDeadletterPrune(func() time.Time { return time.Now() }, policyRoot, time.Minute)
	for _, r := range results {
		if strings.HasPrefix(r.Repo, "_") {
			t.Errorf("underscore dir treated as repo: %s", r.Repo)
		}
	}
}

func TestRunDeadletterPrune_dropsMalformedLines(t *testing.T) {
	policyRoot := t.TempDir()
	repoDir := filepath.Join(policyRoot, "demo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoDir, "pr-comment-deadletter.jsonl")
	// Mix valid + malformed lines + a fresh record.
	body := "not-json-at-all\n" +
		`{"queued_at":"2026-01-01T12:00:00Z","id":"old"}` + "\n" +
		`{not even close}` + "\n" +
		`{"queued_at":"` + time.Now().Format(time.RFC3339Nano) + `","id":"fresh"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Now() }
	results := runDeadletterPrune(now, policyRoot, time.Hour)
	if len(results) != 1 {
		t.Fatal("expected one result")
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("err: %v", r.Err)
	}
	// Malformed (2) + old (1) → 3 pruned; fresh → 1 kept.
	if r.Kept != 1 {
		t.Errorf("Kept = %d; want 1", r.Kept)
	}
	if r.Pruned != 3 {
		t.Errorf("Pruned = %d; want 3 (2 malformed + 1 old)", r.Pruned)
	}
}

func TestRunDeadletterPrune_atomicRewriteKeepsMode0600(t *testing.T) {
	policyRoot := t.TempDir()
	baseTime := time.Now()
	repoDir := filepath.Join(policyRoot, "demo")
	path := writeDeadletter(t, repoDir, baseTime, -time.Minute) // fresh, won't be pruned
	runDeadletterPrune(func() time.Time { return baseTime }, policyRoot, time.Hour)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("deadletter mode = %o; want 600 (rewrite shouldn't loosen perms)", st.Mode().Perm())
	}
}
