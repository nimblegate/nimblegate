// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package help

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPageOf(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/", "index"},
		{"/policy", "policy"},
		{"/policy/", "policy"},
		{"/policy?repo=foo", "policy"},
		{"policy", "policy"},
		{"/ssh-keys", "ssh-keys"},
		{"/policy/../etc/passwd", ""},
		{"/POLICY", ""},
		{"/policy.md", ""},
	}
	for _, c := range cases {
		if got := pageOf(c.in); got != c.want {
			t.Errorf("pageOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitTitleAndBody(t *testing.T) {
	in := "# Policy\n\nFirst paragraph.\n\n## Sub\n\nSecond.\n"
	title, body := splitTitleAndBody(in)
	if title != "Policy" {
		t.Errorf("title = %q, want %q", title, "Policy")
	}
	if strings.Contains(body, "# Policy") {
		t.Errorf("body should not contain the H1: %q", body)
	}
	if !strings.Contains(body, "First paragraph.") {
		t.Errorf("body missing first paragraph: %q", body)
	}
}

func TestSplitTitleAndBody_NoH1(t *testing.T) {
	in := "Plain prose, no heading.\n"
	title, body := splitTitleAndBody(in)
	if title != "" {
		t.Errorf("title should be empty when no H1; got %q", title)
	}
	if !strings.Contains(body, "Plain prose") {
		t.Errorf("body should retain text: %q", body)
	}
}

func TestRenderPage_KnownPageReturnsHTML(t *testing.T) {
	title, body, ok := renderPage("index")
	if !ok {
		t.Fatal("renderPage(index) returned ok=false; expected embedded fixture")
	}
	if title == "" {
		t.Error("title should be non-empty for index")
	}
	if !strings.Contains(body, "<p>") && !strings.Contains(body, "<h2>") {
		t.Errorf("body should be HTML-rendered; got %q", body)
	}
}

func TestRenderPage_UnknownPageReturnsNotOk(t *testing.T) {
	_, _, ok := renderPage("does-not-exist")
	if ok {
		t.Error("renderPage(does-not-exist) should return ok=false")
	}
}

func TestRenderPage_CacheIsIdempotent(t *testing.T) {
	t1, b1, _ := renderPage("index")
	t2, b2, _ := renderPage("index")
	if t1 != t2 || b1 != b2 {
		t.Error("render cache returned different content on second call")
	}
}

func TestHandler_KnownPage(t *testing.T) {
	req := httptest.NewRequest("GET", "/help?page=/", nil)
	rec := httptest.NewRecorder()
	Handler()(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `class="help-head"`) {
		t.Errorf("body missing help-head: %q", body)
	}
	if !strings.Contains(body, `class="help-close"`) {
		t.Errorf("body missing close button: %q", body)
	}
	if !strings.Contains(body, `class="help-body"`) {
		t.Errorf("body missing help-body: %q", body)
	}
}

func TestHandler_UnknownPage_FallsBackTo200(t *testing.T) {
	req := httptest.NewRequest("GET", "/help?page=/does-not-exist", nil)
	rec := httptest.NewRecorder()
	Handler()(rec, req)
	if rec.Code != 200 {
		t.Errorf("unknown page should be 200 fallback, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "hasn't been written yet") {
		t.Errorf("fallback body missing expected text: %q", body)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest("POST", "/help", nil)
	rec := httptest.NewRecorder()
	Handler()(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST should be 405, got %d", rec.Code)
	}
}
