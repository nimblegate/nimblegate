// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"html/template"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// seedNotifRailRepo creates a registered repo under root with a minimal
// gateway.toml (upstream URL set, no notification section yet). Returns the
// repo directory.
func seedNotifRailRepo(t *testing.T, root, repo string) string {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlBody := `upstream-url = "https://example.test/` + repo + `.git"` + "\n" +
		`enabled = true` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(tomlBody), 0o644); err != nil {
		t.Fatalf("write gateway.toml: %v", err)
	}
	return dir
}

func TestRenderNotificationRailSection_includesAllFields(t *testing.T) {
	view := defaultNotifRailView()
	view.WebhookURL = "https://hooks.example.com"
	view.HasSecret = true
	view.RotationBots = []string{"@nimblegate-bot", "@second-bot"}

	var buf bytes.Buffer
	renderNotificationRailSection(&buf, "test", view, true, "tok", "", false)
	body := buf.String()

	for _, want := range []string{
		"Notification rail",
		`name="enabled"`,
		`name="observe_pr_comments"`,
		`name="webhook_url"`, `value="https://hooks.example.com"`,
		`name="auth_mode"`, `value="hmac"`,
		`name="secret"`,
		`name="auth_header"`,
		`name="mention_default"`, "@nimblegate-bot",
		`name="auto_tag_assignees"`,
		`name="rotation_bots"`, "@second-bot",
		`name="attempts_per_bot"`,
		`name="rotate_on_repeat_finding"`,
		`name="fallback_human"`,
		`name="loop_max_attempts"`,
		`name="cooldown_threshold_count"`,
		`name="cooldown_threshold_window"`,
		`name="cooldown_duration"`,
		`name="delivery_max_attempts"`,
		`name="backoff_schedule"`,
		`Generate random`,
		`data-notifrail-reveal`,
		"Reset section to defaults",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in notification rail section\n%s", want, body)
		}
	}
	// Secret must be masked when HasSecret = true; the raw secret never appears.
	if strings.Contains(body, "test-secret") {
		t.Errorf("section should not echo real secret values")
	}
	if !strings.Contains(body, strings.Repeat("•", 16)) {
		t.Errorf("secret should be masked with bullets when one is on file")
	}
}

func TestRenderNotificationRailSection_readOnlyHidesForm(t *testing.T) {
	var buf bytes.Buffer
	renderNotificationRailSection(&buf, "test", defaultNotifRailView(), false, "tok", "", false)
	body := buf.String()
	if !strings.Contains(body, "Notification rail") {
		t.Errorf("read-only should still show the section heading")
	}
	if strings.Contains(body, `name="webhook_url"`) {
		t.Errorf("read-only mode should NOT render the form inputs")
	}
	if !strings.Contains(body, "Read-only mode") {
		t.Errorf("read-only banner should be shown")
	}
}

func TestRenderNotificationRailSection_showsSaveError(t *testing.T) {
	var buf bytes.Buffer
	renderNotificationRailSection(&buf, "test", defaultNotifRailView(), true, "tok", "bad duration: xyz", false)
	body := buf.String()
	if !strings.Contains(body, "bad duration: xyz") {
		t.Errorf("error banner not rendered: %s", body)
	}
	if !strings.Contains(body, "open") {
		// Open attribute should appear in the <details open> for visibility.
		t.Errorf("section should auto-expand when there's a save error: %s", body)
	}
}

func TestLoadNotifRailView_returnsDefaultsForMissingSection(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	view := loadNotifRailView(root, "test")
	if view.AuthMode != "hmac" {
		t.Errorf("default auth mode = %q, want hmac", view.AuthMode)
	}
	if view.LoopMaxAttempts != 5 {
		t.Errorf("default loop max = %d, want 5", view.LoopMaxAttempts)
	}
	if view.MentionDefault != "@nimblegate-bot" {
		t.Errorf("default mention = %q", view.MentionDefault)
	}
	if view.HasSecret {
		t.Errorf("fresh repo has no secret")
	}
}

