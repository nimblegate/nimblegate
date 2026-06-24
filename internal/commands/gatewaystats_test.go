// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/whitelist"
)

// registerRepo writes a minimal <root>/<repo>/gateway.toml so the repo counts as
// "registered" (policyRootNotice → "").
func registerRepo(t *testing.T, root, repo string) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte("upstream = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getStats(t *testing.T, root, query string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/stats?"+query, nil)
	rec := httptest.NewRecorder()
	serveStats(rec, req, root, false, "")
	return rec.Code, rec.Body.String()
}

func TestServeStatsRenders(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:01:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"security/no-private-keys-in-repo","severity":"BLOCK"}]}`)

	code, body := getStats(t, root, "")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	for _, want := range []string{"decisions: 2", "accepts: 1", "rejects: 1", "repo-a", "security/no-private-keys-in-repo"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, `class="warn"`) {
		t.Errorf("unexpected misconfig banner when repos are registered")
	}
}

func TestServeStatsRepoFilter(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	registerRepo(t, root, "repo-b")
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)
	writeAuditLine(t, root, "repo-b", `{"time":"2026-05-26T00:00:00Z","repo":"repo-b","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"x/y","severity":"WARN"}]}`)

	_, body := getStats(t, root, "repo=repo-a")
	if !strings.Contains(body, "decisions: 1") {
		t.Errorf("repo=repo-a should show 1 decision\n%s", body)
	}
	if strings.Contains(body, "x/y") {
		t.Errorf("repo=repo-a must not include repo-b's frame x/y\n%s", body)
	}
}

func TestServeStatsWindowFilter(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	writeAuditLine(t, root, "repo-a", `{"time":"2020-01-01T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)
	recent := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339)
	writeAuditLine(t, root, "repo-a", fmt.Sprintf(`{"time":"%s","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`, recent))

	_, body := getStats(t, root, "window=24h")
	if !strings.Contains(body, "decisions: 1") {
		t.Errorf("window=24h should count only the recent decision\n%s", body)
	}
}

func TestServeStatsTraversalClamp(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)

	code, body := getStats(t, root, "repo=../etc")
	if code != 200 {
		t.Fatalf("traversal repo should clamp to all-repos and return 200, got %d", code)
	}
	if !strings.Contains(body, "decisions: 1") {
		t.Errorf("clamped (all-repos) view should show the 1 decision\n%s", body)
	}
}

func TestServeStatsMisconfigBanner(t *testing.T) {
	root := t.TempDir() // no registered repo (no */gateway.toml)
	code, body := getStats(t, root, "")
	if code != 200 {
		t.Fatalf("status = %d, want 200", code)
	}
	if !strings.Contains(body, `class="warn"`) {
		t.Errorf("empty/misconfig root should render the .warn banner\n%s", body)
	}
}

func TestFeedHeaderHasStatsLink(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwPage(rec, sampleVM(), "", chromeData{})
	if !strings.Contains(rec.Body.String(), `href="/stats"`) {
		t.Errorf("feed header missing Stats nav link:\n%s", rec.Body.String())
	}
}

func TestServeStatsTimePrevented(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo"
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:01:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)

	// Time-saved tab (default) has the time-prevented numbers + breakdown.
	_, body := getStats(t, root, "")
	for _, want := range []string{"Estimated debugging time saved", "Actually prevented", "Modeled", "4.0h", "Per-frame breakdown", "hrs/hit"} {
		if !strings.Contains(body, want) {
			t.Errorf("time-saved tab missing %q\n%s", want, body)
		}
	}
	// Recurring tab has the recurring-findings table.
	_, body2 := getStats(t, root, "tab=recurring")
	for _, want := range []string{"Recurring findings (1)", "2×"} {
		if !strings.Contains(body2, want) {
			t.Errorf("recurring tab missing %q\n%s", want, body2)
		}
	}
}

