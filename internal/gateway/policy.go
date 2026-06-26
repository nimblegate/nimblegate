// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway/notification"
)

// PolicyStore loads/saves per-repo gateway policy.
type PolicyStore interface {
	Load(repo string) (Policy, error)
	Save(p Policy) error
}

// FilePolicyStore stores each repo's policy at <Root>/<repo>/gateway.toml.
type FilePolicyStore struct{ Root string }

func (s FilePolicyStore) dir(repo string) string  { return filepath.Join(s.Root, repo) }
func (s FilePolicyStore) file(repo string) string { return filepath.Join(s.dir(repo), "gateway.toml") }

type policyTOML struct {
	UpstreamURL         string            `toml:"upstream-url"`
	ProtectedRefs       []string          `toml:"protected-refs"`
	DeleteProtectedRefs []string          `toml:"delete-protected-refs,omitempty"`
	GateAllRefs         bool              `toml:"gate-all-refs,omitempty"`
	Enabled             bool              `toml:"enabled"`
	Observe             bool              `toml:"observe"`
	MaxInputSize        string            `toml:"max-input-size,omitempty"`
	Notification        *notificationTOML `toml:"notification,omitempty"`
}

// notificationTOML mirrors the [notification.*] sections of gateway.toml (spec §7.1).
// Kebab-case keys match the existing upstream-url / protected-refs convention.
// All sub-tables are pointers so an absent section reads back as nil and
// toConfig() can apply spec defaults lazily.
type notificationTOML struct {
	Enabled           bool          `toml:"enabled"`
	ObservePRComments bool          `toml:"observe-pr-comments"`
	Webhook           *webhookTOML  `toml:"webhook,omitempty"`
	Mention           *mentionTOML  `toml:"mention,omitempty"`
	Loop              *loopTOML     `toml:"loop,omitempty"`
	Delivery          *deliveryTOML `toml:"delivery,omitempty"`
}

type webhookTOML struct {
	URL        string `toml:"url"`
	AuthMode   string `toml:"auth-mode"`
	Secret     string `toml:"secret"`
	AuthHeader string `toml:"auth-header"`
}

type mentionTOML struct {
	Default            string        `toml:"default"`
	IncludePRAssignees bool          `toml:"include-pr-assignees"`
	Rotation           *rotationTOML `toml:"rotation,omitempty"`
}

type rotationTOML struct {
	Bots                  []string `toml:"bots"`
	AttemptsPerBot        int      `toml:"attempts-per-bot"`
	RotateOnRepeatFinding bool     `toml:"rotate-on-repeat-finding"`
	FallbackHuman         string   `toml:"fallback-human"`
}

type loopTOML struct {
	MaxAttempts             int    `toml:"max-attempts"`
	CooldownThresholdCount  int    `toml:"cooldown-threshold-count"`
	CooldownThresholdWindow string `toml:"cooldown-threshold-window"` // parsed via time.ParseDuration
	CooldownDuration        string `toml:"cooldown-duration"`         // parsed via time.ParseDuration
}

type deliveryTOML struct {
	MaxAttempts     int      `toml:"max-attempts"`
	BackoffSchedule []string `toml:"backoff-schedule"` // each parsed via time.ParseDuration
}

// Spec §7.1 defaults - kept as a single source of truth so Load and the tests
// agree on what an absent sub-section means.
const (
	defaultMentionHandle           = "@nimblegate-bot"
	defaultLoopMaxAttempts         = 5
	defaultCooldownThresholdCount  = 3
	defaultCooldownThresholdWindow = 5 * time.Minute
	defaultCooldownDuration        = 10 * time.Minute
	defaultDeliveryMaxAttempts     = 20
	defaultRotationAttemptsPerBot  = 2
	defaultWebhookAuthMode         = "hmac"
)

func defaultDeliveryBackoff() []time.Duration {
	return []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		30 * time.Minute,
		2 * time.Hour,
	}
}

func (s FilePolicyStore) Save(p Policy) error {
	if err := os.MkdirAll(s.dir(p.Repo), 0o755); err != nil {
		return fmt.Errorf("gateway: mkdir for %q: %w", p.Repo, err)
	}
	return writeGatewayTOML(s.file(p.Repo), p)
}

