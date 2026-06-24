// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"nimblegate/internal/gateway"
	"strings"
	"testing"
)

func TestGwShellAssetServed(t *testing.T) {
	rec := httptest.NewRecorder()
	serveGwShellJS(rec, nil)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Errorf("gwshell.js content-type = %q, want javascript", ct)
	}
	if !strings.Contains(rec.Body.String(), "data-repo-switch") {
		t.Errorf("gwshell.js body missing repo-switch handler:\n%s", rec.Body.String())
	}
}

func TestRenderGwShell_chrome(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwShell(rec, gwLayout{
		Title:   "gateway",
		Content: "<p>CONTENT-MARKER</p>",
		Chrome: chromeData{
			Build: "abc1234", Mode: "observe",
			Repos: []string{"api", "web"}, ActiveRepo: "api", ActiveSection: "feed",
		},
	})
	b := rec.Body.String()
	for _, want := range []string{
		"CONTENT-MARKER",
		`class="gw-shell"`, `class="gw-rail"`, `class="gw-top"`, "<svg",
		">Feed<", ">Stats<", ">Frames<", ">Policy<", ">Settings<", `href="/settings"`,
		`data-repo-switch`, "data-rail-open", `class="gw-backdrop"`,
		`class="modebadge"`, ">observe<",
		`data-rail="expanded"`,
		`name="viewport"`,
		`/static/gwshell.js`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("chrome missing %q\n%s", want, b)
		}
	}
}

func TestRenderGwShell_activeSectionAndRepoLinks(t *testing.T) {
	rec := httptest.NewRecorder()
	renderGwShell(rec, gwLayout{Title: "x", Content: "",
		Chrome: chromeData{ActiveRepo: "api", ActiveSection: "stats", Repos: []string{"api"}}})
	b := rec.Body.String()
	if !strings.Contains(b, `href="/stats?repo=api" class="gw-railitem active"`) {
		t.Errorf("active Stats link with repo query missing:\n%s", b)
	}
	if !strings.Contains(b, `href="/?repo=api"`) {
		t.Errorf("Feed link should carry repo query:\n%s", b)
	}
}

func TestPolicyPage_hasShellChrome(t *testing.T) {
	rec := httptest.NewRecorder()
	renderPolicyHTTP(rec, policyVM{Repo: "api"}, policyPageOpts{Repos: []string{"api"}, Chrome: chromeData{ActiveSection: "policy", ActiveRepo: "api", Repos: []string{"api"}}})
	b := rec.Body.String()
	for _, want := range []string{`class="gw-rail"`, `id="repo-header"`, `href="/policy?repo=api" class="gw-railitem active"`} {
		if !strings.Contains(b, want) {
			t.Errorf("policy page missing %q\n%s", want, b)
		}
	}
}

func TestGatewayMode(t *testing.T) {
	if got := gatewayMode("/does/not/matter", ""); got != "-" {
		t.Errorf("all-repos mode = %q, want -", got)
	}
	if got := gatewayMode(t.TempDir(), "missing"); got != "-" {
		t.Errorf("unreadable policy mode = %q, want -", got)
	}
	root := t.TempDir()
	if err := (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "api", Enabled: true, Observe: true}); err != nil {
		t.Fatalf("save policy: %v", err)
	}
	if got := gatewayMode(root, "api"); got != "observe" {
		t.Errorf("observe policy mode = %q, want observe", got)
	}
	if err := (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "ent", Enabled: true, Observe: false}); err != nil {
		t.Fatalf("save enforce policy: %v", err)
	}
	if got := gatewayMode(root, "ent"); got != "enforce" {
		t.Errorf("enforce policy mode = %q, want enforce", got)
	}
	if err := (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "dis", Enabled: false}); err != nil {
		t.Fatalf("save off policy: %v", err)
	}
	if got := gatewayMode(root, "dis"); got != "off" {
		t.Errorf("disabled policy mode = %q, want off", got)
	}
}

// TestLocalDashboardUnaffected guards the invariant that the gateway shell CSS
// lives only in gwShellStyle, never in the shared dashStyle (which the local
// `nimblegate dashboard` also renders). If a future edit moves shell CSS into
// dashStyle, this fails.
func TestLocalDashboardUnaffected(t *testing.T) {
	if strings.Contains(dashStyle, "gw-shell") || strings.Contains(dashStyle, "gw-rail") {
		t.Fatalf("gateway shell CSS leaked into the shared dashStyle: keep it in gwShellStyle")
	}
	if !strings.Contains(dashStyle, "header{padding") {
		t.Errorf("dashStyle header rule unexpectedly changed")
	}
}
