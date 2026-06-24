// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeGatewayFramesCatalogue(t *testing.T) {
	req := httptest.NewRequest("GET", "/frames", nil)
	rec := httptest.NewRecorder()
	serveGatewayFrames("/etc/nimblegate-gateway/repos")(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"web/html-seo-meta", "/frames?id=", "Frames"} {
		if !strings.Contains(body, want) {
			t.Errorf("catalogue missing %q", want)
		}
	}
}

func TestServeGatewayFrameDetail(t *testing.T) {
	req := httptest.NewRequest("GET", "/frames?id=web/html-seo-meta", nil)
	rec := httptest.NewRecorder()
	serveGatewayFrames("/etc/nimblegate-gateway/repos")(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	// Body of the frame mentions the meta tags it checks.
	for _, want := range []string{"web/html-seo-meta", "description", "canonical"} {
		if !strings.Contains(body, want) {
			t.Errorf("detail missing %q", want)
		}
	}
}

func TestServeGatewayFrameDetailNotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/frames?id=nope/nope", nil)
	rec := httptest.NewRecorder()
	serveGatewayFrames("/etc/nimblegate-gateway/repos")(rec, req)
	if rec.Code != 404 {
		t.Errorf("code=%d, want 404", rec.Code)
	}
}

func TestGatewayFrames_hasShellChrome(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/frames", nil)
	serveGatewayFrames("/etc/nimblegate-gateway/repos")(rec, req)
	b := rec.Body.String()
	for _, want := range []string{`class="gw-rail"`, `class="gw-railitem active"`, `class="gw-pagehead">Frames`, "Inspection only"} {
		if !strings.Contains(b, want) {
			t.Errorf("frames list missing %q\n%s", want, b)
		}
	}
}

func TestFramesFilterControls(t *testing.T) {
	rec := httptest.NewRecorder()
	serveGatewayFrames("/etc/nimblegate-gateway/repos")(rec, httptest.NewRequest("GET", "/frames", nil))
	b := rec.Body.String()
	for _, want := range []string{
		`id="frames-catalog"`,
		`id="frame-search"`,
		`class="gw-searchbox"`,
		`class="gw-sevchip fnd BLOCK" data-sev="BLOCK"`,
		`data-sev="WARN"`,
		`data-sev="INFO"`,
		`<li data-sev=`,
		`<details class="gw-cat">`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("frames page missing %q\n%s", want, b)
		}
	}
}
