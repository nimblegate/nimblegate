// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/whitelist"
)

func TestBuildRepoBlock(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo" // tier-1, 4.0h
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:01:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	db, err := analytics.Open(analyticsDBPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := analytics.Ingest(db, root); err != nil {
		t.Fatal(err)
	}

	b := buildRepoBlock(db, root, "repo-a", time.Time{})
	if b.Decisions != 2 || b.Rejects != 2 {
		t.Errorf("decisions=%d rejects=%d, want 2/2 (pushes are events)", b.Decisions, b.Rejects)
	}
	if b.ActualHours != 4.0 {
		t.Errorf("actual hours = %v, want 4.0 (one distinct issue, not 2)", b.ActualHours)
	}
	if len(b.Recurring) != 1 || b.Recurring[0].Seen != 2 {
		t.Errorf("recurring = %+v, want one row seen=2", b.Recurring)
	}
}

func TestBuildRepoBlockWhitelist(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	if _, err := whitelist.AddEntry(wlPath, whitelist.Entry{Frame: "security/no-private-keys-in-repo", Path: "internal/x_test.go", Reason: "fixture keys"}); err != nil {
		t.Fatal(err)
	}
	db, err := analytics.Open(analyticsDBPath(root))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	b := buildRepoBlock(db, root, "repo-a", time.Time{})
	if len(b.Whitelisted) != 1 || b.Whitelisted[0].Frame != "security/no-private-keys-in-repo" || b.Whitelisted[0].Path != "internal/x_test.go" || b.Whitelisted[0].Reason != "fixture keys" {
		t.Errorf("Whitelisted = %+v, want one no-private-keys entry", b.Whitelisted)
	}
}

func TestFirstPathInMessage(t *testing.T) {
	cases := map[string]string{
		"private keys detected (content redacted): internal/x_test.go:27, PEM key; y:5, z": "internal/x_test.go",
		"pipe-to-shell detected: cmd/installer/install.sh:6, curl|sh":                      "cmd/installer/install.sh",
		"no location here": "",
	}
	for msg, want := range cases {
		if got := firstPathInMessage(msg); got != want {
			t.Errorf("firstPathInMessage(%q) = %q, want %q", msg, got, want)
		}
	}
}

func TestPathIsPattern(t *testing.T) {
	for _, p := range []string{"**", "**/*_test.go", "internal/*", "a/b/", "x?.go", "f[12].go"} {
		if !pathIsPattern(p) {
			t.Errorf("pathIsPattern(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"internal/gateway/e2e_test.go", "a.go", "cmd/installer/install.sh"} {
		if pathIsPattern(p) {
			t.Errorf("pathIsPattern(%q) = true, want false", p)
		}
	}
}