func TestLoadNotifRailView_roundtripsExistingSection(t *testing.T) {
	root := t.TempDir()
	dir := seedNotifRailRepo(t, root, "test")
	// Overwrite gateway.toml with a richer config we want to load back.
	body := `upstream-url = "https://example.test/test.git"
enabled = true

[notification]
enabled = true
observe-pr-comments = true

[notification.webhook]
url = "https://hooks.example.com"
auth-mode = "hmac"
secret = "the-secret"
auth-header = "X-Custom-Sig"

[notification.mention]
default = "@my-bot"
include-pr-assignees = false

[notification.mention.rotation]
bots = ["@b1", "@b2"]
attempts-per-bot = 3
fallback-human = "@me"

[notification.loop]
max-attempts = 9
cooldown-threshold-count = 4
cooldown-threshold-window = "7m"
cooldown-duration = "20m"

[notification.delivery]
max-attempts = 30
backoff-schedule = ["30s", "2m", "10m"]
`
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	view := loadNotifRailView(root, "test")
	if !view.Enabled || !view.ObservePRComments {
		t.Errorf("toggles not loaded: %+v", view)
	}
	if view.WebhookURL != "https://hooks.example.com" {
		t.Errorf("webhook URL = %q", view.WebhookURL)
	}
	if !view.HasSecret {
		t.Errorf("HasSecret should be true when secret is on file")
	}
	if view.MentionDefault != "@my-bot" {
		t.Errorf("mention default = %q", view.MentionDefault)
	}
	if view.AutoTagAssignees {
		t.Errorf("AutoTagAssignees should be false")
	}
	if len(view.RotationBots) != 2 || view.RotationBots[0] != "@b1" {
		t.Errorf("rotation bots wrong: %v", view.RotationBots)
	}
	if view.AttemptsPerBot != 3 || view.FallbackHuman != "@me" {
		t.Errorf("rotation knobs wrong: %+v", view)
	}
	if view.LoopMaxAttempts != 9 || view.CooldownThresholdCount != 4 {
		t.Errorf("loop knobs wrong: %+v", view)
	}
	if view.CooldownThresholdWindow != "7m" || view.CooldownDuration != "20m" {
		t.Errorf("cooldown durations wrong: %+v", view)
	}
	if view.DeliveryMaxAttempts != 30 || len(view.BackoffSchedule) != 3 {
		t.Errorf("delivery wrong: %+v", view)
	}
}

func TestWriteNotifRailTOML_atomicAndPreservesNonNotificationKeys(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")

	view := defaultNotifRailView()
	view.Enabled = true
	view.WebhookURL = "https://h.test"
	if err := writeNotifRailTOML(root, "test", view, "shhh"); err != nil {
		t.Fatalf("write: %v", err)
	}

	var raw struct {
		UpstreamURL  string `toml:"upstream-url"`
		Enabled      bool   `toml:"enabled"`
		Notification *struct {
			Enabled bool `toml:"enabled"`
			Webhook *struct {
				URL    string `toml:"url"`
				Secret string `toml:"secret"`
			} `toml:"webhook"`
		} `toml:"notification"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "test", "gateway.toml"), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.UpstreamURL != "https://example.test/test.git" {
		t.Errorf("upstream-url was clobbered: %+v", raw)
	}
	if !raw.Enabled {
		t.Errorf("top-level enabled was clobbered")
	}
	if raw.Notification == nil || !raw.Notification.Enabled {
		t.Errorf("notification.enabled not written: %+v", raw.Notification)
	}
	if raw.Notification.Webhook.Secret != "shhh" {
		t.Errorf("secret not written: %q", raw.Notification.Webhook.Secret)
	}
}

func TestWriteNotifRailTOML_preservesSecretOnEmpty(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	// Seed with a secret.
	view := defaultNotifRailView()
	view.WebhookURL = "https://h.test"
	if err := writeNotifRailTOML(root, "test", view, "first-secret"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Save again with empty secret - should keep the on-file secret.
	if err := writeNotifRailTOML(root, "test", view, ""); err != nil {
		t.Fatalf("second write: %v", err)
	}

	var raw struct {
		Notification *struct {
			Webhook *struct {
				Secret string `toml:"secret"`
			} `toml:"webhook"`
		} `toml:"notification"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "test", "gateway.toml"), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if raw.Notification.Webhook.Secret != "first-secret" {
		t.Errorf("secret should be preserved when form is empty, got %q", raw.Notification.Webhook.Secret)
	}
}