// writeGatewayTOML writes p as gateway.toml at path. Used by Save and by
// AddRepo, which writes to the lib path before the activation symlink exists
// (so Save's path-via-symlink wouldn't resolve yet).
//
// Mode 0600 because [notification.webhook] sections can carry HMAC / Bearer
// secrets. Matches the cred.go pattern: explicit 0600 on create, plus a
// follow-up Chmod so pre-existing files written by an older binary at the
// default 0644 get tightened on the next write.
func writeGatewayTOML(path string, p Policy) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(policyTOML{
		UpstreamURL:         p.UpstreamURL,
		ProtectedRefs:       p.ProtectedRefs,
		DeleteProtectedRefs: p.DeleteProtectedRefs,
		GateAllRefs:         p.GateAllRefs,
		Enabled:             p.Enabled,
		Observe:             p.Observe,
		MaxInputSize:        p.MaxInputSize,
		Notification:        notificationConfigToTOML(p.Notification),
	}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Enforce 0600 even if the file pre-existed with looser perms (e.g.,
	// written by an earlier binary that used os.Create's default mode).
	return os.Chmod(path, 0o600)
}

// SetEnabled flips a repo's gating on/off (gateway.toml enabled), preserving the
// rest of the policy. Errors if the repo isn't registered.
func (s FilePolicyStore) SetEnabled(repo string, enabled bool) error {
	p, err := s.Load(repo)
	if err != nil {
		return err
	}
	p.Enabled = enabled
	return s.Save(p)
}

// SetObserve flips a repo between enforce (observe=false, rejects on a
// would-block) and observe/advisory (observe=true, records the would-block but
// relays anyway), preserving the rest of the policy. Errors if the repo isn't
// registered.
func (s FilePolicyStore) SetObserve(repo string, observe bool) error {
	p, err := s.Load(repo)
	if err != nil {
		return err
	}
	p.Observe = observe
	return s.Save(p)
}

func (s FilePolicyStore) Load(repo string) (Policy, error) {
	var pt policyTOML
	if _, err := toml.DecodeFile(s.file(repo), &pt); err != nil {
		return Policy{}, fmt.Errorf("gateway: load policy for %q: %w", repo, err)
	}
	p := Policy{
		Repo:                repo,
		UpstreamURL:         pt.UpstreamURL,
		ProtectedRefs:       pt.ProtectedRefs,
		DeleteProtectedRefs: pt.DeleteProtectedRefs,
		GateAllRefs:         pt.GateAllRefs,
		Enabled:             pt.Enabled,
		Observe:             pt.Observe,
		MaxInputSize:        pt.MaxInputSize,
		PolicyDir:           s.dir(repo),
	}
	if err := ValidateReceiveCap(p.MaxInputSize); err != nil {
		return Policy{}, fmt.Errorf("gateway: policy for %q max-input-size: %w", repo, err)
	}
	if pt.Notification != nil {
		nc, err := pt.Notification.toConfig()
		if err != nil {
			return Policy{}, fmt.Errorf("gateway: notification config for %q: %w", repo, err)
		}
		// Fail-fast on hard errors per spec §7.6 - warnings are returned via
		// the per-config Validate() so callers (admin CLI / dashboard) can
		// surface them without aborting the Load.
		if vr := nc.Validate(); !vr.OK() {
			return Policy{}, fmt.Errorf("gateway: notification config for %q: %s", repo, strings.Join(vr.Errors, "; "))
		}
		p.Notification = &nc
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("gateway: policy for %q: %w", repo, err)
	}
	return p, nil
}

// toConfig converts the on-disk TOML shape to the runtime NotificationConfig,
// applying spec §7.1 defaults for any absent sub-section. UpstreamKind is left
// empty - the orchestrator's Registry.LookupByURL fills it in at wiring time.
func (t *notificationTOML) toConfig() (NotificationConfig, error) {
	nc := NotificationConfig{
		Enabled:           t.Enabled,
		ObservePRComments: t.ObservePRComments,
	}

	// [notification.webhook] - absent = webhook off but rail can still post PR comments.
	if t.Webhook != nil {
		nc.WebhookURL = t.Webhook.URL
		mode := t.Webhook.AuthMode
		if mode == "" {
			mode = defaultWebhookAuthMode
		}
		nc.WebhookAuth = notification.WebhookAuth{
			Mode:       mode,
			Secret:     t.Webhook.Secret,
			HeaderName: t.Webhook.AuthHeader,
		}
	} else {
		nc.WebhookAuth = notification.WebhookAuth{Mode: defaultWebhookAuthMode}
	}

	// [notification.mention] - defaults: nimblegate-bot + include assignees.
	mention := MentionConfig{
		Default:            defaultMentionHandle,
		IncludePRAssignees: true,
		AttemptsPerBot:     defaultRotationAttemptsPerBot,
	}
	if t.Mention != nil {
		if t.Mention.Default != "" {
			mention.Default = t.Mention.Default
		}
		mention.IncludePRAssignees = t.Mention.IncludePRAssignees
		if t.Mention.Rotation != nil {
			mention.RotationBots = t.Mention.Rotation.Bots
			if t.Mention.Rotation.AttemptsPerBot > 0 {
				mention.AttemptsPerBot = t.Mention.Rotation.AttemptsPerBot
			}
			mention.RotateOnRepeatFinding = t.Mention.Rotation.RotateOnRepeatFinding
			mention.FallbackHuman = t.Mention.Rotation.FallbackHuman
		}
	}
	nc.Mention = mention

	// [notification.loop] - defaults: 5 attempts, 3-in-5m cooldown threshold, 10m cooldown.
	loopMax := defaultLoopMaxAttempts
	thresholdCount := defaultCooldownThresholdCount
	thresholdWindow := defaultCooldownThresholdWindow
	cooldown := defaultCooldownDuration
	if t.Loop != nil {
		if t.Loop.MaxAttempts > 0 {
			loopMax = t.Loop.MaxAttempts
		}
		if t.Loop.CooldownThresholdCount > 0 {
			thresholdCount = t.Loop.CooldownThresholdCount
		}
		if t.Loop.CooldownThresholdWindow != "" {
			d, err := time.ParseDuration(t.Loop.CooldownThresholdWindow)
			if err != nil {
				return NotificationConfig{}, fmt.Errorf("loop.cooldown-threshold-window %q: %w", t.Loop.CooldownThresholdWindow, err)
			}
			thresholdWindow = d
		}
		if t.Loop.CooldownDuration != "" {
			d, err := time.ParseDuration(t.Loop.CooldownDuration)
			if err != nil {
				return NotificationConfig{}, fmt.Errorf("loop.cooldown-duration %q: %w", t.Loop.CooldownDuration, err)
			}
			cooldown = d
		}
	}
	nc.LoopCfg = notification.LoopConfig{
		MaxAttempts:           loopMax,
		AttemptsPerBot:        mention.AttemptsPerBot,
		RotationBots:          mention.RotationBots,
		RotateOnRepeatFinding: mention.RotateOnRepeatFinding,
		FallbackHuman:         mention.FallbackHuman,
		DefaultMention:        mention.Default,
	}
	nc.Cooldown = notification.CooldownConfig{
		ThresholdCount:  thresholdCount,
		ThresholdWindow: thresholdWindow,
		Duration:        cooldown,
	}

	// [notification.delivery] - defaults: 20 attempts, 1m/5m/30m/2h backoff.
	delivery := DeliveryConfig{
		MaxAttempts:     defaultDeliveryMaxAttempts,
		BackoffSchedule: defaultDeliveryBackoff(),
	}
	if t.Delivery != nil {
		if t.Delivery.MaxAttempts > 0 {
			delivery.MaxAttempts = t.Delivery.MaxAttempts
		}
		if len(t.Delivery.BackoffSchedule) > 0 {
			backoff := make([]time.Duration, 0, len(t.Delivery.BackoffSchedule))
			for i, s := range t.Delivery.BackoffSchedule {
				d, err := time.ParseDuration(s)
				if err != nil {
					return NotificationConfig{}, fmt.Errorf("delivery.backoff-schedule[%d] %q: %w", i, s, err)
				}
				backoff = append(backoff, d)
			}
			delivery.BackoffSchedule = backoff
		}
	}
	nc.Delivery = delivery

	return nc, nil
}

// notificationConfigToTOML mirrors toConfig in reverse for Save's roundtrip.
// Nil in → nil out (no [notification] section written).
func notificationConfigToTOML(nc *NotificationConfig) *notificationTOML {
	if nc == nil {
		return nil
	}
	t := &notificationTOML{
		Enabled:           nc.Enabled,
		ObservePRComments: nc.ObservePRComments,
		Webhook: &webhookTOML{
			URL:        nc.WebhookURL,
			AuthMode:   nc.WebhookAuth.Mode,
			Secret:     nc.WebhookAuth.Secret,
			AuthHeader: nc.WebhookAuth.HeaderName,
		},
		Mention: &mentionTOML{
			Default:            nc.Mention.Default,
			IncludePRAssignees: nc.Mention.IncludePRAssignees,
			Rotation: &rotationTOML{
				Bots:                  nc.Mention.RotationBots,
				AttemptsPerBot:        nc.Mention.AttemptsPerBot,
				RotateOnRepeatFinding: nc.Mention.RotateOnRepeatFinding,
				FallbackHuman:         nc.Mention.FallbackHuman,
			},
		},
		Loop: &loopTOML{
			MaxAttempts:             nc.LoopCfg.MaxAttempts,
			CooldownThresholdCount:  nc.Cooldown.ThresholdCount,
			CooldownThresholdWindow: nc.Cooldown.ThresholdWindow.String(),
			CooldownDuration:        nc.Cooldown.Duration.String(),
		},
		Delivery: &deliveryTOML{
			MaxAttempts:     nc.Delivery.MaxAttempts,
			BackoffSchedule: durationsToStrings(nc.Delivery.BackoffSchedule),
		},
	}
	return t
}

func durationsToStrings(ds []time.Duration) []string {
	out := make([]string, 0, len(ds))
	for _, d := range ds {
		out = append(out, d.String())
	}
	return out
}

// ValidationResult is the outcome of NotificationConfig.Validate (spec §7.6).
// Errors are hard config faults - the caller should refuse to start the rail
// with a misconfigured state. Warnings are soft signals - the rail can still
// run (operator may be mid-configuration) but the caller should log them.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// OK reports whether the config has no hard errors. Warnings do not block.
func (r ValidationResult) OK() bool { return len(r.Errors) == 0 }

// Validate checks the parsed notification config against spec §7.6 rules:
//
//   - HMAC auth-mode with empty secret              → ERROR (cannot sign)
//   - single-bot rotation + attempts-per-bot > loop.max-attempts → ERROR
//     (unreachable state - loop exhausts before per-bot threshold trips,
//     rotation can never advance, fallback never reached)
//   - delivery backoff schedule has a zero or negative duration → ERROR
//     (would burn through MaxAttempts instantly on first failure)
//   - enabled but webhook URL empty AND no upstream credential          → WARNING
//     (rail runs but webhook silently no-ops; PR-comment path still works
//     IF an upstream credential is present - that signal lives outside
//     this config, so we warn rather than error)
//   - fallback-human set but not @-prefixed → WARNING (likely typo)
//
// UpstreamKind is informational only - leave empty when calling pre-rail.
func (c NotificationConfig) Validate() ValidationResult {
	var r ValidationResult

	// ERROR: HMAC mode requires a secret.
	if c.WebhookURL != "" && c.WebhookAuth.Mode == "hmac" && c.WebhookAuth.Secret == "" {
		r.Errors = append(r.Errors, "notification.webhook: auth-mode=hmac requires a non-empty secret")
	}

	// ERROR: single-bot rotation with per-bot threshold above max-attempts is unreachable.
	if len(c.Mention.RotationBots) == 1 &&
		c.Mention.AttemptsPerBot > 0 &&
		c.LoopCfg.MaxAttempts > 0 &&
		c.Mention.AttemptsPerBot > c.LoopCfg.MaxAttempts {
		r.Errors = append(r.Errors,
			fmt.Sprintf("notification.mention.rotation: single bot with attempts-per-bot=%d > loop.max-attempts=%d is unreachable (rotation can never fire)",
				c.Mention.AttemptsPerBot, c.LoopCfg.MaxAttempts))
	}

	// ERROR: backoff schedule with non-positive durations would not space retries.
	for i, d := range c.Delivery.BackoffSchedule {
		if d <= 0 {
			r.Errors = append(r.Errors,
				fmt.Sprintf("notification.delivery.backoff-schedule[%d]=%s must be positive", i, d))
		}
	}

	// WARNING: rail enabled but webhook URL empty (PR-comment side still works
	// if an upstream credential is present, but the operator likely intended
	// both paths - flag for visibility).
	if c.Enabled && c.WebhookURL == "" {
		r.Warnings = append(r.Warnings,
			"notification: enabled=true but webhook.url is empty (PR-comment path only; verify upstream credential is configured)")
	}

	// WARNING: fallback-human should look like a handle (@-prefixed).
	if c.Mention.FallbackHuman != "" && !strings.HasPrefix(c.Mention.FallbackHuman, "@") {
		r.Warnings = append(r.Warnings,
			fmt.Sprintf("notification.mention.rotation.fallback-human=%q should start with @ (expected handle format)",
				c.Mention.FallbackHuman))
	}

	return r
}
