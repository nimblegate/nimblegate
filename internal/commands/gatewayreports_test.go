// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway/agentapi"
)

// reportsRoot seeds a policy root with one accepted decision for repo "demo".
// gateway.toml registers the repo so listGatewayRepos (which globs
// */gateway.toml) shows it in the dropdown; audit.log gives the analytics
// reports data to ingest.
func reportsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte("upstream = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	line := `{"time":"2026-05-26T00:00:00Z","repo":"demo","refs":["refs/heads/main"],"accept":true,"findings":[{"id":"app-correctness/shellcheck","severity":"WARN","message":"x"}]}`
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestServeReportsPage(t *testing.T) {
	root := reportsRoot(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reports", nil)
	serveReports(rec, req, root)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d", rec.Code)
	}
	for _, want := range []string{
		`<option value="demo"`, "Last 7 days", "Last 30 days", "Last 90 days", "Last year",
		"What changed", "Gate stats", "Bounce rate", "Top rules", "Time saved", "Recurring findings", "Decisions",
		`hx-get="/reports/run?report=`,                                       // report key in the URL (crawlable for the static demo)
		`name="rows"`, `value="50" selected`, `value="500"`, `[name='rows']`, // row-count control (default 50, up to 500) + included in button requests
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page missing %q", want)
		}
	}
}

func TestServeReportRunDispatch(t *testing.T) {
	root := reportsRoot(t)
	svc := &agentapi.Service{PolicyRoot: root, Verify: func(string) (bool, error) { return true, nil }}

	run := func(report, window string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/reports/run?report="+report+"&repo=demo&window="+window, nil)
		serveReportRun(rec, req, svc)
		return rec.Code, rec.Body.String()
	}

	code, b := run("gate-stats", "30")
	if code != http.StatusOK || !strings.Contains(b, "decisions:") || !strings.Contains(b, `<div class="report-row">`) {
		t.Errorf("gate-stats: %d %q", code, b)
	}
	if !strings.Contains(b, "Gate stats") || !strings.Contains(b, "demo") {
		t.Errorf("gate-stats title missing: %q", b)
	}

	_, wc := run("what-changed", "7")
	if !strings.Contains(wc, "repo browsing unavailable") {
		t.Errorf("what-changed (no repos-root) should render its note: %q", wc)
	}

	_, bad := run("nope", "30")
	if !strings.Contains(bad, "unknown report") {
		t.Errorf("unknown report should render an error fragment: %q", bad)
	}
}

func TestServeReportRunEscapesAndWindow(t *testing.T) {
	root := reportsRoot(t)
	svc := &agentapi.Service{PolicyRoot: root, Verify: func(string) (bool, error) { return true, nil }}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reports/run?report=gate-stats&repo=../etc&window=365", nil)
	serveReportRun(rec, req, svc)
	b := rec.Body.String()
	if !strings.Contains(b, "last 365 days") {
		t.Errorf("window not applied: %q", b)
	}
	if strings.Contains(b, "../etc") {
		t.Errorf("invalid repo must be dropped, not rendered: %q", b)
	}
}

func TestFormatReport(t *testing.T) {
	text := "(gateway repos, last 7 days, repo demo)\n" +
		"note: interpreted query \"demo\" as the repository name\n" +
		"decisions: 3, accepted: 2, rejected: 1, repos: 1\n" +
		"2026-06-09 12:14 REJECTED demo refs/heads/main: security/no-keys (BLOCK)\n" +
		"2026-06-11 abc1234 alice feat: x  [✓ accepted: shellcheck (WARN)]  (1 files: a.sh)\n" +
		"2026-06-10 def5678 jill wip  [✗ rejected]"
	out := string(formatReport(text))
	// non-timestamped summary lines also become filterable rows (no <pre>).
	if !strings.Contains(out, `<div class="report-row"><span class="report-rb">decisions: 3, accepted: 2, rejected: 1, repos: 1</span></div>`) {
		t.Errorf("summary line not a plain report-row: %s", out)
	}
	if strings.Contains(out, "<pre") {
		t.Errorf("no report should emit a <pre> block now: %s", out)
	}
	if !strings.Contains(out, `<div class="report-head">`) || !strings.Contains(out, "last 7 days") {
		t.Errorf("header line not lifted: %s", out)
	}
	if !strings.Contains(out, `<div class="report-note">`) {
		t.Errorf("note line not boxed: %s", out)
	}
	// timestamped lines render as feed rows inside a .report-rows card.
	if !strings.Contains(out, `<div class="report-rows">`) || !strings.Contains(out, `<div class="report-row">`) {
		t.Errorf("timestamped body not rendered as feed rows: %s", out)
	}
	// a decisions line (has a time) gets the hour-colored chip; what_changed
	// (date only) gets a plain gw-ts chip - both follow the data-tc toggle.
	if !strings.Contains(out, `<time class="gw-ts gw-tc-12" datetime="2026-06-09T12:14:00Z">06-09 12:14</time>`) {
		t.Errorf("datetime chip / hour color / UTC datetime missing: %s", out)
	}
	// date-only commit rows use a NON-gw-ts class (and full date) so gwApplyTz
	// never touches them - a gw-ts chip with no datetime renders as 1970.
	if !strings.Contains(out, `<time class="report-date">2026-06-11</time>`) {
		t.Errorf("date-only chip (report-date class, verbatim) missing: %s", out)
	}
	if strings.Contains(out, `<time class="gw-ts">2026-06-11`) {
		t.Errorf("date-only chip must not be gw-ts (would epoch-convert): %s", out)
	}
	if !strings.Contains(out, `<span class="rb-ok">✓ accepted</span>`) ||
		!strings.Contains(out, `<span class="rb-rej">✗ rejected</span>`) ||
		!strings.Contains(out, `<span class="rb-rej">REJECTED</span>`) {
		t.Errorf("verdict markers not colored: %s", out)
	}
	if !strings.Contains(out, `<span class="sev-block">(BLOCK)</span>`) ||
		!strings.Contains(out, `<span class="sev-warn">(WARN)</span>`) {
		t.Errorf("severity tokens not colored: %s", out)
	}
}

func TestFormatReportEscapesXSS(t *testing.T) {
	// A commit subject with HTML must be escaped before any span insertion.
	out := string(formatReport("2026-06-11 abc1234 alice <script>alert(1)</script>"))
	if strings.Contains(out, "<script>") {
		t.Errorf("XSS not escaped: %s", out)
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Errorf("expected escaped script tag: %s", out)
	}
}