// A finding's frame ID renders as a /frames?id= cross-link in the stats tables
// (both the recurring-findings table and the time-prevented breakdown).
func TestServeStatsFrameIDCrossLinks(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo"
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)

	_, body := getStats(t, root, "repo=repo-a")
	// html/template URL-escapes the "/" in the href attribute context to %2f;
	// Go's query parser decodes it back to "/" on the /frames side, so the link
	// resolves. Assert the escaped link text appears for this frame.
	if !strings.Contains(body, `<a href="/frames?id=security%2fno-private-keys-in-repo">`+key+`</a>`) {
		t.Errorf("stats should cross-link frame ID to /frames?id=\n%s", body)
	}
}

func TestServeStatsComposesMultipleRepos(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	registerRepo(t, root, "repo-b")
	const key = "security/no-private-keys-in-repo" // tier-1, 4.0h each
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"a.pem:1"}]}`)
	writeAuditLine(t, root, "repo-b", `{"time":"2026-05-26T00:00:00Z","repo":"repo-b","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"b.pem:2"}]}`)

	_, body := getStats(t, root, "")
	if !strings.Contains(body, `<h2 class="gw-stats-repo">repo-a</h2>`) || !strings.Contains(body, `<h2 class="gw-stats-repo">repo-b</h2>`) {
		t.Errorf("all-repos view should render both repo blocks\n%s", body)
	}
	// Portfolio header sums both repos' prevented hours: 4.0 + 4.0 = 8.0h.
	if !strings.Contains(body, "prevented: 8.0h") {
		t.Errorf("header should sum per-repo prevented hours to 8.0h\n%s", body)
	}
}

