// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/linters"
	"nimblegate/internal/tasks"
)

func TestSplitOpen(t *testing.T) {
	open := []*tasks.Task{
		{FrameID: "security/x", Severity: "BLOCK"},
		{FrameID: "convention/y", Severity: "WARN"},
		{FrameID: "convention/z", Severity: "INFO"},
	}
	d, a := splitOpen(open)
	if len(d) != 1 || d[0].Severity != "BLOCK" {
		t.Errorf("dangerous = %+v, want 1 BLOCK", d)
	}
	if len(a) != 2 {
		t.Errorf("advisory = %d, want 2 (WARN+INFO)", len(a))
	}
}

func TestBuildDashData_verdict(t *testing.T) {
	clean := tasks.NewLedger()
	clean.Tasks["a"] = &tasks.Task{ID: "a", FrameID: "convention/y", Severity: "WARN", Status: tasks.StatusOpen}
	if d := buildDashData("/proj", clean); !d.Ready {
		t.Error("WARN-only ledger should be production-ready")
	}
	withBlock := tasks.NewLedger()
	withBlock.Tasks["b"] = &tasks.Task{ID: "b", FrameID: "security/x", Severity: "BLOCK", Status: tasks.StatusOpen}
	if d := buildDashData("/proj", withBlock); d.Ready {
		t.Error("open BLOCK should NOT be production-ready")
	}
}

func TestDashboardHandler_rendersHTML(t *testing.T) {
	// The handler reads the ledger from disk; with no project ledger it renders
	// the clean state. We exercise the template via buildDashData + execute.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	dashboardTmpl.Execute(rec, buildDashData(t.TempDir(), tasks.NewLedger()))
	body := rec.Body.String()
	if !strings.Contains(body, "nimblegate:") || !strings.Contains(body, "production-ready") {
		t.Errorf("dashboard HTML missing expected content:\n%s", body)
	}
	_ = req
}

func TestDashboardAccordion(t *testing.T) {
	l := tasks.NewLedger()
	l.Tasks["a"] = &tasks.Task{ID: "a", FrameID: "security/x", Severity: "BLOCK", Status: tasks.StatusOpen}
	l.Tasks["b"] = &tasks.Task{ID: "b", FrameID: "convention/y", Severity: "WARN", Status: tasks.StatusOpen}
	rec := httptest.NewRecorder()
	dashboardTmpl.Execute(rec, buildDashData("/p", l))
	b := rec.Body.String()
	if !strings.Contains(b, `<details class="frame"`) {
		t.Error("expected <details> accordion groups")
	}
	if !strings.Contains(b, `data-frame="security/x" open`) {
		t.Errorf("BLOCK group should default open:\n%s", b)
	}
	if !strings.Contains(b, `data-frame="convention/y">`) {
		t.Errorf("advisory group should default collapsed (no open attr):\n%s", b)
	}
	if !strings.Contains(b, "localStorage") {
		t.Error("expected open-state persistence script")
	}
}

func TestBuildFramesList(t *testing.T) {
	mk := func(cat frames.Category, name string, sev frames.Severity, lc frames.Lifecycle, body string) frames.Frame {
		return frames.Frame{
			Frontmatter: frames.Frontmatter{Category: cat, Name: name, Severity: sev, Lifecycle: lc},
			Body:        body,
		}
	}
	stdlibFrames := []frames.Frame{
		mk("security", "xss", "BLOCK", "", "# Catch XSS\nmore"),
		mk("convention", "todo", "WARN", "", "Flag TODO comments"),
		mk("security", "old", "WARN", frames.LifecycleArchived, "archived frame"),
	}
	projectFrames := []frames.Frame{
		mk("security", "xss", "WARN", "", "project override of xss"), // shadows stdlib
	}
	expanded := []string{"security/xss"} // only xss enabled
	overrides := map[string]config.FrameOverride{"convention/todo": {Severity: "INFO"}}
	linterInfos := []linters.LinterInfo{{Name: "eslint", ID: "app-correctness/eslint", Builtin: true, Severity: "WARN", Dir: "studio"}}

	rows := buildFramesList(stdlibFrames, projectFrames, expanded, nil, overrides, linterInfos, nil)

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (2 frames + 1 linter; archived filtered): %+v", len(rows), rows)
	}
	// Sorted by category then ID: app-correctness/eslint (linter) < convention/todo < security/xss.
	for i, want := range []string{"app-correctness/eslint", "convention/todo", "security/xss"} {
		if rows[i].ID != want {
			t.Fatalf("rows[%d].ID = %s, want %s", i, rows[i].ID, want)
		}
	}
	if lint := rows[0]; lint.Source != "linter" || lint.Severity != "WARN" || !lint.Enabled {
		t.Errorf("linter row = %+v, want source=linter sev=WARN enabled", lint)
	}
	todo, xss := rows[1], rows[2]
	if todo.Severity != "INFO" {
		t.Errorf("override not applied: convention/todo = %s, want INFO", todo.Severity)
	}
	if todo.Enabled {
		t.Error("convention/todo not in expanded → should be disabled")
	}
	if xss.Source != "project" {
		t.Errorf("project frame should shadow stdlib: source = %s, want project", xss.Source)
	}
	if xss.Severity != "WARN" {
		t.Errorf("shadowing project severity = %s, want WARN", xss.Severity)
	}
	if !xss.Enabled {
		t.Error("security/xss in expanded → should be enabled")
	}
	if xss.Summary != "project override of xss" {
		t.Errorf("summary = %q, want first body line", xss.Summary)
	}
}

func TestFramesTemplates_renderAndEscape(t *testing.T) {
	rec := httptest.NewRecorder()
	framesListTmpl.Execute(rec, framesPage{
		Project: "p", Count: 1,
		Rows: []frameRow{{ID: "security/xss", Severity: "BLOCK", Enabled: true, Source: "stdlib", Summary: "catch xss"}},
	})
	if b := rec.Body.String(); !strings.Contains(b, "security/xss") || !strings.Contains(b, "frames") {
		t.Errorf("frames list HTML missing content:\n%s", b)
	}
	rec2 := httptest.NewRecorder()
	frameDetailTmpl.Execute(rec2, frameDetail{ID: "security/xss", Severity: "BLOCK", Category: "security", Tier: 1, Body: "<script>alert(1)</script>"})
	b2 := rec2.Body.String()
	if !strings.Contains(b2, "security/xss") {
		t.Errorf("frame detail missing id:\n%s", b2)
	}
	if strings.Contains(b2, "<script>") {
		t.Error("frame body not HTML-escaped: the body is rendered verbatim and must be escaped")
	}
}

func TestLinterDetailTmpl_render(t *testing.T) {
	rec := httptest.NewRecorder()
	linterDetailTmpl.Execute(rec, linters.LinterInfo{
		Name: "eslint", ID: "app-correctness/eslint", Builtin: true,
		Severity: "WARN", Dir: "studio", Patterns: []string{"src"},
	})
	b := rec.Body.String()
	for _, want := range []string{"app-correctness/eslint", "eslint", "built-in", "studio", "linter"} {
		if !strings.Contains(b, want) {
			t.Errorf("linter detail missing %q:\n%s", want, b)
		}
	}
}
