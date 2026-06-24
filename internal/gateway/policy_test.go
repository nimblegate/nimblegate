// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"nimblegate/internal/gateway/notification"
)

func TestFilePolicyStore_GateAllRefsRoundtrip(t *testing.T) {
	s := FilePolicyStore{Root: t.TempDir()}
	if err := s.Save(Policy{Repo: "demo", UpstreamURL: "file:///u", GateAllRefs: true}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !got.GateAllRefs {
		t.Error("GateAllRefs did not round-trip through gateway.toml")
	}
}

func TestFilePolicyStore_SaveLoad(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	in := Policy{
		Repo:          "demo",
		UpstreamURL:   "https://example.com/demo.git",
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		Observe:       true,
	}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.UpstreamURL != in.UpstreamURL || got.Enabled != in.Enabled || got.Observe != in.Observe ||
		len(got.ProtectedRefs) != 1 || got.ProtectedRefs[0] != "refs/heads/main" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.PolicyDir != filepath.Join(root, "demo") {
		t.Errorf("PolicyDir = %q, want %q", got.PolicyDir, filepath.Join(root, "demo"))
	}
}

func TestFilePolicyStore_LoadUnknown(t *testing.T) {
	s := FilePolicyStore{Root: t.TempDir()}
	if _, err := s.Load("nope"); err == nil {
		t.Error("loading an unregistered repo must error")
	}
}

func TestFilePolicyStore_LoadValidatesPatterns(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	// Save a policy with a malformed protected-ref pattern, then expect Load to reject it.
	if err := s.Save(Policy{Repo: "bad", UpstreamURL: "x", ProtectedRefs: []string{"[oops"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("bad"); err == nil {
		t.Error("Load must reject a policy whose ProtectedRefs pattern is malformed (fail-closed at load)")
	}
}

func TestFilePolicyStore_SetEnabled(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	if err := s.Save(Policy{Repo: "demo", UpstreamURL: "u", ProtectedRefs: []string{"refs/heads/main"}, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled("demo", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	got, err := s.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled {
		t.Error("SetEnabled(false) did not persist")
	}
	if got.UpstreamURL != "u" || len(got.ProtectedRefs) != 1 {
		t.Errorf("SetEnabled clobbered other fields: %+v", got)
	}
}

func TestFilePolicyStore_SetEnabled_unknownRepo(t *testing.T) {
	s := FilePolicyStore{Root: t.TempDir()}
	if err := s.SetEnabled("nope", false); err == nil {
		t.Error("SetEnabled on an unregistered repo must return an error")
	}
}

func TestFilePolicyStore_SetObserve(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	if err := s.Save(Policy{Repo: "demo", UpstreamURL: "u", ProtectedRefs: []string{"refs/heads/main"}, Enabled: true, Observe: false}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetObserve("demo", true); err != nil {
		t.Fatalf("SetObserve: %v", err)
	}
	got, err := s.Load("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Observe {
		t.Error("SetObserve(true) did not persist")
	}
	// Flipping observe must not clobber the rest of the policy.
	if got.UpstreamURL != "u" || len(got.ProtectedRefs) != 1 || !got.Enabled {
		t.Errorf("SetObserve clobbered other fields: %+v", got)
	}
	if err := s.SetObserve("demo", false); err != nil {
		t.Fatalf("SetObserve(false): %v", err)
	}
	if got, _ := s.Load("demo"); got.Observe {
		t.Error("SetObserve(false) did not persist")
	}
}

func TestFilePolicyStore_SetObserve_unknownRepo(t *testing.T) {
	s := FilePolicyStore{Root: t.TempDir()}
	if err := s.SetObserve("nope", true); err == nil {
		t.Error("SetObserve on an unregistered repo must return an error")
	}
}

// writeRepoTOML places a hand-written gateway.toml at <root>/<repo>/gateway.toml.
// Used by the notification-config tests to assert that real on-disk TOML
// (with the kebab-case keys an operator would type) parses correctly.
func writeRepoTOML(t *testing.T, root, repo, body string) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFilePolicyStore_Load_NotificationFullConfig(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url   = "https://example.com/demo.git"
protected-refs = ["refs/heads/main"]
enabled        = true
observe        = false

[notification]
enabled              = true
observe-pr-comments  = true

[notification.webhook]
url         = "https://hooks.example.com/in"
auth-mode   = "bearer"
secret      = "topsecret"
auth-header = "X-Webhook"

[notification.mention]
default              = "@nimblegate-bot"
include-pr-assignees = true

[notification.mention.rotation]
bots                     = ["@claude-code-bot", "@cursor-bot"]
attempts-per-bot         = 3
rotate-on-repeat-finding = true
fallback-human           = "@oncall"

[notification.loop]
max-attempts              = 7
cooldown-threshold-count  = 4
cooldown-threshold-window = "10m"
cooldown-duration         = "1h"

[notification.delivery]
max-attempts     = 12
backoff-schedule = ["30s", "2m", "10m"]
`)
	s := FilePolicyStore{Root: root}
	p, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Notification == nil {
		t.Fatal("Notification must be populated")
	}
	nc := *p.Notification
	if !nc.Enabled || !nc.ObservePRComments {
		t.Errorf("Enabled=%v ObservePRComments=%v want both true", nc.Enabled, nc.ObservePRComments)
	}
	if nc.WebhookURL != "https://hooks.example.com/in" {
		t.Errorf("WebhookURL=%q", nc.WebhookURL)
	}
	if nc.WebhookAuth.Mode != "bearer" || nc.WebhookAuth.Secret != "topsecret" || nc.WebhookAuth.HeaderName != "X-Webhook" {
		t.Errorf("WebhookAuth=%+v", nc.WebhookAuth)
	}
	if nc.Mention.Default != "@nimblegate-bot" || !nc.Mention.IncludePRAssignees {
		t.Errorf("Mention=%+v", nc.Mention)
	}
	wantBots := []string{"@claude-code-bot", "@cursor-bot"}
	if !reflect.DeepEqual(nc.Mention.RotationBots, wantBots) {
		t.Errorf("RotationBots=%v want %v", nc.Mention.RotationBots, wantBots)
	}
	if nc.Mention.AttemptsPerBot != 3 || !nc.Mention.RotateOnRepeatFinding || nc.Mention.FallbackHuman != "@oncall" {
		t.Errorf("rotation=%+v", nc.Mention)
	}
	if nc.LoopCfg.MaxAttempts != 7 {
		t.Errorf("LoopCfg.MaxAttempts=%d want 7", nc.LoopCfg.MaxAttempts)
	}
	if nc.LoopCfg.AttemptsPerBot != 3 {
		t.Errorf("LoopCfg.AttemptsPerBot=%d want 3 (mirrors rotation)", nc.LoopCfg.AttemptsPerBot)
	}
	if nc.LoopCfg.DefaultMention != "@nimblegate-bot" || nc.LoopCfg.FallbackHuman != "@oncall" {
		t.Errorf("LoopCfg=%+v", nc.LoopCfg)
	}
	if nc.Cooldown.ThresholdCount != 4 || nc.Cooldown.ThresholdWindow != 10*time.Minute || nc.Cooldown.Duration != time.Hour {
		t.Errorf("Cooldown=%+v", nc.Cooldown)
	}
	if nc.Delivery.MaxAttempts != 12 {
		t.Errorf("Delivery.MaxAttempts=%d", nc.Delivery.MaxAttempts)
	}
	wantBackoff := []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}
	if !reflect.DeepEqual(nc.Delivery.BackoffSchedule, wantBackoff) {
		t.Errorf("BackoffSchedule=%v want %v", nc.Delivery.BackoffSchedule, wantBackoff)
	}
}

func TestFilePolicyStore_Load_NoNotificationSection(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url   = "https://example.com/demo.git"
protected-refs = ["refs/heads/main"]
enabled        = true
`)
	s := FilePolicyStore{Root: root}
	p, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Notification != nil {
		t.Errorf("expected nil Notification when [notification] absent, got %+v", p.Notification)
	}
}

func TestFilePolicyStore_Load_NotificationDefaultsForMissingSubsections(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url   = "https://example.com/demo.git"
protected-refs = ["refs/heads/main"]
enabled        = true

[notification]
enabled = true
`)
	s := FilePolicyStore{Root: root}
	p, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.Notification == nil {
		t.Fatal("Notification must be populated when [notification] present")
	}
	nc := *p.Notification
	if !nc.Enabled {
		t.Error("Enabled must reflect TOML")
	}

	// Webhook absent → empty URL but default hmac auth mode (rail still works comment-only).
	if nc.WebhookURL != "" {
		t.Errorf("WebhookURL=%q want empty", nc.WebhookURL)
	}
	if nc.WebhookAuth.Mode != "hmac" {
		t.Errorf("WebhookAuth.Mode=%q want default hmac", nc.WebhookAuth.Mode)
	}

	// Mention absent → spec defaults.
	if nc.Mention.Default != "@nimblegate-bot" {
		t.Errorf("Mention.Default=%q want @nimblegate-bot", nc.Mention.Default)
	}
	if !nc.Mention.IncludePRAssignees {
		t.Error("IncludePRAssignees default must be true")
	}
	if len(nc.Mention.RotationBots) != 0 {
		t.Errorf("RotationBots default must be empty (rotation off), got %v", nc.Mention.RotationBots)
	}
	if nc.Mention.AttemptsPerBot != 2 {
		t.Errorf("AttemptsPerBot default=%d want 2", nc.Mention.AttemptsPerBot)
	}

	// Loop absent → spec defaults.
	if nc.LoopCfg.MaxAttempts != 5 {
		t.Errorf("LoopCfg.MaxAttempts=%d want 5", nc.LoopCfg.MaxAttempts)
	}
	if nc.Cooldown.ThresholdCount != 3 || nc.Cooldown.ThresholdWindow != 5*time.Minute || nc.Cooldown.Duration != 10*time.Minute {
		t.Errorf("Cooldown defaults wrong: %+v", nc.Cooldown)
	}

	// Delivery absent → spec defaults.
	if nc.Delivery.MaxAttempts != 20 {
		t.Errorf("Delivery.MaxAttempts=%d want 20", nc.Delivery.MaxAttempts)
	}
	wantBackoff := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	if !reflect.DeepEqual(nc.Delivery.BackoffSchedule, wantBackoff) {
		t.Errorf("BackoffSchedule default=%v want %v", nc.Delivery.BackoffSchedule, wantBackoff)
	}
}

func TestFilePolicyStore_SaveLoad_NotificationRoundtrip(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	in := Policy{
		Repo:          "demo",
		UpstreamURL:   "https://example.com/demo.git",
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		Notification: &NotificationConfig{
			Enabled:           true,
			ObservePRComments: true,
			WebhookURL:        "https://hooks.example.com/in",
			WebhookAuth: notification.WebhookAuth{
				Mode:       "hmac",
				Secret:     "shh",
				HeaderName: "X-Webhook",
			},
			Mention: MentionConfig{
				Default:               "@nimblegate-bot",
				IncludePRAssignees:    true,
				RotationBots:          []string{"@claude-code-bot", "@cursor-bot"},
				AttemptsPerBot:        3,
				RotateOnRepeatFinding: true,
				FallbackHuman:         "@oncall",
			},
			LoopCfg: notification.LoopConfig{
				MaxAttempts:           7,
				AttemptsPerBot:        3,
				RotationBots:          []string{"@claude-code-bot", "@cursor-bot"},
				RotateOnRepeatFinding: true,
				FallbackHuman:         "@oncall",
				DefaultMention:        "@nimblegate-bot",
			},
			Cooldown: notification.CooldownConfig{
				ThresholdCount:  4,
				ThresholdWindow: 10 * time.Minute,
				Duration:        time.Hour,
			},
			Delivery: DeliveryConfig{
				MaxAttempts:     12,
				BackoffSchedule: []time.Duration{30 * time.Second, 2 * time.Minute},
			},
		},
	}
	if err := s.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Notification == nil {
		t.Fatal("Notification lost in roundtrip")
	}
	gnc, inc := *got.Notification, *in.Notification
	if gnc.Enabled != inc.Enabled || gnc.ObservePRComments != inc.ObservePRComments {
		t.Errorf("toplevel mismatch: got %+v want %+v", gnc, inc)
	}
	if gnc.WebhookURL != inc.WebhookURL || gnc.WebhookAuth != inc.WebhookAuth {
		t.Errorf("webhook mismatch: got %+v want %+v", gnc.WebhookAuth, inc.WebhookAuth)
	}
	if !reflect.DeepEqual(gnc.Mention, inc.Mention) {
		t.Errorf("mention mismatch:\n got %+v\nwant %+v", gnc.Mention, inc.Mention)
	}
	if !reflect.DeepEqual(gnc.LoopCfg, inc.LoopCfg) {
		t.Errorf("loop mismatch:\n got %+v\nwant %+v", gnc.LoopCfg, inc.LoopCfg)
	}
	if gnc.Cooldown != inc.Cooldown {
		t.Errorf("cooldown mismatch: got %+v want %+v", gnc.Cooldown, inc.Cooldown)
	}
	if !reflect.DeepEqual(gnc.Delivery, inc.Delivery) {
		t.Errorf("delivery mismatch:\n got %+v\nwant %+v", gnc.Delivery, inc.Delivery)
	}
}

// TestFilePolicyStore_Save_KebabCaseKeys verifies the on-disk TOML uses
// kebab-case keys (matching upstream-url / protected-refs). Catches anyone
// who edits the struct tags without thinking about operator-facing format.
func TestFilePolicyStore_Save_KebabCaseKeys(t *testing.T) {
	root := t.TempDir()
	s := FilePolicyStore{Root: root}
	if err := s.Save(Policy{
		Repo:          "demo",
		UpstreamURL:   "u",
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		Notification: &NotificationConfig{
			Enabled:           true,
			ObservePRComments: true,
			WebhookAuth:       notification.WebhookAuth{Mode: "hmac"},
			Mention: MentionConfig{
				Default:            "@nimblegate-bot",
				IncludePRAssignees: true,
				AttemptsPerBot:     2,
			},
			LoopCfg: notification.LoopConfig{MaxAttempts: 5},
			Cooldown: notification.CooldownConfig{
				ThresholdCount:  3,
				ThresholdWindow: 5 * time.Minute,
				Duration:        10 * time.Minute,
			},
			Delivery: DeliveryConfig{
				MaxAttempts:     20,
				BackoffSchedule: defaultDeliveryBackoff(),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, "demo", "gateway.toml"))
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{
		"upstream-url",
		"protected-refs",
		"observe-pr-comments",
		"auth-mode",
		"auth-header",
		"include-pr-assignees",
		"attempts-per-bot",
		"rotate-on-repeat-finding",
		"fallback-human",
		"max-attempts",
		"cooldown-threshold-count",
		"cooldown-threshold-window",
		"cooldown-duration",
		"backoff-schedule",
	}
	bodyStr := string(body)
	for _, k := range wantKeys {
		if !containsKey(bodyStr, k) {
			t.Errorf("missing kebab-case key %q in TOML output:\n%s", k, bodyStr)
		}
	}
	// Snake-case slip-up canaries - none of these should appear.
	for _, bad := range []string{"upstream_url", "protected_refs", "auth_mode", "max_attempts", "backoff_schedule"} {
		if containsKey(bodyStr, bad) {
			t.Errorf("snake_case key %q must not appear in TOML output", bad)
		}
	}
}

func containsKey(body, key string) bool {
	// TOML keys are followed by " =" or "=" on their line.
	for _, sep := range []string{key + " =", key + "="} {
		if indexOf(body, sep) >= 0 {
			return true
		}
	}
	return false
}

// indexOf is a thin strings.Index - kept inline so the test file stays
// independent of any package import beyond what's already used.
func indexOf(haystack, needle string) int {
	hn := len(needle)
	for i := 0; i+hn <= len(haystack); i++ {
		if haystack[i:i+hn] == needle {
			return i
		}
	}
	return -1
}

func TestFilePolicyStore_Load_BadDurationReturnsError(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url = "u"
enabled      = true

[notification]
enabled = true

[notification.loop]
cooldown-duration = "not-a-duration"
`)
	s := FilePolicyStore{Root: root}
	if _, err := s.Load("demo"); err == nil {
		t.Error("Load must error on malformed duration string")
	}
}

func TestFilePolicyStore_Load_BadBackoffDurationReturnsError(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url = "u"
enabled      = true

[notification]
enabled = true

[notification.delivery]
backoff-schedule = ["1m", "garbage"]
`)
	s := FilePolicyStore{Root: root}
	if _, err := s.Load("demo"); err == nil {
		t.Error("Load must error on malformed backoff duration")
	}
}

// ----- Task 26: NotificationConfig.Validate -----

// validConfig is a known-good baseline that tests then mutate into invalid
// shapes - keeps each test focused on the single rule it exercises and
// proves the baseline produces a clean ValidationResult.
func validConfig() NotificationConfig {
	return NotificationConfig{
		Enabled:    true,
		WebhookURL: "https://hooks.example.com/in",
		WebhookAuth: notification.WebhookAuth{
			Mode:   "hmac",
			Secret: "shh",
		},
		Mention: MentionConfig{
			Default:            "@nimblegate-bot",
			IncludePRAssignees: true,
			AttemptsPerBot:     2,
		},
		LoopCfg: notification.LoopConfig{
			MaxAttempts:    5,
			AttemptsPerBot: 2,
			DefaultMention: "@nimblegate-bot",
		},
		Cooldown: notification.CooldownConfig{
			ThresholdCount:  3,
			ThresholdWindow: 5 * time.Minute,
			Duration:        10 * time.Minute,
		},
		Delivery: DeliveryConfig{
			MaxAttempts:     20,
			BackoffSchedule: defaultDeliveryBackoff(),
		},
	}
}

func TestNotificationConfig_Validate_ValidProducesNothing(t *testing.T) {
	r := validConfig().Validate()
	if !r.OK() {
		t.Errorf("valid config produced errors: %v", r.Errors)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("valid config produced warnings: %v", r.Warnings)
	}
}

func TestNotificationConfig_Validate_HMACWithoutSecretIsError(t *testing.T) {
	c := validConfig()
	c.WebhookAuth.Secret = ""
	r := c.Validate()
	if r.OK() {
		t.Fatal("hmac + empty secret must be a hard error")
	}
	found := false
	for _, e := range r.Errors {
		if indexOf(e, "auth-mode=hmac") >= 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected hmac-secret error, got %v", r.Errors)
	}
}

func TestNotificationConfig_Validate_HMACNoSecretButNoURLIsOK(t *testing.T) {
	// Webhook disabled (URL=="") means the HMAC mode never gets used, so an
	// empty secret with default hmac mode must NOT error - this is the
	// default state when [notification.webhook] is absent.
	c := validConfig()
	c.WebhookURL = ""
	c.WebhookAuth.Secret = ""
	r := c.Validate()
	// Must not flag hmac-secret error.
	for _, e := range r.Errors {
		if indexOf(e, "auth-mode=hmac") >= 0 {
			t.Errorf("hmac-secret error should not fire when webhook URL is empty: %v", r.Errors)
		}
	}
}

func TestNotificationConfig_Validate_SingleBotRotationUnreachableIsError(t *testing.T) {
	c := validConfig()
	c.Mention.RotationBots = []string{"@one"}
	c.Mention.AttemptsPerBot = 6
	c.LoopCfg.AttemptsPerBot = 6
	c.LoopCfg.MaxAttempts = 5 // < attempts-per-bot
	r := c.Validate()
	if r.OK() {
		t.Fatal("single-bot rotation with attempts-per-bot > max-attempts must error (unreachable rotation)")
	}
	found := false
	for _, e := range r.Errors {
		if indexOf(e, "unreachable") >= 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected unreachable-rotation error, got %v", r.Errors)
	}
}

func TestNotificationConfig_Validate_MultiBotRotationDoesNotErrorOnAttemptsPerBot(t *testing.T) {
	// Same numbers, but with 2+ bots the per-bot threshold rotates instead of stalling.
	c := validConfig()
	c.Mention.RotationBots = []string{"@one", "@two"}
	c.Mention.AttemptsPerBot = 6
	c.LoopCfg.AttemptsPerBot = 6
	c.LoopCfg.MaxAttempts = 5
	r := c.Validate()
	for _, e := range r.Errors {
		if indexOf(e, "unreachable") >= 0 {
			t.Errorf("multi-bot rotation must not trigger unreachable error: %v", r.Errors)
		}
	}
}

func TestNotificationConfig_Validate_EmptyWebhookURLWhenEnabledIsWarning(t *testing.T) {
	c := validConfig()
	c.WebhookURL = ""
	c.WebhookAuth.Secret = "" // avoid the hmac+secret error noise
	r := c.Validate()
	if !r.OK() {
		t.Fatalf("empty webhook URL must be warning only, got errors: %v", r.Errors)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("expected a warning for enabled+empty-webhook")
	}
	found := false
	for _, w := range r.Warnings {
		if indexOf(w, "webhook.url is empty") >= 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected empty-webhook warning, got %v", r.Warnings)
	}
}

func TestNotificationConfig_Validate_FallbackHumanWithoutAtPrefixIsWarning(t *testing.T) {
	c := validConfig()
	c.Mention.FallbackHuman = "oncall" // missing @
	r := c.Validate()
	if !r.OK() {
		t.Fatalf("non-@ fallback must be warning only, got errors: %v", r.Errors)
	}
	found := false
	for _, w := range r.Warnings {
		if indexOf(w, "fallback-human") >= 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected fallback-human warning, got %v", r.Warnings)
	}
}

func TestNotificationConfig_Validate_FallbackHumanWithAtPrefixIsOK(t *testing.T) {
	c := validConfig()
	c.Mention.FallbackHuman = "@oncall"
	r := c.Validate()
	for _, w := range r.Warnings {
		if indexOf(w, "fallback-human") >= 0 {
			t.Errorf("@-prefixed fallback-human must not warn: %v", r.Warnings)
		}
	}
}

func TestNotificationConfig_Validate_NegativeBackoffIsError(t *testing.T) {
	c := validConfig()
	c.Delivery.BackoffSchedule = []time.Duration{time.Minute, -1 * time.Second, 10 * time.Minute}
	r := c.Validate()
	if r.OK() {
		t.Fatal("negative backoff must be a hard error")
	}
	found := false
	for _, e := range r.Errors {
		if indexOf(e, "backoff-schedule[1]") >= 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("expected backoff[1] error, got %v", r.Errors)
	}
}

func TestNotificationConfig_Validate_ZeroBackoffIsError(t *testing.T) {
	c := validConfig()
	c.Delivery.BackoffSchedule = []time.Duration{0, time.Minute}
	r := c.Validate()
	if r.OK() {
		t.Fatal("zero backoff must be a hard error")
	}
}

// Load wires Validate: a TOML that parses cleanly but fails validation
// (hmac + empty secret + non-empty URL) must be rejected at Load time so the
// daemon never starts with a broken rail.
func TestFilePolicyStore_Load_HMACWithoutSecretIsRejected(t *testing.T) {
	root := t.TempDir()
	writeRepoTOML(t, root, "demo", `
upstream-url = "u"
enabled      = true

[notification]
enabled = true

[notification.webhook]
url       = "https://hooks.example.com/in"
auth-mode = "hmac"
secret    = ""
`)
	s := FilePolicyStore{Root: root}
	if _, err := s.Load("demo"); err == nil {
		t.Error("Load must reject hmac+empty-secret when webhook URL is set")
	}
}
