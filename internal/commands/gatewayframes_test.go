// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"os"
	"path/filepath"
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

// A linter has no catalog entry, but the stats page links its findings by the
// synthetic frame ID - so the handler has to resolve it from the repo's policy
// rather than 404 on a link the product itself renders.
func TestServeGatewayFrames_resolvesLinterID(t *testing.T) {
	root := gwPolicyRootWithLinter(t)
	rec := httptest.NewRecorder()
	serveGatewayFrames(root)(rec, httptest.NewRequest("GET", "/frames?id=app-correctness/no-em-dash&repo=payments", nil))
	if rec.Code != 200 {
		t.Fatalf("code=%d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"app-correctness/no-em-dash", "WARN", "linter", "(custom)"} {
		if !strings.Contains(body, want) {
			t.Errorf("linter page missing %q\n%s", want, body)
		}
	}
}

// Without the repo there is no policy to resolve against, and a linter the
// policy no longer declares has genuinely stopped existing - both stay 404.
func TestServeGatewayFrames_linterIDNeedsItsRepo(t *testing.T) {
	root := gwPolicyRootWithLinter(t)
	for _, url := range []string{
		"/frames?id=app-correctness/no-em-dash",
		"/frames?id=app-correctness/no-em-dash&repo=other",
		"/frames?id=app-correctness/dropped&repo=payments",
	} {
		rec := httptest.NewRecorder()
		serveGatewayFrames(root)(rec, httptest.NewRequest("GET", url, nil))
		if rec.Code != 404 {
			t.Errorf("%s: code=%d, want 404", url, rec.Code)
		}
	}
}

func gwPolicyRootWithLinter(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "payments"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "[frames]\nenabled = []\n\n[linters]\n  [linters.no-em-dash]\n" +
		"    kind = \"regex\"\n    enabled = true\n    severity = \"WARN\"\n" +
		"    patterns = [\"*\"]\n    regex = \"x\"\n"
	if err := os.WriteFile(filepath.Join(root, "payments", "appframes.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