func TestWriteNotifRailTOML_rejectsHMACWithoutSecret(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	view := defaultNotifRailView()
	view.WebhookURL = "https://h.test"
	view.AuthMode = "hmac"
	// No prior secret on file + empty in form = hard error.
	err := writeNotifRailTOML(root, "test", view, "")
	if err == nil {
		t.Fatalf("expected validation error for HMAC + empty secret")
	}
	if !strings.Contains(err.Error(), "HMAC") || !strings.Contains(err.Error(), "secret") {
		t.Errorf("error doesn't explain HMAC/secret requirement: %v", err)
	}
}

func TestParseNotifRailForm_validatesDurations(t *testing.T) {
	form := url.Values{}
	form.Set("repo", "test")
	form.Set("cooldown_threshold_window", "not-a-duration")
	form.Set("cooldown_duration", "10m")
	_, _, errMsg := parseNotifRailForm(form)
	if errMsg == "" {
		t.Fatalf("expected error for non-duration cooldown window")
	}
	if !strings.Contains(errMsg, "cooldown threshold window") {
		t.Errorf("error message should reference cooldown window: %q", errMsg)
	}
}

func TestParseNotifRailForm_validatesBackoffEntries(t *testing.T) {
	form := url.Values{}
	form.Set("repo", "test")
	form.Set("cooldown_threshold_window", "5m")
	form.Set("cooldown_duration", "10m")
	form.Set("backoff_schedule", "1m, junk, 5m")
	_, _, errMsg := parseNotifRailForm(form)
	if errMsg == "" || !strings.Contains(errMsg, "backoff schedule") {
		t.Errorf("expected backoff schedule error, got: %q", errMsg)
	}
}

func TestParseNotifRailForm_happyPath(t *testing.T) {
	form := url.Values{}
	form.Set("repo", "test")
	form.Set("enabled", "1")
	form.Set("webhook_url", " https://h.test ")
	form.Set("auth_mode", "bearer")
	form.Set("secret", "tok-xyz")
	form.Set("mention_default", "@bot")
	form.Set("auto_tag_assignees", "1")
	form.Set("rotation_bots", "@a\n@b\n\n  @c  ")
	form.Set("attempts_per_bot", "3")
	form.Set("loop_max_attempts", "7")
	form.Set("cooldown_threshold_count", "2")
	form.Set("cooldown_threshold_window", "5m")
	form.Set("cooldown_duration", "10m")
	form.Set("delivery_max_attempts", "15")
	form.Set("backoff_schedule", "1m, 5m, 1h")

	view, secret, errMsg := parseNotifRailForm(form)
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if !view.Enabled {
		t.Errorf("enabled toggle dropped")
	}
	if view.WebhookURL != "https://h.test" {
		t.Errorf("webhook URL not trimmed: %q", view.WebhookURL)
	}
	if view.AuthMode != "bearer" {
		t.Errorf("auth mode = %q", view.AuthMode)
	}
	if secret != "tok-xyz" {
		t.Errorf("secret pass-through wrong: %q", secret)
	}
	if len(view.RotationBots) != 3 || view.RotationBots[2] != "@c" {
		t.Errorf("rotation bots parsing wrong: %v", view.RotationBots)
	}
	if view.AttemptsPerBot != 3 {
		t.Errorf("attempts per bot = %d", view.AttemptsPerBot)
	}
	if len(view.BackoffSchedule) != 3 || view.BackoffSchedule[2] != "1h" {
		t.Errorf("backoff schedule wrong: %v", view.BackoffSchedule)
	}
}

