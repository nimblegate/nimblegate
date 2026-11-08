// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

const kubeFrame = "security/no-kubeconfig-in-repo"

// previewRepoFixture registers repo "demo" (enforce, refs/heads/* gated) with
// the given enabled list, and a bare repo whose tree holds a kubeconfig.
func previewRepoFixture(t *testing.T, enabled []string) (policyRoot, reposRoot string) {
	t.Helper()
	policyRoot, reposRoot = t.TempDir(), t.TempDir()
	if err := (gateway.FilePolicyStore{Root: policyRoot}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", Enabled: true, ProtectedRefs: []string{"refs/heads/*"}}); err != nil {
		t.Fatal(err)
	}
	if err := (gateway.FramePolicy{Enabled: enabled}).Save(policyRoot, "demo"); err != nil {
		t.Fatal(err)
	}
	makeBareUnderReposRoot(t, reposRoot, "demo", map[string]string{
		"deploy/kubeconfig": "apiVersion: v1\nclusters: []\nusers:\n- name: ci\n  user:\n    client-key-data: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlFb3dJQkFBS0NBUUVBdGVzdA==\n",
		"main.go":           "package main\n",
	})
	return policyRoot, reposRoot
}

func kubeBlocks(res gateway.PreviewResult) int {
	n := 0
	for _, f := range res.Blocks {
		if f.Frame == kubeFrame && f.File == "deploy/kubeconfig" {
			n++
		}
	}
	return n
}

func TestPolicyPreview_engineBlockWhitelistAndFrameOff(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, []string{"git/no-bypass-pre-commit"})

	res, _, err := repoPreview(policyRoot, reposRoot, "demo", []string{kubeFrame})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref != "refs/heads/main" || kubeBlocks(res) == 0 {
		t.Fatalf("candidate with %s must block on the fixture: %+v", kubeFrame, res)
	}

	// The stored list does not hold the frame: no block.
	res, _, _ = repoPreview(policyRoot, reposRoot, "demo", []string{"git/no-bypass-pre-commit"})
	if kubeBlocks(res) != 0 {
		t.Fatalf("frame off, still blocked: %+v", res.Blocks)
	}

	// Whitelist this, through the handler the dashboard posts to.
	h := previewHandlers{policyRoot: policyRoot, reposRoot: reposRoot, token: "tok"}
	w := httptest.NewRecorder()
	h.whitelistThis(w, authoringReq(t, "/policy/preview/whitelist", url.Values{
		"repo": {"demo"}, "frame": {kubeFrame}, "path": {"deploy/kubeconfig"}, "reason": {"test fixture, not a real cluster"},
	}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("whitelist-this = %d %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); loc != "/policy?repo=demo" {
		t.Errorf("redirect = %q", loc)
	}
	if present, n, err := gateway.CheckWhitelist(policyRoot, "demo"); err != nil || !present || n != 1 {
		t.Fatalf("the gate's whitelist loader must accept the entry: present=%v n=%d err=%v", present, n, err)
	}
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "whitelist-add" })
	if len(evs) != 1 {
		t.Errorf("whitelist-add events = %d, want 1", len(evs))
	}

	res, _, _ = repoPreview(policyRoot, reposRoot, "demo", []string{kubeFrame})
	if kubeBlocks(res) != 0 {
		t.Fatalf("whitelisted finding still blocks: %+v", res.Blocks)
	}
}

func TestWhitelistThis_rejectsBadInput(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, nil)
	h := previewHandlers{policyRoot: policyRoot, reposRoot: reposRoot, token: "tok"}
	good := url.Values{"repo": {"demo"}, "frame": {kubeFrame}, "path": {"deploy/kubeconfig"}, "reason": {"fixture"}}
	with := func(k, v string) url.Values {
		out := url.Values{}
		for kk, vv := range good {
			out[kk] = vv
		}
		out.Set(k, v)
		return out
	}
	cases := []struct {
		name  string
		form  url.Values
		token string
		want  int
	}{
		{"bad csrf", good, "nope", http.StatusForbidden},
		{"missing reason", with("reason", "  "), "tok", http.StatusBadRequest},
		{"newline in reason", with("reason", "ok\n[[entry]]"), "tok", http.StatusBadRequest},
		{"dotdot in reason", with("reason", "see ../x"), "tok", http.StatusBadRequest},
		{"newline in path", with("path", "a\nb"), "tok", http.StatusBadRequest},
		{"dotdot in path", with("path", "../../etc/passwd"), "tok", http.StatusBadRequest},
		{"glob path", with("path", "deploy/*"), "tok", http.StatusBadRequest},
		{"absolute path", with("path", "/etc/passwd"), "tok", http.StatusBadRequest},
		{"unknown frame", with("frame", "x/nope"), "tok", http.StatusBadRequest},
		{"bad repo", with("repo", "../demo"), "tok", http.StatusBadRequest},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.whitelistThis(w, authoringReq(t, "/policy/preview/whitelist", c.form, c.token))
		if w.Code != c.want {
			t.Errorf("%s: status %d, want %d", c.name, w.Code, c.want)
		}
	}
	if _, err := os.Stat(gateway.WhitelistPath(policyRoot, "demo")); !os.IsNotExist(err) {
		t.Errorf("a rejected request wrote the whitelist (err=%v)", err)
	}
}

// The mutating preview routes sit inside the --allow-edits block of the
// dashboard, next to the other mutation routes, and nowhere else.
func TestPreviewRoutes_onlyUnderAllowEdits(t *testing.T) {
	src, err := os.ReadFile("gatewaydashboard.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "if *allowEdits {\n\t\tph := policyHandlers{")
	end := strings.Index(s, "// --- auth bootstrap ---")
	if start < 0 || end < start {
		t.Fatal("allow-edits block not found")
	}
	for _, route := range []string{`"/policy/preview/whitelist"`, `"/policy/kits/preview"`, `"/policy/repo/scan-preview"`} {
		if strings.Count(s, route) != 1 {
			t.Errorf("%s registered %d times, want 1", route, strings.Count(s, route))
		}
		if i := strings.Index(s, route); i < start || i > end {
			t.Errorf("%s registered outside the allow-edits block", route)
		}
	}
}

func TestKitConfirm_beforeAfterAndObserveOffer(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, []string{"git/no-bypass-pre-commit"})
	h := previewHandlers{policyRoot: policyRoot, reposRoot: reposRoot, token: "tok"}
	w := httptest.NewRecorder()
	h.kitConfirm(w, httptest.NewRequest("GET", "/policy/kits/preview?repo=demo&name=core", nil))
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	for _, want := range []string{"Apply kit: Core", "after apply", `hx-post="/policy/kits/apply"`, "Apply and switch demo to observe mode", "deploy/kubeconfig", "Whitelist this", `value="kit:core"`} {
		if !strings.Contains(body, want) {
			t.Errorf("confirm view missing %q", want)
		}
	}
	// Nothing written by the GET.
	fp, _ := gateway.LoadFramePolicy(policyRoot, "demo")
	if len(fp.Enabled) != 1 {
		t.Errorf("GET confirm changed the policy: %v", fp.Enabled)
	}

	// Observe repo: no observe offer.
	if err := (gateway.FilePolicyStore{Root: policyRoot}).SetObserve("demo", true); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.kitConfirm(w, httptest.NewRequest("GET", "/policy/kits/preview?repo=demo&name=core", nil))
	if strings.Contains(w.Body.String(), "switch demo to observe") {
		t.Error("observe offered to a repo already in observe mode")
	}
}

func TestApplyKit_observeSwitchIsExplicit(t *testing.T) {
	policyRoot, _ := previewRepoFixture(t, []string{"git/no-bypass-pre-commit"})
	h := policyHandlers{policyRoot: policyRoot, token: "tok"}

	w := httptest.NewRecorder()
	h.applyKit(w, authoringReq(t, "/policy/kits/apply", url.Values{"repo": {"demo"}, "name": {"core"}, "confirm": {"1"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("apply = %d %s", w.Code, w.Body.String())
	}
	if p, _ := (gateway.FilePolicyStore{Root: policyRoot}).Load("demo"); p.Observe {
		t.Fatal("plain apply switched the repo to observe")
	}

	w = httptest.NewRecorder()
	h.applyKit(w, authoringReq(t, "/policy/kits/apply", url.Values{"repo": {"demo"}, "name": {"web-app"}, "confirm": {"1"}, "observe": {"1"}}, "tok"))
	if w.Code != http.StatusSeeOther {
		t.Fatalf("apply+observe = %d %s", w.Code, w.Body.String())
	}
	if p, _ := (gateway.FilePolicyStore{Root: policyRoot}).Load("demo"); !p.Observe {
		t.Fatal("observe=1 did not switch the repo")
	}
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "set-observe" })
	if len(evs) != 1 {
		t.Errorf("set-observe events = %d, want 1", len(evs))
	}
}

func TestPolicyPage_previewCountsIncludeCoreAndEscapeFindings(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, []string{"git/no-bypass-pre-commit"})
	rec := &ScanRecommendation{ScannedAt: "2026-11-01T00:00:00Z", RecommendedGroups: []RecommendedGroup{{Name: "core", Always: true}}}
	next, after := policyPreviews(policyRoot, reposRoot, "demo", []string{"git/no-bypass-pre-commit"}, true, rec)
	if next == nil || after == nil {
		t.Fatal("previews not computed")
	}
	// A pushed file name is attacker-controlled: it must render escaped.
	next.Findings.Rows = append(next.Findings.Rows, previewRow{Severity: "BLOCK", Frame: kubeFrame, File: "<script>x</script>.yml", Loc: "<script>x</script>.yml:1", Label: `<img src=x onerror=alert(1)>`, CanWhitelist: true})

	vm := buildPolicyView(policyRoot, "demo", []string{"git/no-bypass-pre-commit"})
	w := httptest.NewRecorder()
	if err := renderPolicyPage(w, vm, policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}, PolicyRoot: policyRoot, ScanRec: rec, NextPush: next, ScanRecAfter: after}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !regexp.MustCompile(`With the recommendation applied, the next push would: <b>block [1-9]`).MatchString(body) {
		t.Errorf("banner must count Core's blocks; got:\n%s", body)
	}
	if strings.Contains(body, "would flag") {
		t.Error("the old per-group would-flag counts are still rendered")
	}
	if !strings.Contains(body, "With this policy, the next push would:") || !strings.Contains(body, `href="/policy/repo/scan-preview"`) && !strings.Contains(body, `action="/policy/repo/scan-preview"`) {
		t.Error("preview section or confirm link missing")
	}
	if strings.Contains(body, "<script>x</script>") || strings.Contains(body, "<img src=x") {
		t.Error("finding fields rendered unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;x&lt;/script&gt;.yml") {
		t.Error("escaped file name missing")
	}
}

func TestPolicyPage_previewLoadsAsync(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, []string{kubeFrame})
	rec := &ScanRecommendation{ScannedAt: "2026-11-01T00:00:00Z", RecommendedGroups: []RecommendedGroup{{Name: "core", Always: true}}}

	vm := buildPolicyView(policyRoot, "demo", []string{kubeFrame})
	w := httptest.NewRecorder()
	if err := renderPolicyPage(w, vm, policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}, PolicyRoot: policyRoot, ScanRec: rec, NextPush: pendingNextPush("demo", true)}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, `hx-get="/policy/preview/next?repo=demo"`) || !strings.Contains(body, `hx-trigger="load"`) {
		t.Errorf("page must render a self-loading preview placeholder; got:\n%s", body)
	}
	if strings.Contains(body, "With this policy, the next push would:") {
		t.Error("page rendered computed counts instead of the placeholder")
	}
	if !strings.Contains(body, `id="gw-scan-preview"`) {
		t.Error("banner placeholder missing")
	}

	recJSON, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(policyRoot, "demo", "scan-recommendation.json"), recJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	h := previewHandlers{policyRoot: policyRoot, reposRoot: reposRoot, token: "tok", allowEdits: true}
	w = httptest.NewRecorder()
	h.previewNext(w, httptest.NewRequest(http.MethodGet, "/policy/preview/next?repo=demo", nil))
	body = w.Body.String()
	if !regexp.MustCompile(`With this policy, the next push would: <b>block [1-9]`).MatchString(body) {
		t.Errorf("fill must carry the computed block count; got:\n%s", body)
	}
	if !strings.Contains(body, `id="gw-scan-preview" hx-swap-oob="outerHTML"`) || !strings.Contains(body, "With the recommendation applied") {
		t.Errorf("fill must update the banner out of band; got:\n%s", body)
	}

	w = httptest.NewRecorder()
	h.previewNext(w, httptest.NewRequest(http.MethodGet, "/policy/preview/next?repo=../x", nil))
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad repo name = %d, want 400", w.Code)
	}
}

func TestToggleFrame_previewRefreshIsAsync(t *testing.T) {
	policyRoot, reposRoot := previewRepoFixture(t, []string{kubeFrame})
	h := policyHandlers{policyRoot: policyRoot, token: "tok", reposRoot: reposRoot}
	w := httptest.NewRecorder()
	h.toggleFrame(w, authoringReq(t, "/policy/frames/toggle", url.Values{"repo": {"demo"}, "id": {"git/no-bypass-pre-commit"}, "enabled": {"1"}}, "tok"))
	body := w.Body.String()
	if !strings.Contains(body, `id="gw-next-push" hx-swap-oob="outerHTML" hx-get="/policy/preview/next?repo=demo"`) {
		t.Errorf("toggle must swap in a self-loading placeholder; got:\n%s", body)
	}
	if strings.Contains(body, "the next push would:") {
		t.Error("toggle computed the preview synchronously")
	}
}

// A dashboard without --allow-edits (and the static demo built from one) has
// no new-form or kit-preview route: its buttons must not lead there.
func TestPolicyPage_readOnlyDisablesEditOnlyKitControls(t *testing.T) {
	policyRoot, _ := previewRepoFixture(t, []string{"git/no-bypass-pre-commit"})
	vm := buildPolicyView(policyRoot, "demo", []string{"git/no-bypass-pre-commit"})
	for _, edits := range []bool{false, true} {
		w := httptest.NewRecorder()
		if err := renderPolicyPage(w, vm, policyPageOpts{AllowEdits: edits, CSRFToken: "tok", Repos: []string{"demo"}, PolicyRoot: policyRoot}); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		hasForm := strings.Contains(body, `hx-get="/policy/kits/new-form"`)
		live := regexp.MustCompile(`action="/policy/kits/preview"[^>]*>(<input[^>]*>)*<button type="submit" class="gw-kit-apply">`).MatchString(body)
		if edits != hasForm || edits != live {
			t.Errorf("allow-edits=%v: new-form link=%v, enabled kit apply=%v", edits, hasForm, live)
		}
	}
}
