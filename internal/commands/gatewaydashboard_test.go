// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

func TestHtmxAssetServed(t *testing.T) {
	rec := httptest.NewRecorder()
	serveHtmx(rec, nil)
	if rec.Body.Len() < 1000 {
		t.Errorf("htmx asset too small (%d bytes), not vendored?", rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("htmx content-type = %q, want javascript", ct)
	}
}

func sampleVM() gateway.ViewModel {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	return gateway.BuildView([]gateway.AuditRecord{
		{Time: base.Add(1 * time.Minute), Repo: "api", Refs: []string{"refs/heads/main"}, Accept: true},
		{Time: base.Add(2 * time.Minute), Repo: "web", Refs: []string{"refs/heads/main"}, Accept: false,
			Messages: []string{"refs/heads/main: BLOCK [security/no-private-keys-in-repo] key"},
			Findings: []gateway.Finding{{ID: "security/no-private-keys-in-repo", Severity: "BLOCK"}}},
	}, gateway.Filter{})
}

func TestRenderPage(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwPage(rec, sampleVM(), "", chromeData{Build: "t", ActiveSection: "feed"})
	b := rec.Body.String()
	for _, want := range []string{
		`<title>gateway</title>`, `>nimblegate</span>`, `class="gw-repolabel"`, `data-repo-switch`, "htmx.min.js", `id="feed"`, "no-private-keys-in-repo",
		"accepts", "rejects",
		`class="gw-rail"`, `class="gw-railitem active"`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("page missing %q", want)
		}
	}
	for _, want := range []string{`<table class="fr">`, `>time</td>`, `>location</td>`, `>status</td>`, ` REJECT</span>`, `class="gw-ico"`, `name="last100"`} {
		if !strings.Contains(b, want) {
			t.Errorf("feed table missing %q\n%s", want, b)
		}
	}
}

// The 'last 100' toggle caps the view to 100 (default off), and never raises above the read cap.
func TestFilterLast100(t *testing.T) {
	on := filterFromQuery(httptest.NewRequest("GET", "/feed?last100=1", nil), 500)
	if !on.Last100 || on.Limit != 100 {
		t.Errorf("last100=1 (tail 500): Last100=%v Limit=%d, want true/100", on.Last100, on.Limit)
	}
	off := filterFromQuery(httptest.NewRequest("GET", "/feed", nil), 500)
	if off.Last100 || off.Limit != 500 {
		t.Errorf("default: Last100=%v Limit=%d, want false/500", off.Last100, off.Limit)
	}
	small := filterFromQuery(httptest.NewRequest("GET", "/feed?last100=1", nil), 50)
	if small.Limit != 50 {
		t.Errorf("last100 with tail=50: Limit=%d, want 50 (only lowers, never raises)", small.Limit)
	}
}

// Per-finding details: the detail is always rendered (hidden client-side), a
// message-bearing finding is a toggle button, a message-less finding is a plain
// chip, and the message cell stacks repo over branch.
func TestFeedPerRowDetails(t *testing.T) {
	rows := []gateway.DecisionRow{{
		Time: time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC), Repo: "demo", Refs: []string{"refs/heads/main"}, Accept: false,
		Findings: []gateway.Finding{
			{Severity: "BLOCK", ID: "security/no-private-keys-in-repo", Message: "work.txt:1 : OpenSSH private key"},
			{Severity: "INFO", ID: "style/note"},
		},
	}}
	rec := httptest.NewRecorder()
	renderGwFeed(rec, gateway.ViewModel{Rows: rows, Filter: gateway.Filter{Details: false}})
	b := rec.Body.String()

	if !strings.Contains(b, "OpenSSH private key") {
		t.Errorf("detail must always render (no server-side gate):\n%s", b)
	}
	if !strings.Contains(b, `<button type="button" class="fnd BLOCK" aria-expanded="false" title="show rule detail">BLOCK security/no-private-keys-in-repo</button>`) {
		t.Errorf("message finding must render a toggle button:\n%s", b)
	}
	if !strings.Contains(b, `<span class="dmsg">work.txt:1 : OpenSSH private key</span>`) {
		t.Errorf("message finding must render its dmsg:\n%s", b)
	}
	if !strings.Contains(b, `<span class="fnd INFO">INFO style/note</span>`) {
		t.Errorf("message-less finding must render a plain chip:\n%s", b)
	}
	if !strings.Contains(b, `class="gw-repo"`) || !strings.Contains(b, `class="gw-ref"`) {
		t.Errorf("message cell must stack repo/ref:\n%s", b)
	}
	if !strings.Contains(b, `<button type="button" class="gw-ref" aria-expanded="false"`) {
		t.Errorf("gw-ref must be a button so it can toggle message visibility:\n%s", b)
	}
}