func TestNotifRailHandler_save_rejectsCSRFFailure(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	h := notifRailHandlers{policyRoot: root, token: "tok"}

	form := url.Values{}
	form.Set("repo", "test")
	form.Set("cooldown_threshold_window", "5m")
	form.Set("cooldown_duration", "10m")

	req := httptest.NewRequest("POST", "/policy/notification/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// No CSRF token → must be rejected.
	rec := httptest.NewRecorder()
	h.save(rec, req)
	if rec.Code != 403 {
		t.Errorf("expected 403 on missing CSRF, got %d", rec.Code)
	}
}

func TestNotifRailHandler_save_happyPathWritesTOML(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	h := notifRailHandlers{policyRoot: root, token: "tok"}

	form := url.Values{}
	form.Set("repo", "test")
	form.Set("enabled", "1")
	form.Set("webhook_url", "https://h.test")
	form.Set("auth_mode", "bearer")
	form.Set("secret", "tok-xyz")
	form.Set("cooldown_threshold_window", "5m")
	form.Set("cooldown_duration", "10m")

	req := httptest.NewRequest("POST", "/policy/notification/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")

	rec := httptest.NewRecorder()
	h.save(rec, req)
	if rec.Code != 303 {
		t.Errorf("expected 303 redirect, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "notifrail=saved") {
		t.Errorf("redirect location missing notifrail=saved: %q", loc)
	}

	// Verify the TOML actually was written with the form's values.
	view := loadNotifRailView(root, "test")
	if !view.Enabled || view.AuthMode != "bearer" {
		t.Errorf("save did not persist: %+v", view)
	}
}

func TestNotifRailHandler_save_HMACWithoutSecretReturns500(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	h := notifRailHandlers{policyRoot: root, token: "tok"}

	form := url.Values{}
	form.Set("repo", "test")
	form.Set("enabled", "1")
	form.Set("webhook_url", "https://h.test")
	form.Set("auth_mode", "hmac")
	// no secret
	form.Set("cooldown_threshold_window", "5m")
	form.Set("cooldown_duration", "10m")

	req := httptest.NewRequest("POST", "/policy/notification/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")

	rec := httptest.NewRecorder()
	h.save(rec, req)
	if rec.Code != 500 {
		t.Errorf("expected 500 on HMAC + empty secret, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "HMAC") {
		t.Errorf("error body should mention HMAC: %s", rec.Body.String())
	}
}

func TestNotifRailHandler_generateSecret(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	h := notifRailHandlers{policyRoot: root, token: "tok"}

	form := url.Values{}
	form.Set("repo", "test")
	req := httptest.NewRequest("POST", "/policy/notification/generate-secret", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")

	rec := httptest.NewRecorder()
	h.generateSecret(rec, req)
	if rec.Code != 200 {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "notifrail-secret") {
		t.Errorf("response missing the secret input partial: %s", body)
	}
	if !strings.Contains(body, `value="`) {
		t.Errorf("response missing a value attribute")
	}
}

// Cover the section render path end-to-end by exercising the helper that
// callers use to embed the section into the policy page.
func TestRenderNotificationRailSection_emptyRepoSilent(t *testing.T) {
	var buf bytes.Buffer
	renderNotificationRailSection(&buf, "", defaultNotifRailView(), true, "tok", "", false)
	if buf.Len() != 0 {
		t.Errorf("empty repo should render nothing, got: %s", buf.String())
	}
}

// Sanity check: the page renderer writes the NotifRail HTML into the
// pageTrailer when present.
// TestRenderPolicyPage_doesNotEmbedNotifRail pins the design: as of the
// Auto-PR sidebar rework, the notification rail edit form lives on
// /auto-pr/config (Setup tab) - NOT on /policy. The opts.NotifRail field is
// still in the struct for backwards compat but renderPolicyPage no longer
// renders it (the pointer hint was removed too, since the sidebar entry is
// the discovery surface now).
func TestRenderPolicyPage_doesNotEmbedNotifRail(t *testing.T) {
	root := t.TempDir()
	seedNotifRailRepo(t, root, "test")
	vm := buildPolicyView(root, "test", nil)

	var sectionBuf bytes.Buffer
	renderNotificationRailSection(&sectionBuf, "test", defaultNotifRailView(), true, "tok", "", false)
	if sectionBuf.Len() == 0 {
		t.Fatalf("section should not be empty")
	}

	var pageBuf bytes.Buffer
	err := renderPolicyPage(&pageBuf, vm, policyPageOpts{
		AllowEdits: true,
		CSRFToken:  "tok",
		Repos:      []string{"test"},
		PolicyRoot: root,
		NotifRail:  template.HTML(sectionBuf.String()),
	})
	if err != nil {
		t.Fatalf("renderPolicyPage: %v", err)
	}
	if strings.Contains(pageBuf.String(), "gw-notifrail-form") {
		t.Errorf("policy page should not embed the notif rail edit form (lives on /auto-pr/config now):\n%s", pageBuf.String())
	}
}
