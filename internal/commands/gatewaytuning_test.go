// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway"
	"nimblegate/internal/whitelist"
)

func TestCSRFGuard(t *testing.T) {
	const tok = "secret-token"
	mk := func(setHdr func(h http.Header)) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/policy/severity", nil)
		setHdr(r.Header)
		return r
	}
	if !csrfOK(mk(func(h http.Header) { h.Set("X-CSRF-Token", tok); h.Set("Sec-Fetch-Site", "same-origin") }), tok) {
		t.Error("valid token + same-origin should pass")
	}
	if csrfOK(mk(func(h http.Header) { h.Set("Sec-Fetch-Site", "same-origin") }), tok) {
		t.Error("missing token must fail")
	}
	if csrfOK(mk(func(h http.Header) { h.Set("X-CSRF-Token", "nope"); h.Set("Sec-Fetch-Site", "same-origin") }), tok) {
		t.Error("wrong token must fail")
	}
	if csrfOK(mk(func(h http.Header) { h.Set("X-CSRF-Token", tok); h.Set("Sec-Fetch-Site", "cross-site") }), tok) {
		t.Error("cross-site must fail")
	}
	if csrfOK(mk(func(h http.Header) { h.Set("X-CSRF-Token", tok); h.Set("Sec-Fetch-Site", "same-site") }), tok) {
		t.Error("same-site must fail")
	}
	if !csrfOK(mk(func(h http.Header) { h.Set("X-CSRF-Token", tok) }), tok) {
		t.Error("token present + no Sec-Fetch-Site should pass")
	}
}

func TestBuildPolicyView(t *testing.T) {
	root := t.TempDir()
	enabled := []string{"security/no-private-keys-in-repo", "git/no-bypass-pre-commit"}
	vm := buildPolicyView(root, "demo", enabled)
	if vm.Repo != "demo" {
		t.Errorf("repo wrong: %+v", vm)
	}
	if len(vm.Enabled) != 2 {
		t.Errorf("Enabled = %v, want 2 frame IDs", vm.Enabled)
	}
	if len(vm.Categories) == 0 {
		t.Error("no categories loaded")
	}
}

func TestRenderPolicyPage(t *testing.T) {
	// Verifies the page renders the repo picker and frame selection, but no longer
	// includes the add-repo form (that moved to /repos).
	enabled := []string{"security/no-private-keys-in-repo", "git/no-bypass-pre-commit"}
	vm := buildPolicyView(t.TempDir(), "demo", enabled)
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}})
	b := rec.Body.String()
	for _, want := range []string{"demo", "security/no-private-keys-in-repo", "Manage repos"} {
		if !strings.Contains(b, want) {
			t.Errorf("policy page missing %q", want)
		}
	}
	if strings.Contains(b, "/policy/repo/add") {
		t.Error("add-repo form must not appear on /policy (it moved to /repos)")
	}
}

func postReq(path string, form url.Values, token, site string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		r.Header.Set("X-CSRF-Token", token)
	}
	if site != "" {
		r.Header.Set("Sec-Fetch-Site", site)
	}
	return r
}