func TestFeedHasNoDetailsCheckbox(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwPage(rec, sampleVM(), "", chromeData{ActiveSection: "feed"})
	if strings.Contains(rec.Body.String(), `name="details"`) {
		t.Error("the details checkbox must be removed (per-row expand replaces it)")
	}
}

func TestRenderFeed_isJustRows(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwFeed(rec, sampleVM())
	b := rec.Body.String()
	if strings.Contains(b, "<html") || strings.Contains(b, `id="feed"`) {
		t.Errorf("feed fragment must NOT be a full page:\n%s", b)
	}
	if !strings.Contains(b, "api") || !strings.Contains(b, "no-private-keys-in-repo") {
		t.Errorf("feed fragment missing rows:\n%s", b)
	}
}

func TestRenderFeed_showsNonBlockingFindings(t *testing.T) {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	vm := gateway.BuildView([]gateway.AuditRecord{
		{Time: base, Repo: "api", Refs: []string{"refs/heads/main"}, Accept: true,
			Findings: []gateway.Finding{{ID: "app-correctness/no-owner-todos", Severity: "WARN", Message: "TODO with no owner"}}},
	}, gateway.Filter{})
	rec := httptest.NewRecorder()
	renderGwFeed(rec, vm)
	b := rec.Body.String()
	if !strings.Contains(b, "accept") {
		t.Fatalf("expected the accept label, got:\n%s", b)
	}
	for _, want := range []string{"app-correctness/no-owner-todos", "WARN", "fnd"} {
		if !strings.Contains(b, want) {
			t.Errorf("accepted row missing %q (non-blocking finding not rendered):\n%s", want, b)
		}
	}
}

func TestFilterFromQuery(t *testing.T) {
	r := &http.Request{URL: &url.URL{RawQuery: "repo=api&rejects=1"}}
	f := filterFromQuery(r, 500)
	if f.Repo != "api" || !f.RejectsOnly || f.Limit != 500 {
		t.Errorf("filterFromQuery = %+v", f)
	}
}

func TestFeedTimeIsMachineReadable(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwFeed(rec, sampleVM())
	b := rec.Body.String()
	if !strings.Contains(b, `datetime="2026-05-26T12:01:00Z"`) {
		t.Errorf("feed time should be a <time> with RFC3339 UTC datetime:\n%s", b)
	}
}

func TestFeedTimeHasHourColorClass(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwFeed(rec, sampleVM()) // first record 2026-05-26T12:01:00Z → hour 12
	b := rec.Body.String()
	if !strings.Contains(b, `class="gw-ts gw-tc-12"`) {
		t.Errorf("feed time should carry the hour-12 color class:\n%s", b)
	}
}

