// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func auditLine(t *testing.T, ts time.Time, accept, observed bool) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"time": ts, "repo": "r", "accept": accept, "observed": observed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestRunAuditPrune_DropsOldAcceptsKeepsRejectsForever(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour) // older than 30d
	fresh := now.Add(-1 * time.Hour)
	lines := []string{
		auditLine(t, old, true, false),   // old accept  -> pruned
		auditLine(t, old, false, false),  // old reject  -> kept (reject_retention=0)
		auditLine(t, old, true, true),    // old observed-> kept (observed never pruned)
		auditLine(t, fresh, true, false), // fresh accept-> kept
		"{not valid json",                // unparseable -> kept
	}
	path := filepath.Join(repo, "audit.log")
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o600); err != nil {
		t.Fatal(err)
	}

	res := runAuditPrune(func() time.Time { return now }, root, 30*24*time.Hour, 0)
	if len(res) != 1 {
		t.Fatalf("want 1 result, got %d", len(res))
	}
	r := res[0]
	if r.Err != nil {
		t.Fatalf("unexpected err: %v", r.Err)
	}
	if r.PrunedAccept != 1 || r.PrunedReject != 0 {
		t.Fatalf("want pruned accept=1 reject=0, got accept=%d reject=%d", r.PrunedAccept, r.PrunedReject)
	}
	if r.KeptUnparseable != 1 {
		t.Fatalf("want KeptUnparseable=1, got %d", r.KeptUnparseable)
	}
	out, _ := os.ReadFile(path)
	if got := countLines(string(out)); got != 4 {
		t.Fatalf("want 4 lines remaining, got %d:\n%s", got, out)
	}
}

func TestRunAuditPrune_FiniteRejectRetentionDropsOldRejects(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	_ = os.MkdirAll(repo, 0o755)
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	old := now.Add(-400 * 24 * time.Hour)
	_ = os.WriteFile(filepath.Join(repo, "audit.log"),
		[]byte(auditLine(t, old, false, false)+"\n"), 0o600)

	res := runAuditPrune(func() time.Time { return now }, root, 30*24*time.Hour, 365*24*time.Hour)
	if res[0].PrunedReject != 1 {
		t.Fatalf("want pruned reject=1, got %d", res[0].PrunedReject)
	}
}

func joinLines(ls []string) string {
	out := ""
	for _, l := range ls {
		out += l + "\n"
	}
	return out
}

func countLines(s string) int {
	n := 0
	for _, l := range splitNonEmpty(s) {
		_ = l
		n++
	}
	return n
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}