func TestPostSeverity(t *testing.T) {
	root := t.TempDir()
	_ = gateway.FramePolicy{Enabled: []string{"security/no-private-keys-in-repo", "git/no-bypass-pre-commit"}}.Save(root, "demo")
	_ = (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", Enabled: true})
	h := policyHandlers{policyRoot: root, token: "tok"}
	rec := httptest.NewRecorder()
	h.severity(rec, postReq("/policy/severity", url.Values{"repo": {"demo"}, "frame": {"security/no-private-keys-in-repo"}, "severity": {"WARN"}}, "tok", "same-origin"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "WARN") {
		t.Errorf("valid severity POST: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if fp, _ := gateway.LoadFramePolicy(root, "demo"); fp.Severity["security/no-private-keys-in-repo"] != "WARN" {
		t.Error("severity not persisted")
	}
	if evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "frame-severity" }); len(evs) != 1 || evs[0].Repo != "demo" {
		t.Errorf("frame-severity event: %+v", evs)
	}
	rec = httptest.NewRecorder()
	h.severity(rec, postReq("/policy/severity", url.Values{"repo": {"demo"}, "frame": {"x"}, "severity": {"WARN"}}, "", "same-origin"))
	if rec.Code != 403 {
		t.Errorf("missing token want 403, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.severity(rec, postReq("/policy/severity", url.Values{"repo": {"demo"}, "frame": {"security/no-private-keys-in-repo"}, "severity": {"LOUD"}}, "tok", "same-origin"))
	if rec.Code != 400 {
		t.Errorf("invalid severity want 400, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.severity(rec, postReq("/policy/severity", url.Values{"repo": {"ghost"}, "frame": {"security/no-private-keys-in-repo"}, "severity": {"WARN"}}, "tok", "same-origin"))
	if rec.Code != 400 {
		t.Errorf("unregistered repo want 400, got %d", rec.Code)
	}
}

// renderPolicyBody is a test helper: seeds a single repo and renders the page with
// the supplied opts. Returns the page body for substring assertions.
func renderPolicyBody(t *testing.T, root, repo string, opts policyPageOpts) string {
	t.Helper()
	enabled := []string{"git/no-bypass-pre-commit", "security/no-private-keys-in-repo"}
	_ = gateway.FramePolicy{Enabled: enabled}.Save(root, repo)
	_ = (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: repo, UpstreamURL: "u", Enabled: true})
	vm := buildPolicyView(root, repo, enabled)
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, opts)
	return rec.Body.String()
}

// TestPolicyPage_groupToggles* removed: group-toggle markup lived in the old
// renderer and is not present in the T9 stub. T10 introduces the new category-tree
// markup and will add replacement tests for that shape.

func TestBuildGroupToggleSets_classifiesByAxis(t *testing.T) {
	got := buildGroupToggleSets([]string{"@tier-1", "@web", "@custom-extra"})
	if len(got.Coverage) != len(coverageGroups) {
		t.Fatalf("Coverage len: %d want %d", len(got.Coverage), len(coverageGroups))
	}
	if got.Coverage[0].Name != "@tier-1" || !got.Coverage[0].Checked {
		t.Fatalf("@tier-1 should be checked in Coverage: %+v", got.Coverage[0])
	}
	if len(got.Stack) != len(stackGroups) {
		t.Fatalf("Stack len: %d want %d", len(got.Stack), len(stackGroups))
	}
	var webChecked bool
	for _, gt := range got.Stack {
		if gt.Name == "@web" {
			webChecked = gt.Checked
		}
	}
	if !webChecked {
		t.Fatal("@web should be checked in Stack")
	}
	if len(got.Other) != 1 || got.Other[0].Name != "@custom-extra" || !got.Other[0].Checked {
		t.Fatalf("@custom-extra should land in Other (checked): %+v", got.Other)
	}
}

func TestPolicyPage_addRepoFormPresent(t *testing.T) {
	// Add-repo form moved to /repos. /policy must not contain it.
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}})
	// The add form moved to /repos; assert its action is absent. (The Edit-repo-
	// settings form legitimately has its own upstream field, so check the add
	// action specifically rather than name="upstream".)
	if strings.Contains(body, "/policy/repo/add") {
		t.Error("/policy must not contain /policy/repo/add action (form moved to /repos)")
	}
}

func TestPolicyPage_addRepoFormHiddenWithoutAllowEdits(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: false, Repos: []string{"demo"}})
	if strings.Contains(body, "/policy/repo/add") {
		t.Error("add-repo form must be hidden when allowEdits=false")
	}
}

func TestPolicyPage_repoPicker(t *testing.T) {
	root := t.TempDir()
	// Compact repo picker replaces the old edit-repo form.
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo", "other"}})
	for _, want := range []string{
		`<select name="repo"`,
		`value="demo"`,
		`value="other"`,
		"Manage repos",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("repo picker missing %q", want)
		}
	}
	// Old edit form is gone.
	if strings.Contains(body, "Edit existing repo in gateway") {
		t.Error("/policy must not contain the old edit-repo form heading")
	}
	// Without repos, shows a link to /repos.
	root2 := t.TempDir()
	vm2 := buildPolicyView(root2, "", nil)
	rec2 := httptest.NewRecorder()
	renderPolicyPage(rec2, vm2, policyPageOpts{AllowEdits: true, Repos: nil})
	if !strings.Contains(rec2.Body.String(), "/repos") {
		t.Error("policy page with no repos should link to /repos")
	}
}

func TestPolicyPage_frameSelectionGatedOnRepo(t *testing.T) {
	root := t.TempDir()
	// No repo selected: placeholder shown, no checkbox tree.
	vm := buildPolicyView(root, "", nil)
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, policyPageOpts{AllowEdits: true, Repos: []string{"demo"}})
	body := rec.Body.String()
	if !strings.Contains(body, "gw-policy-empty") {
		t.Error("expected empty-state placeholder when no repo selected")
	}
	if strings.Contains(body, `hx-post="/policy/frames/toggle"`) {
		t.Error("frame toggle should not appear when no repo is selected")
	}
}

func TestPolicyPage_scanBannerWhenRecExistsAndNotDismissed(t *testing.T) {
	root := t.TempDir()
	rec := &ScanRecommendation{
		ScannedAt:         "2026-05-30T12:00:00Z",
		TreeRef:           "HEAD",
		RecommendedGroups: []RecommendedGroup{{Name: "@web", Always: false, WouldFlag: 3}},
		Dismissed:         false,
	}
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}, ScanRec: rec})
	for _, want := range []string{"Scan recommendation", "@web", "/policy/repo/scan-apply", "/policy/repo/scan-dismiss", "/policy/repo/scan-rescan"} {
		if !strings.Contains(body, want) {
			t.Errorf("scan banner missing %q", want)
		}
	}
}

func TestPolicyPage_scanBannerHiddenWhenDismissed(t *testing.T) {
	root := t.TempDir()
	// Dismissed via the loader: write rec with dismissed:true, call loadScanRec.
	if err := os.MkdirAll(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{
		"scanned_at":         "2026-05-30T12:00:00Z",
		"tree_ref":           "HEAD",
		"recommended_groups": []any{map[string]any{"name": "@web"}},
		"dismissed":          true,
	})
	if err := os.WriteFile(filepath.Join(root, "demo", "scan-recommendation.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := loadScanRec(root, "demo")
	if got != nil {
		t.Fatalf("loadScanRec must return nil when dismissed=true; got %+v", got)
	}
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}, ScanRec: got})
	if strings.Contains(body, "/policy/repo/scan-apply") {
		t.Error("scan banner must be hidden when rec dismissed")
	}
}