func TestFeedSev(t *testing.T) {
	cases := []struct {
		in   []gateway.Finding
		want string
	}{
		{nil, "clean"},
		{[]gateway.Finding{{Severity: "INFO"}}, "INFO"},
		{[]gateway.Finding{{Severity: "WARN"}, {Severity: "INFO"}}, "WARN"},
		{[]gateway.Finding{{Severity: "BLOCK"}, {Severity: "WARN"}}, "BLOCK"},
		{[]gateway.Finding{{Severity: "ERROR"}}, "BLOCK"},
	}
	for _, c := range cases {
		if got := feedSev(c.in); got != c.want {
			t.Errorf("feedSev(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFeedRowsHaveBucket(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwFeed(rec, sampleVM())
	if !strings.Contains(rec.Body.String(), `data-feedsev=`) {
		t.Errorf("feed rows should carry data-feedsev:\n%s", rec.Body.String())
	}
}

func TestFeedHasStatusChips(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwPage(rec, sampleVM(), "", chromeData{ActiveSection: "feed"})
	b := rec.Body.String()
	for _, w := range []string{
		`class="gw-feedchip fnd BLOCK" data-feedsev="BLOCK"`,
		`data-feedsev="WARN"`, `data-feedsev="INFO"`, `data-feedsev="clean"`,
	} {
		if !strings.Contains(b, w) {
			t.Errorf("feed page missing chip %q\n%s", w, b)
		}
	}
}

func TestFeedStacksFindings(t *testing.T) {
	base := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	vm := gateway.BuildView([]gateway.AuditRecord{
		{Time: base, Repo: "api", Refs: []string{"refs/heads/main"}, Accept: false,
			Messages: []string{"x"},
			Findings: []gateway.Finding{
				{ID: "security/no-private-keys-in-repo", Severity: "BLOCK", Message: "key.pem:1"},
				{ID: "app-correctness/no-owner-todos", Severity: "WARN", Message: "a.go:2"},
			}},
	}, gateway.Filter{})
	rec := httptest.NewRecorder()
	renderGwFeed(rec, vm)
	b := rec.Body.String()
	for _, want := range []string{`class="gw-statcell"`, `class="gw-finds"`, `class="gw-find"`} {
		if !strings.Contains(b, want) {
			t.Errorf("feed missing %q\n%s", want, b)
		}
	}
	if n := strings.Count(b, `class="gw-find"`); n != 2 {
		t.Errorf("want 2 gw-find spans (stacked), got %d\n%s", n, b)
	}
	if !strings.Contains(b, `class="loc"`) {
		t.Errorf("time cell should still carry loc")
	}
}

// The feed bar carries a client-side text-search input (+ a count span) inside
// the filter form, and the orphan seam comment is removed.
func TestFeedHasSearchBox(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwPage(rec, sampleVM(), "", chromeData{ActiveSection: "feed"})
	b := rec.Body.String()
	for _, w := range []string{
		`id="feed-search"`,
		`class="gw-searchbox"`,
		`placeholder="filter feed…"`,
		`id="feed-count"`,
	} {
		if !strings.Contains(b, w) {
			t.Errorf("feed page missing %q", w)
		}
	}
	if strings.Contains(b, "seam: search box attaches in the filter bar") {
		t.Error("the seam comment must be removed once the search lands")
	}
}

func TestServeFavicon(t *testing.T) {
	rec := httptest.NewRecorder()
	serveFavicon(rec, nil)
	if ct := rec.Header().Get("Content-Type"); ct != "image/svg+xml" {
		t.Errorf("Content-Type = %q, want image/svg+xml", ct)
	}
	if !strings.Contains(rec.Body.String(), "<svg") {
		t.Error("favicon body is not an SVG")
	}
	if !publicRoute("/favicon.ico") || !publicRoute("/static/favicon.svg") {
		t.Error("favicon paths must be reachable without auth (login tab needs the icon too)")
	}
}

// TestLoadHasMore_requiresNPlus1Read guards the N+1 fetch pattern in the load
// closure of gatewayDashboard. BuildView.HasMore is computed as
// len(rows) > f.Limit; if ReadDecisionsBefore is called with exactly *tail
// (not *tail+1), HasMore is always false on a single-repo gateway and the
// "Load older" button never appears. This test locks in that reading
// *tail+1 records is necessary for HasMore to be true when exactly *tail+1
// records exist.
func TestLoadHasMore_requiresNPlus1Read(t *testing.T) {
	const tail = 5 // small explicit display limit
	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	root := t.TempDir()
	repoDir := filepath.Join(root, "myrepo")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(repoDir, "audit.log")
	// Write tail+1 records so there is exactly one record past the display limit.
	for i := 0; i < tail+1; i++ {
		if err := gateway.AppendAudit(logPath, gateway.AuditRecord{
			Time:   base.Add(time.Duration(i) * time.Minute),
			Repo:   "myrepo",
			Refs:   []string{"refs/heads/main"},
			Accept: true,
		}); err != nil {
			t.Fatal(err)
		}
	}

	f := gateway.Filter{Limit: tail}

	// With N+1 read: BuildView sees the overflow and sets HasMore = true.
	vmMore := gateway.BuildView(gateway.ReadDecisionsBefore(root, time.Time{}, tail+1), f)
	if !vmMore.Summary.HasMore {
		t.Error("HasMore should be true when ReadDecisionsBefore reads tail+1 records and Limit=tail; the Load older button would never appear without the +1")
	}

	// With N read: BuildView cannot see the overflow; HasMore is always false.
	vmNoMore := gateway.BuildView(gateway.ReadDecisionsBefore(root, time.Time{}, tail), f)
	if vmNoMore.Summary.HasMore {
		t.Error("sanity: reading only tail records should yield HasMore=false (all records fit)")
	}
}
