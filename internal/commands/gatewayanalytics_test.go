// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeAuditLine appends a JSONL audit record under <root>/<repo>/audit.log.
func writeAuditLine(t *testing.T, root, repo, line string) {
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

func TestAnalyticsStatsJSON(t *testing.T) {
	root := t.TempDir()
	writeAuditLine(t, root, "repoA", `{"time":"2026-05-26T00:00:00Z","repo":"repoA","refs":["refs/heads/main"],"accept":true}`)
	writeAuditLine(t, root, "repoA", `{"time":"2026-05-26T00:01:00Z","repo":"repoA","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"security/no-private-keys-in-repo","severity":"BLOCK"}]}`)

	out := captureStdout(t, func() int {
		return gatewayAnalytics([]string{"stats", "--policy-root", root, "--json"})
	})

	var s struct {
		Decisions int `json:"decisions"`
		Rejects   int `json:"rejects"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		t.Fatalf("stats --json not valid JSON: %v\n%s", err, out)
	}
	if s.Decisions != 2 || s.Rejects != 1 {
		t.Errorf("decisions=%d rejects=%d, want 2/1", s.Decisions, s.Rejects)
	}
	if _, err := os.Stat(filepath.Join(root, "analytics.db")); err != nil {
		t.Errorf("analytics.db not created: %v", err)
	}
}

func TestAnalyticsIngestThenStatsText(t *testing.T) {
	root := t.TempDir()
	writeAuditLine(t, root, "repoA", `{"time":"2026-05-26T00:00:00Z","repo":"repoA","refs":["refs/heads/main"],"accept":true}`)
	if rc := gatewayAnalytics([]string{"ingest", "--policy-root", root}); rc != 0 {
		t.Fatalf("ingest rc = %d, want 0", rc)
	}
	out := captureStdout(t, func() int {
		return gatewayAnalytics([]string{"stats", "--policy-root", root})
	})
	if !strings.Contains(out, "decisions") {
		t.Errorf("text stats missing 'decisions':\n%s", out)
	}
}

func TestAnalyticsUnknownSub(t *testing.T) {
	if rc := gatewayAnalytics([]string{"frobnicate"}); rc == 0 {
		t.Error("unknown analytics subcommand should return nonzero")
	}
}

func captureStdout(t *testing.T, fn func() int) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}