func TestPolicyPage_NoMoreArchiveSection(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}})
	if strings.Contains(body, "/policy/repo/archive") {
		t.Error("archive section must not appear on /policy (it moved to /repos)")
	}
}

func TestPolicyPage_archivedPanelMovedToRepos(t *testing.T) {
	// The archived panel moved to /repos. /policy no longer renders it.
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "stale", UpstreamURL: "u", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{
		Name: "stale", PolicyRoot: policyRoot, ReposRoot: reposRoot,
	}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "active", UpstreamURL: "u", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	archived := gateway.ListArchivedRepos(policyRoot)
	if len(archived) != 1 || archived[0] != "stale" {
		t.Fatalf("ListArchivedRepos: got %v want [stale]", archived)
	}
	vm := buildPolicyView(policyRoot, "active", nil)
	w := httptest.NewRecorder()
	renderPolicyPage(w, vm, policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"active"}, Archived: archived})
	// Archived panel is now at /repos - policy page must not contain restore controls.
	if strings.Contains(w.Body.String(), "/policy/repo/restore") {
		t.Error("/policy must not render the restore form (archived panel moved to /repos)")
	}
}

func TestPolicyPage_NoLongerHasAddForm(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}})
	// The add form (action /policy/repo/add) moved to /repos. The Edit-repo-
	// settings form on /policy has its own upstream field, so assert the add
	// action's absence rather than name="upstream".
	if strings.Contains(body, "/policy/repo/add") {
		t.Error("/policy must not contain the add-repo form action")
	}
}

func TestPolicyPage_PickerHasManageOption(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{AllowEdits: true, CSRFToken: "tok", Repos: []string{"demo"}})
	if !strings.Contains(body, "Manage repos") {
		t.Error("/policy repo picker missing 'Manage repos' option")
	}
}

func TestPolicyPage_JustRegisteredNoticeAppears(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "myrepo", policyPageOpts{
		AllowEdits:     true,
		CSRFToken:      "tok",
		Repos:          []string{"myrepo"},
		JustRegistered: "myrepo",
	})
	if !strings.Contains(body, "myrepo") {
		t.Error("just-registered banner should contain the repo name")
	}
	if !strings.Contains(body, "gw-justregistered") {
		t.Error("just-registered banner class missing")
	}
	// Banner must not appear when JustRegistered is empty.
	body2 := renderPolicyBody(t, root, "myrepo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"myrepo"},
	})
	if strings.Contains(body2, "gw-justregistered") {
		t.Error("just-registered banner must not appear when JustRegistered is empty")
	}
}

