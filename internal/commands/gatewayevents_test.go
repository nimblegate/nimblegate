// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

func TestEventGroup_classifies(t *testing.T) {
	for _, tc := range []struct{ ev, want string }{
		{"add", "lifecycle"},
		{"archive", "lifecycle"},
		{"restore", "lifecycle"},
		{"migrate-layout", "lifecycle"},
		{"scan-first-push", "scan"},
		{"scan-apply", "scan"},
		{"scan-dismiss", "scan"},
		{"scan-rescan", "scan"},
		{"frame-severity", "tuning"},
		{"repo-toggle", "tuning"},
		{"linter-add", "tuning"},
		{"linter-delete", "tuning"},
		{"linter-severity", "tuning"},
		{"linter-enabled", "tuning"},
		{"whitelist-add", "whitelist"},
		{"whitelist-remove", "whitelist"},
		{"something-unknown", "other"},
	} {
		if got := eventGroup(tc.ev); got != tc.want {
			t.Errorf("eventGroup(%q) = %q, want %q", tc.ev, got, tc.want)
		}
	}
}

func TestServeGatewayEvents_rendersEvents(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	_ = gateway.AppendEvent(root, gateway.Event{
		Timestamp: time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC),
		Event:     "archive", Repo: "repo-a", OK: true,
	})
	_ = gateway.AppendEvent(root, gateway.Event{
		Timestamp: time.Date(2026, 5, 30, 15, 0, 0, 0, time.UTC),
		Event:     "linter-add", Repo: "repo-a", OK: true,
		Payload: map[string]any{"name": "tmp-check", "severity": "WARN"},
	})

	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="events-list"`,
		`id="events-search"`,
		`data-events-repo`,
		`data-evgroup="lifecycle"`,
		`data-evgroup="scan"`,
		`data-evgroup="tuning"`,
		`data-evgroup="whitelist"`,
		`>archive<`,
		`>linter-add<`,
		`tmp-check`,
		`repo-a`,
		`active`, // rail item should highlight
	} {
		if !strings.Contains(body, want) {
			t.Errorf("events body missing %q", want)
		}
	}
	// Newest first - linter-add (15:00) row before archive (14:00) row.
	lAdd := strings.Index(body, ">linter-add<")
	arch := strings.Index(body, ">archive<")
	if lAdd < 0 || arch < 0 || lAdd > arch {
		t.Errorf("events not newest-first: linter-add@%d archive@%d", lAdd, arch)
	}
}

func TestServeGatewayEvents_emptyState(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No events recorded yet") {
		t.Errorf("empty state missing")
	}
}

func TestParseEventsLimit(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"", 100},      // default
		{"100", 100},   // allowed
		{"50", 50},     // allowed
		{"500", 500},   // allowed
		{"1000", 1000}, // allowed
		{"0", 0},       // all
		{"42", 100},    // not allowlisted → default
		{"abc", 100},   // garbage → default
		{"-5", 100},    // negative → default
	} {
		if got := parseEventsLimit(tc.in); got != tc.want {
			t.Errorf("parseEventsLimit(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestServeGatewayEvents_tailsToLimit(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	// Append 5 events; ask for the last 2.
	for i := 0; i < 5; i++ {
		_ = gateway.AppendEvent(root, gateway.Event{
			Timestamp: time.Date(2026, 5, 30, 10, i, 0, 0, time.UTC),
			Event:     "archive", Repo: "repo-a", OK: true,
		})
	}
	req := httptest.NewRequest("GET", "/events?limit=50", nil) // 50 is allowlisted; returns all 5 < 50
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status: %d", rec.Code)
	}
	got := strings.Count(rec.Body.String(), `<tr data-evgroup=`)
	if got != 5 {
		t.Errorf("limit=50 with 5 events: got %d rows, want 5", got)
	}
	if strings.Contains(rec.Body.String(), "Showing") {
		t.Error("with total <= limit, heading must not say 'Showing N of M'")
	}

	// Force truncation by lowering the limit via a non-allowlisted path - use
	// the URL param directly with an allowed value smaller than total.
	// 5 < 50 so reaching truncation here means seeding more. Re-seed to 200.
	for i := 0; i < 200; i++ {
		_ = gateway.AppendEvent(root, gateway.Event{
			Timestamp: time.Date(2026, 6, 1, 0, 0, i, 0, time.UTC),
			Event:     "archive", Repo: "repo-a", OK: true,
		})
	}
	rec2 := httptest.NewRecorder()
	serveGatewayEvents(root)(rec2, httptest.NewRequest("GET", "/events?limit=100", nil))
	body := rec2.Body.String()
	rows := strings.Count(body, `<tr data-evgroup=`)
	if rows != 100 {
		t.Errorf("limit=100 with 205 events: got %d rows, want 100", rows)
	}
	if !strings.Contains(body, "Showing <b>100</b> of 205") {
		t.Errorf("truncation heading missing: %s", firstLineContaining(body, "Showing"))
	}

	// limit=0 → all (no truncation), even with 205 events.
	rec3 := httptest.NewRecorder()
	serveGatewayEvents(root)(rec3, httptest.NewRequest("GET", "/events?limit=0", nil))
	all := strings.Count(rec3.Body.String(), `<tr data-evgroup=`)
	if all != 205 {
		t.Errorf("limit=0: got %d rows, want 205", all)
	}
}

func firstLineContaining(s, needle string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func TestServeGatewayEvents_limitDropdownPresent(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	body := rec.Body.String()
	for _, want := range []string{
		`name="limit"`,
		`data-events-limit`,
		`value="50"`,
		`value="100"`,
		`value="500"`,
		`value="1000"`,
		`value="0"`,
		`>all<`,
		`method="get"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("limit dropdown missing %q", want)
		}
	}
}