func TestServeStatsWhitelistButton(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	const key = "security/no-private-keys-in-repo"
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"`+key+`","severity":"BLOCK","message":"internal/x_test.go:27 : PEM key"}]}`)

	// allow-edits: form + prefilled path + scope hint render on the recurring tab.
	req := httptest.NewRequest("GET", "/stats?repo=repo-a&tab=recurring", nil)
	rec := httptest.NewRecorder()
	serveStats(rec, req, root, true, "tok")
	body := rec.Body.String()
	for _, want := range []string{"whitelist", "/policy/whitelist/add", "internal/x_test.go", "X-CSRF-Token", "wlScope"} {
		if !strings.Contains(body, want) {
			t.Errorf("allow-edits body missing %q", want)
		}
	}
	// read-only: no form.
	_, ro := getStats(t, root, "repo=repo-a&tab=recurring")
	if strings.Contains(ro, "/policy/whitelist/add") {
		t.Error("read-only stats must not render the whitelist form")
	}
}

func TestServeStatsWhitelistedPanel(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	if _, err := whitelist.AddEntry(wlPath, whitelist.Entry{Frame: "commands/curl-pipe-shell", Path: "cmd/installer/install.sh", Reason: "documented installer"}); err != nil {
		t.Fatal(err)
	}
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)

	_, body := getStats(t, root, "repo=repo-a&tab=recurring") // read-only path - must show regardless of allow-edits
	for _, want := range []string{"Whitelist (1)", "commands/curl-pipe-shell", "cmd/installer/install.sh", "documented installer"} {
		if !strings.Contains(body, want) {
			t.Errorf("stats body missing %q\n%s", want, body)
		}
	}
	// Read-only path → NO Remove button on whitelisted rows.
	if strings.Contains(body, "/policy/whitelist/remove") {
		t.Error("read-only stats must not render the whitelist Remove button")
	}
}

func TestServeStatsWhitelistedPanel_hasRemoveButtonWithAllowEdits(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	if _, err := whitelist.AddEntry(wlPath, whitelist.Entry{Frame: "commands/curl-pipe-shell", Path: "cmd/installer/install.sh", Reason: "documented installer"}); err != nil {
		t.Fatal(err)
	}
	writeAuditLine(t, root, "repo-a", `{"time":"2026-05-26T00:00:00Z","repo":"repo-a","refs":["refs/heads/main"],"accept":true}`)

	req := httptest.NewRequest("GET", "/stats?repo=repo-a&tab=recurring", nil)
	rec := httptest.NewRecorder()
	serveStats(rec, req, root, true, "tok")
	body := rec.Body.String()
	for _, want := range []string{`/policy/whitelist/remove`, `wlrm-out`, `>Remove<`} {
		if !strings.Contains(body, want) {
			t.Errorf("stats body missing %q (allow-edits path)\n%s", want, body)
		}
	}
}

// The whitelist panel renders even for a repo with no decisions yet (it's repo
// config, independent of activity) - the panel sits outside the HasData guard.
func TestStatsLastSeenIsMachineReadable(t *testing.T) {
	rec := httptest.NewRecorder()
	data := statsPageData{Repo: "api", ActiveTab: "recurring", Repos: []string{"api"}, Blocks: []repoBlock{{
		Repo: "api", HasData: true,
		Recurring: []recurringRow{{FrameID: "security/x", Severity: "BLOCK", Message: "k:1", Seen: 3, LastSeen: 1716724860}},
	}}}
	renderStatsPage(rec, data, chromeData{ActiveSection: "stats"})
	b := rec.Body.String()
	if !strings.Contains(b, `datetime="2024-05-26T12:01:00Z"`) {
		t.Errorf("recurring last-seen should be a <time> with RFC3339 UTC datetime:\n%s", b)
	}
}

func TestStatsLastSeenHasHourColorClass(t *testing.T) {
	rec := httptest.NewRecorder()
	data := statsPageData{Repo: "api", ActiveTab: "recurring", Repos: []string{"api"}, Blocks: []repoBlock{{
		Repo: "api", HasData: true,
		Recurring: []recurringRow{{FrameID: "security/x", Severity: "BLOCK", Message: "k:1", Seen: 3, LastSeen: 1716724860}},
	}}}
	renderStatsPage(rec, data, chromeData{ActiveSection: "stats"})
	b := rec.Body.String()
	if !strings.Contains(b, `class="gw-ts gw-tc-12"`) {
		t.Errorf("stats last-seen should carry the hour-12 color class:\n%s", b)
	}
}

func TestServeStatsWhitelistedPanelNoDecisions(t *testing.T) {
	root := t.TempDir()
	registerRepo(t, root, "repo-a")
	wlPath := filepath.Join(root, "repo-a", ".appframes", "_canonical", "whitelist.toml")
	if _, err := whitelist.AddEntry(wlPath, whitelist.Entry{Frame: "commands/curl-pipe-shell", Path: "cmd/installer/install.sh", Reason: "documented installer"}); err != nil {
		t.Fatal(err)
	}
	// No audit lines → HasData false; the whitelist panel must still show on the recurring tab.
	_, body := getStats(t, root, "repo=repo-a&tab=recurring")
	if !strings.Contains(body, "No decisions recorded yet") {
		t.Errorf("expected the no-decisions note\n%s", body)
	}
	if !strings.Contains(body, "Whitelist (1)") || !strings.Contains(body, "cmd/installer/install.sh") {
		t.Errorf("whitelist panel must render even with no decisions\n%s", body)
	}
}

func TestStatsPage_hasShellChrome(t *testing.T) {
	rec := httptest.NewRecorder()
	renderStatsPage(rec, statsPageData{Repo: "api", Repos: []string{"api"}}, chromeData{ActiveSection: "stats", Repos: []string{"api"}, ActiveRepo: "api"})
	b := rec.Body.String()
	for _, want := range []string{`class="gw-rail"`, `id="stats-results"`, `href="/stats?repo=api" class="gw-railitem active"`} {
		if !strings.Contains(b, want) {
			t.Errorf("stats page missing %q\n%s", want, b)
		}
	}
}

// A failed refresh (ingest) is non-fatal: the page shows the existing stats
// plus a "couldn't refresh" note, not just the error (the read-only-DB case).
func TestStatsPageShowsStaleDataOnIngestWarn(t *testing.T) {
	rec := httptest.NewRecorder()
	renderStatsPage(rec, statsPageData{
		Warn:         "couldn't refresh (showing existing data): readonly",
		TotDecisions: 5,
		TotAccepts:   4,
		TotRejects:   1,
	}, chromeData{})
	body := rec.Body.String()
	// (html/template escapes the apostrophe in "couldn't", so match an
	// apostrophe-free substring of the warn note.)
	if !strings.Contains(body, "showing existing data") {
		t.Errorf("missing refresh warning:\n%s", body)
	}
	if !strings.Contains(body, "decisions: 5") {
		t.Errorf("stale stats should still render alongside the warning:\n%s", body)
	}
}