func TestPostRepoToggle(t *testing.T) {
	root := t.TempDir()
	_ = (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", Enabled: true})
	h := policyHandlers{policyRoot: root, token: "tok"}
	rec := httptest.NewRecorder()
	h.repo(rec, postReq("/policy/repo", url.Values{"repo": {"demo"}, "enabled": {"0"}}, "tok", "same-origin"))
	if rec.Code != 200 {
		t.Fatalf("repo toggle code=%d body=%s", rec.Code, rec.Body.String())
	}
	if p, _ := (gateway.FilePolicyStore{Root: root}).Load("demo"); p.Enabled {
		t.Error("repo not disabled")
	}
	if evs, _ := gateway.ReadEvents(root, func(e gateway.Event) bool { return e.Event == "repo-toggle" }); len(evs) != 1 || evs[0].Repo != "demo" {
		t.Errorf("repo-toggle event: %+v", evs)
	}

	rec = httptest.NewRecorder()
	h.repo(rec, postReq("/policy/repo", url.Values{"repo": {"demo"}, "enabled": {"0"}}, "", "same-origin"))
	if rec.Code != 403 {
		t.Errorf("repo toggle missing token want 403, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.repo(rec, postReq("/policy/repo", url.Values{"repo": {"ghost"}, "enabled": {"0"}}, "tok", "same-origin"))
	if rec.Code != 400 {
		t.Errorf("repo toggle unknown repo want 400, got %d", rec.Code)
	}
}

func TestBuildPolicyView_GroupsByCategory(t *testing.T) {
	enabled := []string{
		"security/no-hardcoded-credentials",
		"git/folder-branch-lock",
	}
	tmp := t.TempDir()
	writeTestPolicyConfig(t, tmp, enabled, nil)
	vm := buildPolicyView(tmp, "you/myapp", enabled)

	// v2 axis grouping: top-level Categories are the four v2 axes, NOT v1
	// categories. Sub-buckets inside Domain still use v1 category names
	// (security, network, etc) as their Name, but Display can be overridden.
	gotAxes := map[string]bool{}
	for _, c := range vm.Categories {
		gotAxes[c.ID] = true
	}
	for _, want := range []string{"core", "framework", "domain"} {
		if !gotAxes[want] {
			t.Errorf("expected v2 axis %q in view; got: %v", want, gotAxes)
		}
	}

	// Under v2 single-axis classification, cf-pages-headers-baseline
	// (platform: [cloudflare, cf-pages]) lives EXCLUSIVELY under
	// Platform > Cloudflare > Cf-Pages - no more cross-listing under
	// Domain > Security. Verify the single placement.
	var seenAt []string
	for _, c := range vm.Categories {
		for _, sub := range c.Subcategories {
			for _, fr := range sub.Frames {
				if fr.ID == "security/cf-pages-headers-baseline" {
					seenAt = append(seenAt, c.ID+"/"+sub.Name)
				}
			}
			for _, child := range sub.Children {
				for _, fr := range child.Frames {
					if fr.ID == "security/cf-pages-headers-baseline" {
						seenAt = append(seenAt, c.ID+"/"+sub.Name+"/"+child.Name)
					}
				}
			}
		}
	}
	if len(seenAt) != 1 || seenAt[0] != "platform/cloudflare/cf-pages" {
		t.Errorf("cf-pages-headers-baseline classification = %v, want exactly [platform/cloudflare/cf-pages]", seenAt)
	}
}

// TestBuildPolicyView_FrameworkAlwaysVisible: under v2 the Framework axis
// is always shown so operators can see the axis exists, even when no
// frames currently declare framework (today every frame has framework=[]).
func TestBuildPolicyView_FrameworkAlwaysVisible(t *testing.T) {
	tmp := t.TempDir()
	writeTestPolicyConfig(t, tmp, nil, nil)
	vm := buildPolicyView(tmp, "you/myapp", nil)
	found := false
	for _, c := range vm.Categories {
		if c.ID == "framework" {
			found = true
			break
		}
	}
	if !found {
		t.Error("framework axis missing: should always render, even empty")
	}
}

// writeTestPolicyConfig creates a minimal .appframes/ + config skeleton in tmp.
func writeTestPolicyConfig(t *testing.T, tmp string, enabled, _ []string) {
	t.Helper()
	// The new buildPolicyView(root, repo, enabled) takes enabled as a direct
	// parameter and does not read from disk; this helper is a no-op placeholder
	// kept for test clarity and potential future disk-reading variants.
	_ = tmp
	_ = enabled
}

func TestRenderPolicyPage_HasKitChipsRow(t *testing.T) {
	var buf bytes.Buffer
	// Pills only render when the applied kit has at least one frame ticked,
	// so the fixture must include enabled frames that belong to core.
	vm := policyVM{
		Repo: "you/myapp",
		Enabled: []string{
			"git/folder-branch-lock",
			"security/no-hardcoded-credentials",
		},
		AppliedKits: []string{"core"},
		BuiltinKits: []policyKitInfo{
			{Name: "core", Display: "Core", FrameCount: 15, FullyApplied: false},
			{Name: "web-app", Display: "Web app", FrameCount: 27, FullyApplied: false},
		},
	}
	if err := renderPolicyPage(&buf, vm, policyPageOpts{AllowEdits: true}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`id="gw-policy-kits-row"`,
		`data-applied-kit="core"`,
		`Apply Web app`,
		`hx-post="/policy/kits/apply"`,
		`gw-kit-count`, // count badge inside each pill
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in rendered HTML", want)
		}
	}
}

func TestRenderPolicyPage_HasGroupsRow(t *testing.T) {
	var buf bytes.Buffer
	// Use a real buildPolicyView so Categories is populated from stdlib.
	root := t.TempDir()
	enabled := []string{
		"git/folder-branch-lock",
		"security/no-hardcoded-credentials",
	}
	vm := buildPolicyView(root, "you/myapp", enabled)
	vm.AppliedKits = []string{"core"}
	vm.BuiltinKits = []policyKitInfo{
		{Name: "core", Display: "Core", FrameCount: 15, FullyApplied: false},
	}
	if err := renderPolicyPage(&buf, vm, policyPageOpts{AllowEdits: true}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`id="gw-policy-groups-row"`,
		`data-applied-category=`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in rendered HTML", want)
		}
	}
}

func TestRenderPolicyPage_BrowseTreeHasCheckboxes(t *testing.T) {
	var buf bytes.Buffer
	vm := policyVM{
		Repo: "you/myapp",
		Categories: []policyCategory{
			{ID: "security", Display: "Security", Subcategories: []policySubcategory{
				{Name: "credentials", Display: "Credentials", Frames: []policyFrameRef{
					{ID: "security/no-hardcoded-credentials", Enabled: true, Severity: "BLOCK", Tier: 1},
				}},
			}},
		},
	}
	if err := renderPolicyPage(&buf, vm, policyPageOpts{AllowEdits: true}); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	for _, want := range []string{
		`>Security<`,
		`>Credentials<`,
		`security/no-hardcoded-credentials`,
		`hx-post="/policy/frames/toggle"`,
		`type="checkbox"`,
		`checked`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in rendered HTML", want)
		}
	}
}

// writeMinConfig writes a minimal appframes.toml into tmp/<testRepo>/ with the
// given enabled frames and applied kit names. All handler tests use testRepo as
// the repo form value so handler writes land in the same path.
const testRepo = "testrepo"