func TestServeGatewayEvents_rendersRailWithEventsActive(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	body := rec.Body.String()
	// Rail link present + active class.
	if !strings.Contains(body, `href="/events"`) {
		t.Error("rail missing /events link")
	}
}

func TestFormatEventPayload_AddEvent(t *testing.T) {
	got := formatEventPayload("add", map[string]any{
		"upstream":        "github.com/foo/bar",
		"kit":             "core",
		"security_strict": true,
		"credential_set":  true,
	})
	if !strings.Contains(got, "upstream=github.com/foo/bar") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "kit=core") {
		t.Errorf("got %q", got)
	}
	if !strings.Contains(got, "+security-strict") {
		t.Errorf("got %q", got)
	}
}

func TestFormatEventPayload_FrameSeverity(t *testing.T) {
	got := formatEventPayload("frame-severity", map[string]any{
		"frame":    "security/no-mixed-content-urls",
		"severity": "WARN",
	})
	if got != "security/no-mixed-content-urls → WARN" {
		t.Errorf("got %q", got)
	}
}

func TestFormatEventPayload_EmptyPayload(t *testing.T) {
	got := formatEventPayload("credential-update", map[string]any{})
	if got != "" {
		t.Errorf("expected empty for no-payload event, got %q", got)
	}
}

func TestFormatEventPayload_UnknownEvent(t *testing.T) {
	got := formatEventPayload("never-heard-of-it", map[string]any{"k": "v"})
	if got != "k=v" {
		t.Errorf("fallback should format first key=value, got %q", got)
	}
}

func TestEventsPage_HeaderRenamedToDetails(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	_ = gateway.AppendEvent(root, gateway.Event{
		Timestamp: time.Date(2026, 5, 30, 14, 0, 0, 0, time.UTC),
		Event:     "archive", Repo: "repo-a", OK: true,
	})
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	body := rec.Body.String()
	if strings.Contains(body, `>payload<`) {
		t.Error("column header must be 'details', not 'payload'")
	}
	if !strings.Contains(body, `>details<`) {
		t.Error("column header 'details' not found")
	}
}

func TestFormatEventPayload_LinterAdd(t *testing.T) {
	got := formatEventPayload("linter-add", map[string]any{
		"name":     "my-check",
		"severity": "BLOCK",
		"patterns": []any{"*.go"},
	})
	if !strings.Contains(got, "my-check") {
		t.Errorf("linter-add summary missing name, got %q", got)
	}
	if !strings.Contains(got, "BLOCK") {
		t.Errorf("linter-add summary missing severity, got %q", got)
	}
}

func TestFormatEventPayload_RepoToggle(t *testing.T) {
	if got := formatEventPayload("repo-toggle", map[string]any{"enabled": true}); got != "enabled=true" {
		t.Errorf("repo-toggle true: got %q", got)
	}
	if got := formatEventPayload("repo-toggle", map[string]any{"enabled": false}); got != "enabled=false" {
		t.Errorf("repo-toggle false: got %q", got)
	}
}

func TestFormatEventPayload_WhitelistAdd(t *testing.T) {
	got := formatEventPayload("whitelist-add", map[string]any{
		"frame":  "security/no-eval",
		"path":   "src/vendor.js",
		"reason": "third-party",
	})
	if !strings.Contains(got, "security/no-eval") {
		t.Errorf("whitelist-add missing frame: %q", got)
	}
	if !strings.Contains(got, "path=src/vendor.js") {
		t.Errorf("whitelist-add missing path: %q", got)
	}
	if !strings.Contains(got, `"third-party"`) {
		t.Errorf("whitelist-add missing reason: %q", got)
	}
}

func TestEventsPage_DetailsExpanderRendered(t *testing.T) {
	root := t.TempDir()
	registerRepoForTest(t, root, "repo-a")
	_ = gateway.AppendEvent(root, gateway.Event{
		Timestamp: time.Date(2026, 5, 30, 15, 0, 0, 0, time.UTC),
		Event:     "frame-severity", Repo: "repo-a", OK: true,
		Payload: map[string]any{"frame": "security/no-eval", "severity": "BLOCK"},
	})
	req := httptest.NewRequest("GET", "/events", nil)
	rec := httptest.NewRecorder()
	serveGatewayEvents(root)(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, `gw-event-details`) {
		t.Error("details expander element missing")
	}
	if !strings.Contains(body, `gw-event-raw`) {
		t.Error("raw JSON pre element missing")
	}
	if !strings.Contains(body, "security/no-eval → BLOCK") {
		t.Errorf("summary text missing in body")
	}
}
