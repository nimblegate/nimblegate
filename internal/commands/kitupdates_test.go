// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/kits"
)

const newFrame = "security/no-secret-in-client-env"

// writePreUpgradeRepo writes a repo that applied Core before newFrame was
// added to it: every Core frame but newFrame enabled, no seen_frames.
func writePreUpgradeRepo(t *testing.T, tmp string) []string {
	t.Helper()
	ks, err := kits.LoadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	core, _ := ks.Get("core")
	var enabled []string
	for _, id := range core.Frames {
		if id != newFrame {
			enabled = append(enabled, id)
		}
	}
	writeMinConfig(t, tmp, enabled, []string{"core"})
	return enabled
}

func TestNewKitFrames_ListsFrameAddedAfterApply(t *testing.T) {
	tmp := t.TempDir()
	writePreUpgradeRepo(t, tmp)
	got := newKitFrames(tmp, testRepo)
	if len(got) != 1 || got[0].Kit != "core" || len(got[0].Frames) != 1 || got[0].Frames[0].ID != newFrame {
		t.Fatalf("newKitFrames = %+v, want core: [%s]", got, newFrame)
	}
	if got[0].Frames[0].Title == "" {
		t.Errorf("frame title missing")
	}
}

func TestNewKitFrames_NoneAfterApplyingTheKit(t *testing.T) {
	tmp := t.TempDir()
	writeMinConfig(t, tmp, nil, nil)
	h := policyHandlers{policyRoot: tmp, token: "tok"}
	req := httptest.NewRequest("POST", "/policy/kits/apply", strings.NewReader("repo="+testRepo+"&name=core"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.applyKit(w, req)
	if w.Code != 200 {
		t.Fatalf("apply status %d", w.Code)
	}
	if got := newKitFrames(tmp, testRepo); len(got) != 0 {
		t.Fatalf("after apply: %+v, want none", got)
	}
	// A frame unticked after applying is not offered again.
	cfg := filepath.Join(tmp, testRepo, "appframes.toml")
	data, _ := os.ReadFile(cfg)
	doc, _, err := rewriteEnabledList(string(data), newFrame, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := newKitFrames(tmp, testRepo); len(got) != 0 {
		t.Fatalf("after untick: %+v, want none", got)
	}
}

func TestNewKitFrames_Dismiss(t *testing.T) {
	tmp := t.TempDir()
	before := writePreUpgradeRepo(t, tmp)
	h := policyHandlers{policyRoot: tmp, token: "tok"}
	req := httptest.NewRequest("POST", "/policy/kits/dismiss-new", strings.NewReader("repo="+testRepo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.dismissNewKitFrames(w, req)
	if w.Code >= 400 {
		t.Fatalf("dismiss status %d: %s", w.Code, w.Body.String())
	}
	if got := newKitFrames(tmp, testRepo); len(got) != 0 {
		t.Fatalf("after dismiss: %+v, want none", got)
	}
	if after := readEnabledList(t, tmp); len(after) != len(before) {
		t.Errorf("dismiss changed enabled: %d -> %d", len(before), len(after))
	}
}

func TestNewKitFrames_AddEnablesOnlyNewFrames(t *testing.T) {
	tmp := t.TempDir()
	before := writePreUpgradeRepo(t, tmp)
	h := policyHandlers{policyRoot: tmp, token: "tok"}
	req := httptest.NewRequest("POST", "/policy/kits/add-new", strings.NewReader("repo="+testRepo+"&confirm=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	h.addNewKitFrames(w, req)
	if w.Code >= 400 {
		t.Fatalf("add status %d: %s", w.Code, w.Body.String())
	}
	after := readEnabledList(t, tmp)
	if len(after) != len(before)+1 || !containsStr(after, newFrame) {
		t.Fatalf("enabled after add: %d frames, has new=%v; want %d with it", len(after), containsStr(after, newFrame), len(before)+1)
	}
	if got := newKitFrames(tmp, testRepo); len(got) != 0 {
		t.Fatalf("after add: %+v, want none", got)
	}
	if applied := readAppliedKitsList(t, tmp); len(applied) != 1 || applied[0] != "core" {
		t.Errorf("applied_kits = %v, want [core]", applied)
	}
}

func TestNewKitFrames_CSRF(t *testing.T) {
	tmp := t.TempDir()
	writePreUpgradeRepo(t, tmp)
	h := policyHandlers{policyRoot: tmp, token: "tok"}
	req := httptest.NewRequest("POST", "/policy/kits/add-new", strings.NewReader("repo="+testRepo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.addNewKitFrames(w, req)
	if w.Code != 403 {
		t.Errorf("add without token: %d, want 403", w.Code)
	}
	req = httptest.NewRequest("POST", "/policy/kits/dismiss-new", strings.NewReader("repo="+testRepo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	h.dismissNewKitFrames(w, req)
	if w.Code != 403 {
		t.Errorf("dismiss without token: %d, want 403", w.Code)
	}
	if got := newKitFrames(tmp, testRepo); len(got) != 1 {
		t.Errorf("notice gone after rejected requests: %+v", got)
	}
}

func TestKitUpdateNotice_Render(t *testing.T) {
	tmp := t.TempDir()
	writePreUpgradeRepo(t, tmp)
	vm := buildPolicyView(tmp, testRepo, readEnabledList(t, tmp))
	var b bytes.Buffer
	renderKitUpdateNotice(&b, vm)
	html := b.String()
	for _, want := range []string{"Core has 1 new frame", newFrame, "/policy/kits/preview-new", "/policy/kits/dismiss-new"} {
		if !strings.Contains(html, want) {
			t.Errorf("notice lacks %q:\n%s", want, html)
		}
	}
	vm.ReadOnly = true
	b.Reset()
	renderKitUpdateNotice(&b, vm)
	if strings.Contains(b.String(), "<form") || !strings.Contains(b.String(), newFrame) {
		t.Errorf("read-only notice must list the frame without forms:\n%s", b.String())
	}
}

func TestKitUpdate_ReposBadgeAndHealth(t *testing.T) {
	tmp := t.TempDir()
	writePreUpgradeRepo(t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, testRepo, "gateway.toml"), []byte("enabled = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := renderReposPage(&b, reposPageOpts{PolicyRoot: tmp}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "1 new kit frame") {
		t.Errorf("repos page lacks the badge")
	}
	d := collectHealth(tmp, "", time.Now(), time.Now())
	if len(d.KitUpdateRepos) != 1 || d.KitUpdateRepos[0] != testRepo {
		t.Errorf("health KitUpdateRepos = %v, want [%s]", d.KitUpdateRepos, testRepo)
	}
}

func TestWriteFullConfig_KeepsSeverityAndLinters(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "appframes.toml")
	doc := "[frames]\nenabled = [\n    \"security/no-hardcoded-credentials\",\n]\n\n[frames.\"security/no-hardcoded-credentials\"]\nseverity = \"WARN\"\n\n[time-estimates]\ntier-1 = 2\n\n[ui]\napplied_kits = [\"core\"]\nseen_frames = [\"a/b\"]\n"
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := readFullConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UI.UserKits = append(cfg.UI.UserKits, userKitTOML{Name: "mine", Frames: []string{"security/no-hardcoded-credentials"}})
	if err := writeFullConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	for _, want := range []string{`severity = "WARN"`, "[time-estimates]", `seen_frames = ["a/b"]`, `name = "mine"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lost %q:\n%s", want, b)
		}
	}
	if _, err := readFullConfig(path); err != nil {
		t.Errorf("rewritten file does not parse: %v\n%s", err, b)
	}
}