func writeMinConfig(t *testing.T, tmp string, enabled, applied []string) {
	t.Helper()
	dir := filepath.Join(tmp, testRepo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("[frames]\nenabled = [\n")
	for _, id := range enabled {
		fmt.Fprintf(&b, "    %q,\n", id)
	}
	b.WriteString("]\n\n[ui]\napplied_kits = [")
	for i, name := range applied {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", name)
	}
	b.WriteString("]\n")
	if err := os.WriteFile(filepath.Join(dir, "appframes.toml"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readEnabledList(t *testing.T, tmp string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tmp, testRepo, "appframes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Frames struct {
			Enabled []string `toml:"enabled"`
		} `toml:"frames"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Frames.Enabled
}

func readAppliedKitsList(t *testing.T, tmp string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(tmp, testRepo, "appframes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		UI struct {
			AppliedKits []string `toml:"applied_kits"`
		} `toml:"ui"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.UI.AppliedKits
}

func TestToggleFrameAddsToEnabled(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, []string{"git/folder-branch-lock"}, []string{"core"})

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&id=security/no-mixed-content-urls`)
	req := httptest.NewRequest("POST", "/policy/frames/toggle", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.toggleFrame(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	enabled := readEnabledList(t, tmp)
	if !containsStr(enabled, "security/no-mixed-content-urls") {
		t.Errorf("expected toggle to add frame; enabled=%v", enabled)
	}
	// Original frame must still be present.
	if !containsStr(enabled, "git/folder-branch-lock") {
		t.Errorf("pre-existing frame removed unexpectedly; enabled=%v", enabled)
	}
}

func TestToggleFrameRemovesFromEnabled(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, []string{"git/folder-branch-lock", "security/no-mixed-content-urls"}, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&id=security/no-mixed-content-urls`)
	req := httptest.NewRequest("POST", "/policy/frames/toggle", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.toggleFrame(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	enabled := readEnabledList(t, tmp)
	if containsStr(enabled, "security/no-mixed-content-urls") {
		t.Errorf("expected toggle to remove frame; enabled=%v", enabled)
	}
}

func TestToggleFrameCSRFGuard(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&id=security/no-mixed-content-urls`)
	req := httptest.NewRequest("POST", "/policy/frames/toggle", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No CSRF token - should get 403.
	w := httptest.NewRecorder()
	h.toggleFrame(w, req)
	if w.Code != 403 {
		t.Errorf("want 403 without CSRF token, got %d", w.Code)
	}
}

func TestApplyKitWritesFrames(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&name=core`)
	req := httptest.NewRequest("POST", "/policy/kits/apply", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.applyKit(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	enabled := readEnabledList(t, tmp)
	if len(enabled) != 15 {
		t.Errorf("core apply: got %d enabled, want 15", len(enabled))
	}
	applied := readAppliedKitsList(t, tmp)
	if len(applied) != 1 || applied[0] != "core" {
		t.Errorf("applied_kits = %v, want [core]", applied)
	}
}

func TestApplyKitCSRFGuard(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&name=core`)
	req := httptest.NewRequest("POST", "/policy/kits/apply", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.applyKit(w, req)
	if w.Code != 403 {
		t.Errorf("want 403 without CSRF token, got %d", w.Code)
	}
}

func TestApplyKitUnknown(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&name=no-such-kit`)
	req := httptest.NewRequest("POST", "/policy/kits/apply", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.applyKit(w, req)
	if w.Code != 400 {
		t.Errorf("want 400 for unknown kit, got %d", w.Code)
	}
}

func TestClearKitRemovesFrames(t *testing.T) {
	tmp := t.TempDir()
	corp := []string{
		"git/folder-branch-lock", "git/no-amend-pushed-commits",
		"git/no-bypass-pre-commit", "git/no-force-push-main",
		"commands/apt-purge-preview", "commands/curl-pipe-shell",
		"database/migration-script-explicit-env",
		"database/migration-verification-step",
		"database/sqlite-migration-idempotent-wrapper",
		"filesystem/rm-rf-protected-paths",
		"security/no-hardcoded-credentials", "security/no-private-keys-in-repo",
		"network/no-localhost-in-proxy-config",
		"database/schema-vs-code-drift", "app-correctness/dynamic-env-declared",
	}
	writeMinConfig(t, tmp, corp, []string{"core"})

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&name=core`)
	req := httptest.NewRequest("POST", "/policy/kits/clear", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.clearKit(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	enabled := readEnabledList(t, tmp)
	if len(enabled) != 0 {
		t.Errorf("clear core: got %d enabled, want 0", len(enabled))
	}
	applied := readAppliedKitsList(t, tmp)
	if len(applied) != 0 {
		t.Errorf("applied_kits = %v, want []", applied)
	}
}

func TestClearKitCSRFGuard(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&name=core`)
	req := httptest.NewRequest("POST", "/policy/kits/clear", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.clearKit(w, req)
	if w.Code != 403 {
		t.Errorf("want 403 without CSRF token, got %d", w.Code)
	}
}

func TestUserKitForm_RendersFramePicker(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	h := policyHandlers{policyRoot: tmp, token: "tok"}

	req := httptest.NewRequest("GET", "/policy/kits/new-form?repo="+testRepo, nil)
	w := httptest.NewRecorder()
	h.userKitForm(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`name="name"`,
		`name="frames"`,
		`security/no-hardcoded-credentials`,
		`hx-post="/policy/kits/create"`,
		`name="repo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("form missing %q", want)
		}
	}
}

func TestCreateUserKitEndpoint(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	h := policyHandlers{policyRoot: tmp, token: "tok"}

	form := url.Values{
		"repo":   {testRepo},
		"name":   {"MVP gate"},
		"frames": {"security/no-hardcoded-credentials", "git/folder-branch-lock"},
	}
	req := httptest.NewRequest("POST", "/policy/kits/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.createUserKit(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	uks := readUserKits(tmp, testRepo, nil)
	if len(uks) != 1 || uks[0].Name != "MVP gate" {
		t.Errorf("user kit not persisted: %v", uks)
	}
}

func TestCreateUserKitEndpoint_CSRFRejected(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	h := policyHandlers{policyRoot: tmp, token: "tok"}

	form := url.Values{"repo": {testRepo}, "name": {"x"}, "frames": {"git/folder-branch-lock"}}
	req := httptest.NewRequest("POST", "/policy/kits/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No CSRF header
	w := httptest.NewRecorder()
	h.createUserKit(w, req)
	if w.Code != 403 {
		t.Errorf("expected 403 csrf rejection, got %d", w.Code)
	}
}

func TestDeleteUserKitEndpoint(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	if err := addUserKit(filepath.Join(tmp, testRepo, "appframes.toml"), "MVP", []string{
		"security/no-hardcoded-credentials",
	}); err != nil {
		t.Fatal(err)
	}
	h := policyHandlers{policyRoot: tmp, token: "tok"}

	form := url.Values{"repo": {testRepo}, "name": {"MVP"}}
	req := httptest.NewRequest("POST", "/policy/kits/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.deleteUserKitHandler(w, req)
	if w.Code != 200 {
		t.Fatalf("status=%d", w.Code)
	}
	uks := readUserKits(tmp, testRepo, nil)
	if len(uks) != 0 {
		t.Errorf("expected 0 user kits after delete, got %d", len(uks))
	}
}

// readEnabled is a test helper that reads the enabled frames from appframes.toml.
func readEnabled(t *testing.T, tmp string) []string {
	t.Helper()
	return readEnabledList(t, tmp)
}

func TestUserKit_AddAndRead(t *testing.T) {
	tmp := t.TempDir()
	// Write directly into the per-repo dir so readUserKits finds it.
	dir := filepath.Join(tmp, testRepo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "appframes.toml")
	if err := os.WriteFile(cfg, []byte("[frames]\nenabled = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := addUserKit(cfg, "MVP gate", []string{
		"security/no-hardcoded-credentials",
		"git/folder-branch-lock",
	}); err != nil {
		t.Fatal(err)
	}
	uks := readUserKits(tmp, testRepo, map[string]bool{
		"security/no-hardcoded-credentials": true,
	})
	if len(uks) != 1 {
		t.Fatalf("expected 1 user kit, got %d", len(uks))
	}
	if uks[0].Name != "MVP gate" {
		t.Errorf("got name %q", uks[0].Name)
	}
	if len(uks[0].Frames) != 2 {
		t.Errorf("got %d frames, want 2", len(uks[0].Frames))
	}
}

func TestUserKit_AddDuplicateNameRejected(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	if err := addUserKit(cfg, "MVP", []string{"security/no-hardcoded-credentials"}); err != nil {
		t.Fatal(err)
	}
	if err := addUserKit(cfg, "MVP", []string{"git/folder-branch-lock"}); err == nil {
		t.Error("expected error adding duplicate kit name; got nil")
	}
}

func TestUserKit_AddAlsoTicksFrames(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	if err := addUserKit(cfg, "MVP", []string{
		"security/no-hardcoded-credentials",
		"git/folder-branch-lock",
	}); err != nil {
		t.Fatal(err)
	}
	enabled := readEnabled(t, tmp)
	if !containsStr(enabled, "security/no-hardcoded-credentials") {
		t.Error("expected addUserKit to also tick security/no-hardcoded-credentials")
	}
	if !containsStr(enabled, "git/folder-branch-lock") {
		t.Error("expected addUserKit to also tick git/folder-branch-lock")
	}
}

func TestUserKit_Delete(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	if err := addUserKit(cfg, "MVP gate", []string{"security/no-hardcoded-credentials"}); err != nil {
		t.Fatal(err)
	}
	if err := deleteUserKit(cfg, "MVP gate"); err != nil {
		t.Fatal(err)
	}
	uks := readUserKits(tmp, testRepo, nil)
	if len(uks) != 0 {
		t.Errorf("expected 0 user kits after delete, got %d", len(uks))
	}
}

func TestUserKit_DeleteDoesNotUntickFrames(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	if err := addUserKit(cfg, "MVP", []string{"security/no-hardcoded-credentials"}); err != nil {
		t.Fatal(err)
	}
	if err := deleteUserKit(cfg, "MVP"); err != nil {
		t.Fatal(err)
	}
	enabled := readEnabled(t, tmp)
	if !containsStr(enabled, "security/no-hardcoded-credentials") {
		t.Error("deleteUserKit should NOT untick frames; security/no-hardcoded-credentials should stay enabled")
	}
}

func TestClearCustomKit_UntickAndDelete(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	// Add a custom kit with two frames, plus an extra frame outside the kit.
	if err := addUserKit(cfg, "MVP", []string{
		"security/no-hardcoded-credentials",
		"git/folder-branch-lock",
	}); err != nil {
		t.Fatal(err)
	}
	// Manually add an extra enabled frame that should survive the clear.
	data, _ := os.ReadFile(cfg)
	updated, _, err := rewriteEnabledList(string(data), "git/no-bypass-pre-commit", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(cfg, []byte(updated)); err != nil {
		t.Fatal(err)
	}

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	form := url.Values{"repo": {testRepo}, "name": {"MVP"}}
	req := httptest.NewRequest("POST", "/policy/userkits/clear", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.clearCustomKit(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	// Kit entry must be gone.
	uks := readUserKits(tmp, testRepo, nil)
	if len(uks) != 0 {
		t.Errorf("expected 0 user kits after clearCustomKit, got %d", len(uks))
	}
	// Kit frames must be unticked.
	enabled := readEnabled(t, tmp)
	if containsStr(enabled, "security/no-hardcoded-credentials") {
		t.Error("security/no-hardcoded-credentials should be unticked after clearCustomKit")
	}
	if containsStr(enabled, "git/folder-branch-lock") {
		t.Error("git/folder-branch-lock should be unticked after clearCustomKit")
	}
	// Frame outside the kit must remain.
	if !containsStr(enabled, "git/no-bypass-pre-commit") {
		t.Error("git/no-bypass-pre-commit must stay enabled after clearCustomKit")
	}
}

func TestClearCategory_UntickFramesInCategory(t *testing.T) {
	tmp := t.TempDir()
	// Enable frames from two categories: security and git.
	writeMinConfig(t, tmp, []string{
		"security/no-hardcoded-credentials",
		"security/no-private-keys-in-repo",
		"git/folder-branch-lock",
		"git/no-bypass-pre-commit",
	}, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	form := url.Values{"repo": {testRepo}, "category": {"security"}}
	req := httptest.NewRequest("POST", "/policy/category/clear", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.clearCategory(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	enabled := readEnabled(t, tmp)
	// Security frames must be gone.
	if containsStr(enabled, "security/no-hardcoded-credentials") {
		t.Error("security/no-hardcoded-credentials should be unticked after clearCategory(security)")
	}
	if containsStr(enabled, "security/no-private-keys-in-repo") {
		t.Error("security/no-private-keys-in-repo should be unticked after clearCategory(security)")
	}
	// Git frames must remain.
	if !containsStr(enabled, "git/folder-branch-lock") {
		t.Error("git/folder-branch-lock must stay enabled after clearCategory(security)")
	}
	if !containsStr(enabled, "git/no-bypass-pre-commit") {
		t.Error("git/no-bypass-pre-commit must stay enabled after clearCategory(security)")
	}
}

func TestToggleFrame_EmitsOOBSwap(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, []string{"git/folder-branch-lock"}, nil)

	h := policyHandlers{policyRoot: tmp, token: "tok"}
	body := strings.NewReader(`repo=` + testRepo + `&id=security/no-hardcoded-credentials`)
	req := httptest.NewRequest("POST", "/policy/frames/toggle", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.toggleFrame(w, req)

	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	resp := w.Body.String()
	if !strings.Contains(resp, `hx-swap-oob="outerHTML"`) {
		t.Error("toggleFrame response must contain hx-swap-oob=\"outerHTML\"")
	}
	if !strings.Contains(resp, `id="gw-policy-kits-row"`) {
		t.Error("toggleFrame response must contain div with id=\"gw-policy-kits-row\"")
	}
	if !strings.Contains(resp, `id="gw-policy-groups-row"`) {
		t.Error("toggleFrame response must contain div with id=\"gw-policy-groups-row\"")
	}
	if !strings.Contains(resp, `id="gw-policy-total-row"`) {
		t.Error("toggleFrame response must contain div with id=\"gw-policy-total-row\"")
	}
}

func TestPolicyPage_CustomLintersHasSectionHead(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		ActiveTab:  "linters",
		Authoring:  `<p>my-check</p>`,
	})
	if !strings.Contains(body, `<h3 class="gw-section-head">Custom linters</h3>`) {
		t.Error("Custom linters must have h3.gw-section-head heading")
	}
	// Must NOT be inside a <details> wrapper.
	if strings.Contains(body, `<details><summary>Custom linters</summary>`) {
		t.Error("Custom linters must NOT be inside a <details><summary> wrapper")
	}
	if !strings.Contains(body, "Regex-based rules you authored") {
		t.Error("Custom linters section must include description paragraph")
	}
}

// writeWhitelistToml seeds a whitelist.toml at the canonical path under policyRoot/repo.
func writeWhitelistToml(t *testing.T, policyRoot, repo string, entries []whitelist.Entry) {
	t.Helper()
	dir := filepath.Join(policyRoot, repo, ".appframes", "_canonical")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "whitelist.toml")
	var b strings.Builder
	for _, e := range entries {
		b.WriteString("\n[[entry]]\n")
		fmt.Fprintf(&b, "frame  = %q\n", e.Frame)
		fmt.Fprintf(&b, "path   = %q\n", e.Path)
		fmt.Fprintf(&b, "reason = %q\n", e.Reason)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyPage_WhitelistPanelRenders(t *testing.T) {
	root := t.TempDir()
	writeWhitelistToml(t, root, "demo", []whitelist.Entry{
		{Frame: "security/no-hardcoded-credentials", Path: "config/dev.toml", Reason: "test fixture only"},
	})
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		ActiveTab:  "whitelist",
		PolicyRoot: root,
	})
	if !strings.Contains(body, `Whitelist (1)`) {
		t.Error("Whitelist panel heading missing: want 'Whitelist (1)'")
	}
	if !strings.Contains(body, "security/no-hardcoded-credentials") {
		t.Error("Whitelist panel missing frame ID")
	}
	if !strings.Contains(body, "config/dev.toml") {
		t.Error("Whitelist panel missing path")
	}
	if !strings.Contains(body, "test fixture only") {
		t.Error("Whitelist panel missing reason")
	}
}

func TestPolicyPage_WhitelistPanelHasRemoveButtonWithAllowEdits(t *testing.T) {
	root := t.TempDir()
	writeWhitelistToml(t, root, "demo", []whitelist.Entry{
		{Frame: "git/no-bypass-pre-commit", Path: "**", Reason: "allow in test"},
	})
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		ActiveTab:  "whitelist",
		PolicyRoot: root,
	})
	if !strings.Contains(body, `/policy/whitelist/remove`) {
		t.Error("Whitelist panel must show Remove button when AllowEdits=true")
	}
}

func TestPolicyPage_WhitelistPanelReadOnly(t *testing.T) {
	root := t.TempDir()
	writeWhitelistToml(t, root, "demo", []whitelist.Entry{
		{Frame: "git/no-bypass-pre-commit", Path: "**", Reason: "allow in test"},
	})
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: false,
		Repos:      []string{"demo"},
		ActiveTab:  "whitelist",
		PolicyRoot: root,
	})
	if strings.Contains(body, `/policy/whitelist/remove`) {
		t.Error("Whitelist panel must NOT show Remove button when AllowEdits=false")
	}
	// Panel still shows in read-only mode.
	if !strings.Contains(body, `Whitelist (1)`) {
		t.Error("Whitelist panel must still be visible in read-only mode")
	}
}

func TestPolicyPage_TrailerSectionHeads(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		ActiveTab:  "linters",
		PolicyRoot: root,
		Authoring:  `<p>check</p>`,
	})
	if !strings.Contains(body, `<h3 class="gw-section-head">Custom linters</h3>`) {
		t.Error("Custom linters section head missing")
	}
	// Credential and archive sections must not appear on /policy.
	if strings.Contains(body, `Upstream credential`) {
		t.Error("credential section must not appear on /policy (moved to /repos)")
	}
	if strings.Contains(body, `Archive this repo`) {
		t.Error("archive section must not appear on /policy (moved to /repos)")
	}
}

func TestPolicyPage_ActiveRepoSectionHasPickerAndSummary(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		PolicyRoot: root,
	})
	if !strings.Contains(body, `<h3 class="gw-section-head">Active repo</h3>`) {
		t.Error("Active repo section head missing")
	}
	if !strings.Contains(body, `<select name="repo"`) {
		t.Error("repo picker missing from Active repo section")
	}
	if !strings.Contains(body, `Selected: <strong>demo</strong>`) {
		t.Error("Selected repo line missing from Active repo section")
	}
	if !strings.Contains(body, `Currently enabled frames`) {
		t.Error("Currently enabled frames summary missing from Active repo section")
	}
}

func TestPolicyPage_NoActiveRepoEmptyState(t *testing.T) {
	root := t.TempDir()
	vm := buildPolicyView(root, "", nil)
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, policyPageOpts{AllowEdits: true, Repos: nil})
	body := rec.Body.String()
	if !strings.Contains(body, "No repo selected") {
		t.Error("empty-state message missing when no repo and no repos registered")
	}
	if strings.Contains(body, `hx-post="/policy/frames/toggle"`) {
		t.Error("frame toggle must not appear when no repo is selected")
	}
}

func TestPolicyPage_NoMoreTimeEstimatesSection(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		PolicyRoot: root,
	})
	if strings.Contains(body, "Time estimates") || strings.Contains(body, "time-prevented estimates") {
		t.Error("Time estimates section must not appear on /policy")
	}
}

func TestPolicyPage_NoMoreCredentialSection(t *testing.T) {
	root := t.TempDir()
	body := renderPolicyBody(t, root, "demo", policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"demo"},
		PolicyRoot: root,
	})
	if strings.Contains(body, `/policy/repo/credential`) {
		t.Error("credential form must not appear on /policy (moved to /repos)")
	}
}
