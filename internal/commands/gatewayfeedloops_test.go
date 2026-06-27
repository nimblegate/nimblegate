// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
)

// seedActiveLoopRepo writes a registered repo with one active PRState file
// at the expected location. Used to exercise the /feed enrichment + the
// /feed/reset-loop handler.
func seedActiveLoopRepo(t *testing.T, root, repo string, pr int, attempt, max int, bot string) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(`upstream-url = "https://example.test"`+"\n"), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}
	state := notification.PRState{
		PRNumber: pr,
		Repo:     repo,
		Loop:     notification.LoopCounters{AttemptCount: attempt, MaxAttempts: max},
		Mention:  notification.MentionCounters{CurrentBot: bot},
	}
	if err := notification.WritePRState(root, repo, pr, state); err != nil {
		t.Fatalf("write state: %v", err)
	}
}

func TestLoadActiveLoopsByRepo_returnsHighestAttempt(t *testing.T) {
	root := t.TempDir()
	seedActiveLoopRepo(t, root, "api", 41, 1, 5, "@bot")
	seedActiveLoopRepo(t, root, "api", 42, 3, 5, "@bot-2")
	seedActiveLoopRepo(t, root, "web", 7, 2, 5, "@bot")

	loops := loadActiveLoopsByRepo(root)
	if len(loops) != 2 {
		t.Fatalf("want 2 repos with loops, got %d (%+v)", len(loops), loops)
	}
	if api := loops["api"]; api == nil || api.PRNumber != 42 || api.AttemptCount != 3 {
		t.Errorf("api should surface highest-attempt PR 42 (attempt 3), got %+v", api)
	}
	if web := loops["web"]; web == nil || web.PRNumber != 7 || web.AttemptCount != 2 {
		t.Errorf("web active loop wrong: %+v", web)
	}
	if !strings.Contains(loops["api"].ResetURL, "repo=api") {
		t.Errorf("ResetURL should encode repo: %q", loops["api"].ResetURL)
	}
}

func TestLoadActiveLoopsByRepo_skipsExhausted(t *testing.T) {
	root := t.TempDir()
	seedActiveLoopRepo(t, root, "api", 42, 5, 5, "@bot")
	// Mark exhausted on disk.
	state := notification.PRState{
		PRNumber: 42, Repo: "api",
		Loop: notification.LoopCounters{AttemptCount: 5, MaxAttempts: 5, Exhausted: true},
	}
	if err := notification.WritePRState(root, "api", 42, state); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	loops := loadActiveLoopsByRepo(root)
	if loops["api"] != nil {
		t.Errorf("exhausted loops should not be surfaced, got %+v", loops["api"])
	}
}

func TestApplyActiveLoops_attachesToFirstRejectedRow(t *testing.T) {
	vm := gateway.ViewModel{Rows: []gateway.DecisionRow{
		{Repo: "api", Accept: true, Time: time.Now()},
		{Repo: "api", Accept: false, Time: time.Now().Add(-time.Minute)},
		{Repo: "api", Accept: false, Time: time.Now().Add(-2 * time.Minute)},
		{Repo: "web", Accept: false, Time: time.Now().Add(-time.Minute)},
	}}
	loops := map[string]*gateway.ActiveLoopView{
		"api": {PRNumber: 42, AttemptCount: 3, MaxAttempts: 5, CurrentBot: "@bot"},
		"web": {PRNumber: 7, AttemptCount: 2, MaxAttempts: 5},
	}
	applyActiveLoops(&vm, loops)

	if vm.Rows[0].ActiveLoop != nil {
		t.Errorf("accepted row should not get an ActiveLoop")
	}
	if vm.Rows[1].ActiveLoop == nil {
		t.Errorf("first rejected api row should get the loop")
	} else if vm.Rows[1].ActiveLoop.PRNumber != 42 {
		t.Errorf("wrong PR number: %d", vm.Rows[1].ActiveLoop.PRNumber)
	}
	if vm.Rows[2].ActiveLoop != nil {
		t.Errorf("only the FIRST rejected row per repo should be tagged, got %+v on row 2", vm.Rows[2].ActiveLoop)
	}
	if vm.Rows[3].ActiveLoop == nil || vm.Rows[3].ActiveLoop.PRNumber != 7 {
		t.Errorf("web loop should land on its rejected row: %+v", vm.Rows[3].ActiveLoop)
	}
}

func TestApplyActiveLoops_emptyMapNoop(t *testing.T) {
	vm := gateway.ViewModel{Rows: []gateway.DecisionRow{
		{Repo: "api", Accept: false},
	}}
	applyActiveLoops(&vm, nil)
	if vm.Rows[0].ActiveLoop != nil {
		t.Errorf("nil loops should leave rows untouched")
	}
}

func TestFeedResetLoopHandler_deletesStateFile(t *testing.T) {
	root := t.TempDir()
	seedActiveLoopRepo(t, root, "api", 42, 3, 5, "@bot")
	statePath := filepath.Join(root, "api", "pr-comment-state", "42.json")
	if _, err := os.Stat(statePath); err != nil {
		t.Fatalf("seeding failed: %v", err)
	}

	req := httptest.NewRequest("POST", "/feed/reset-loop?repo=api&pr=42", nil)
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()
	feedResetLoopHandler(root, "tok")(rec, req)

	if rec.Code != 200 {
		t.Errorf("status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("state file should be deleted, stat err = %v", err)
	}
}

func TestFeedResetLoopHandler_noopOnMissingState(t *testing.T) {
	root := t.TempDir()
	// Register repo so validRepoName passes, but no PRState exists.
	if err := os.MkdirAll(filepath.Join(root, "api"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	req := httptest.NewRequest("POST", "/feed/reset-loop?repo=api&pr=999", nil)
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()
	feedResetLoopHandler(root, "tok")(rec, req)

	if rec.Code != 200 {
		t.Errorf("reset on missing state should be a 200 no-op, got %d", rec.Code)
	}
}

func TestFeedResetLoopHandler_methodNotAllowedOnGet(t *testing.T) {
	root := t.TempDir()
	req := httptest.NewRequest("GET", "/feed/reset-loop?repo=api&pr=42", nil)
	rec := httptest.NewRecorder()
	feedResetLoopHandler(root, "tok")(rec, req)
	if rec.Code != 405 {
		t.Errorf("GET should be 405 (state-changing endpoints must be POST), got %d", rec.Code)
	}
}

func TestFeedResetLoopHandler_csrfRejected(t *testing.T) {
	root := t.TempDir()
	seedActiveLoopRepo(t, root, "api", 42, 1, 5, "@bot")
	form := url.Values{}
	form.Set("repo", "api")
	form.Set("pr", "42")
	req := httptest.NewRequest("POST", "/feed/reset-loop", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No CSRF token.
	rec := httptest.NewRecorder()
	feedResetLoopHandler(root, "tok")(rec, req)
	if rec.Code != 403 {
		t.Errorf("missing CSRF should 403, got %d", rec.Code)
	}
}

func TestFeedResetLoopHandler_missingRepoIs400(t *testing.T) {
	req := httptest.NewRequest("POST", "/feed/reset-loop?pr=42", nil)
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()
	feedResetLoopHandler(t.TempDir(), "tok")(rec, req)
	if rec.Code != 400 {
		t.Errorf("missing repo should 400, got %d", rec.Code)
	}
}

// The feed template surfaces the new indicator + the Reset Loop button when
// the row carries the matching state.
func TestRenderGwFeed_showsNotificationIndicators(t *testing.T) {
	delivered := gateway.AuditRecord{
		Time: time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC), Repo: "api",
		Refs: []string{"refs/heads/main"}, Accept: false,
		Notification: &gateway.NotificationStatus{
			EventID:         "evt-1",
			InlineSucceeded: true,
		},
	}
	deadletter := gateway.AuditRecord{
		Time: time.Date(2026, 6, 4, 12, 1, 0, 0, time.UTC), Repo: "web",
		Refs: []string{"refs/heads/main"}, Accept: false,
		Notification: &gateway.NotificationStatus{
			EventID:          "evt-2",
			Deadlettered:     true,
			DeliveryAttempts: 20,
		},
	}
	vm := gateway.BuildView([]gateway.AuditRecord{delivered, deadletter}, gateway.Filter{})
	rec := httptest.NewRecorder()
	renderGwFeed(rec, vm)
	body := rec.Body.String()
	for _, want := range []string{
		"PR comment delivered",
		"delivery failed after 20 attempts",
		`class="gw-ico"`,
		`class="gw-notif gw-notif-delivered"`,
		`class="gw-notif gw-notif-deadlettered"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feed body missing %q\n%s", want, body)
		}
	}
}

func TestRenderGwFeed_showsActiveLoopStrip(t *testing.T) {
	row := gateway.AuditRecord{
		Time: time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC), Repo: "api",
		Refs: []string{"refs/heads/main"}, Accept: false,
	}
	vm := gateway.BuildView([]gateway.AuditRecord{row}, gateway.Filter{})
	vm.Rows[0].ActiveLoop = &gateway.ActiveLoopView{
		PRNumber: 42, AttemptCount: 3, MaxAttempts: 5, CurrentBot: "@bot-a",
		ResetURL: "/feed/reset-loop?repo=api&pr=42",
	}
	rec := httptest.NewRecorder()
	renderGwFeed(rec, vm)
	body := rec.Body.String()
	for _, want := range []string{
		`class="fnd LOOP"`, "3/5", "@bot-a", ">Reset<",
		`class="gw-looprow"`,
		`<col class="col-reset">`, `class="gw-resetcell"`,
		`action="/feed/reset-loop?repo=api&amp;pr=42"`,
		`hx-confirm`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("feed missing %q\n%s", want, body)
		}
	}
}

// Without a notification status the row renders exactly as before - the new
// chips/loops are additive.
func TestRenderGwFeed_legacyRowsRenderUnchanged(t *testing.T) {
	row := gateway.AuditRecord{
		Time:   time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC),
		Repo:   "api",
		Refs:   []string{"refs/heads/main"},
		Accept: true,
	}
	vm := gateway.BuildView([]gateway.AuditRecord{row}, gateway.Filter{})
	rec := httptest.NewRecorder()
	renderGwFeed(rec, vm)
	body := rec.Body.String()
	for _, banned := range []string{"gw-notif", "fnd LOOP", "gw-loopresetform", "gw-looprow"} {
		if strings.Contains(body, banned) {
			t.Errorf("legacy row should not get notification UI, found %q in:\n%s", banned, body)
		}
	}
}

func TestApplyNotifOff(t *testing.T) {
	dir := t.TempDir()
	store := gateway.FilePolicyStore{Root: dir}
	save := func(p gateway.Policy) {
		if err := os.MkdirAll(filepath.Join(dir, p.Repo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := store.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	save(gateway.Policy{Repo: "off", UpstreamURL: "https://x.test/off.git"})
	save(gateway.Policy{Repo: "on", UpstreamURL: "https://x.test/on.git", Notification: &gateway.NotificationConfig{Enabled: true}})
	save(gateway.Policy{Repo: "local"})

	vm := &gateway.ViewModel{Rows: []gateway.DecisionRow{
		{Repo: "off", Accept: false},
		{Repo: "on", Accept: false},
		{Repo: "local", Accept: false},
		{Repo: "off", Accept: true},
	}}
	applyNotifOff(vm, dir)

	if vm.Rows[0].NotifOff == nil {
		t.Error("repo with upstream + notifications off must get the nudge")
	}
	if vm.Rows[1].NotifOff != nil {
		t.Error("repo with notifications on must NOT get the nudge")
	}
	if vm.Rows[2].NotifOff != nil {
		t.Error("repo without an upstream must NOT get the nudge")
	}
	if vm.Rows[3].NotifOff != nil {
		t.Error("accept rows must never get the nudge")
	}
}
