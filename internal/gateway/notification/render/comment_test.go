// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package render

import (
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway/notification"
)

func baseNotification() notification.Notification {
	return notification.Notification{
		SchemaVersion: "1.0",
		Event:         "push.rejected",
		EventID:       "evt_test",
		Gateway:       notification.GatewayInfo{Name: "nimblegate", Version: "v0.1.0", InstanceID: "gw-host"},
		Repo:          notification.RepoInfo{Name: "nimblegate"},
		Push: notification.PushInfo{
			Timestamp:            time.Date(2026, 6, 4, 18, 23, 0, 0, time.UTC),
			PusherKeyFingerprint: "SHA256:abc",
			Refs:                 []notification.RefInfo{{Name: "refs/heads/main", OldSHA: "ca3f056", NewSHA: "5ea730c", Type: "update"}},
			PR:                   &notification.PRInfo{Number: 42},
		},
		Decision: notification.DecisionInfo{
			Findings: []notification.Finding{
				{FrameID: "security/no-private-keys-in-repo", Severity: "BLOCK", Message: "PEM EC private key found", File: "config/key.pem", Line: 1, Hint: "Move to env vars; rotate the key.", Fingerprint: "sha256:e7f8a9"},
			},
		},
	}
}

func TestRender_FirstReject_SingleBot(t *testing.T) {
	n := baseNotification()
	n.Mention = &notification.MentionInfo{Default: "@nimblegate-bot", CurrentBot: "@nimblegate-bot", AutoTaggedHumans: []string{"@alice"}}
	n.LoopState = &notification.LoopState{PRAttemptCount: 1, MaxAttempts: 5}

	got := Comment(n)

	mustContain(t, got,
		"## ⛔ Push rejected by nimblegate gateway",
		"**Repo:** `nimblegate`",
		"**Branch:** `refs/heads/main`",
		"**PR:** #42",
		"@nimblegate-bot",
		"@alice (assigned)",
		"### Must fix (1 BLOCK)",
		"`security/no-private-keys-in-repo`",
		"`config/key.pem:1`",
		"PEM EC private key found",
		"### Next steps",
		"attempt 1/5",
		MarkerStart,
		`"event_id":"evt_test"`, // hidden JSON block
	)
	mustNotContain(t, got, "🔄", "⛔⛔", "⚠ OBSERVE", "Previous attempts")
}

func TestRender_StickyUpdate_WithHistory(t *testing.T) {
	n := baseNotification()
	n.Mention = &notification.MentionInfo{Default: "@nimblegate-bot", CurrentBot: "@nimblegate-bot"}
	n.LoopState = &notification.LoopState{
		PRAttemptCount:        2,
		MaxAttempts:           5,
		SameFindingAsPrevious: true,
		PreviousAttempts: []notification.AttemptRecord{
			{SHA: "a1b2c3d", Timestamp: time.Date(2026, 6, 4, 18, 10, 0, 0, time.UTC), Bot: "@nimblegate-bot"},
		},
	}

	got := Comment(n)
	mustContain(t, got, "attempt 2/5", "Previous attempts (1", "`a1b2c3d`")
}

func TestRender_RotationBanner(t *testing.T) {
	n := baseNotification()
	n.Mention = &notification.MentionInfo{
		Default:    "@nimblegate-bot",
		CurrentBot: "@cursor-bot",
		Rotation: &notification.RotationInfo{
			Enabled:       true,
			AttemptIndex:  3,
			RotatedFrom:   "@claude-code-bot",
			RotatedReason: "same-finding",
		},
	}
	n.LoopState = &notification.LoopState{PRAttemptCount: 3, MaxAttempts: 5}

	got := Comment(n)
	mustContain(t, got,
		"🔄 **Rotated from @claude-code-bot** → @cursor-bot",
		"same finding as previous attempt",
		"@cursor-bot",
	)
}

func TestRender_LoopExhaustion(t *testing.T) {
	n := baseNotification()
	n.Mention = &notification.MentionInfo{CurrentBot: "@team-leads", FallbackActive: true}
	n.LoopState = &notification.LoopState{PRAttemptCount: 5, MaxAttempts: 5}

	got := Comment(n)
	mustContain(t, got,
		"## ⛔⛔ Loop exhausted",
		"@team-leads",
		"Human review required",
		"agents tried 5 times",
	)
	mustNotContain(t, got, "### Next steps") // exhaustion skips next-steps section
}

func TestRender_ObserveMode(t *testing.T) {
	n := baseNotification()
	n.Event = "push.observed"
	n.Decision.Observed = true
	n.Mention = &notification.MentionInfo{Default: "@nimblegate-bot", CurrentBot: "@nimblegate-bot"}

	got := Comment(n)
	mustContain(t, got,
		"## ⚠ OBSERVE mode",
		"would have rejected",
		"### Would have blocked (1 BLOCK)",
	)
	mustNotContain(t, got, "### Next steps") // observe mode skips next-steps section
}

func TestRender_HiddenJSONBlockAfterRenderedContent(t *testing.T) {
	n := baseNotification()
	n.Mention = &notification.MentionInfo{Default: "@nimblegate-bot", CurrentBot: "@nimblegate-bot"}
	body := Comment(n)

	markerIdx := strings.Index(body, MarkerStart)
	if markerIdx < 0 {
		t.Fatalf("hidden marker not found in body")
	}
	// Footer is the last rendered-markdown section - must come BEFORE the
	// hidden JSON block, so an agent parsing the marker finds JSON not prose.
	footerIdx := strings.Index(body, "*Posted by nimblegate")
	if footerIdx < 0 {
		t.Fatalf("footer not found in body")
	}
	if markerIdx < footerIdx {
		t.Errorf("hidden block at %d should come AFTER footer at %d", markerIdx, footerIdx)
	}
	// Marker must be followed by '-->' close tag - confirms the block is
	// well-formed HTML comment markup, not orphaned JSON.
	if !strings.Contains(body[markerIdx:], "-->") {
		t.Errorf("hidden block missing closing -->")
	}
}

func mustContain(t *testing.T, body string, expected ...string) {
	t.Helper()
	for _, e := range expected {
		if !strings.Contains(body, e) {
			t.Errorf("expected body to contain %q\n--- BODY ---\n%s\n--- END ---", e, body)
		}
	}
}

func mustNotContain(t *testing.T, body string, unexpected ...string) {
	t.Helper()
	for _, u := range unexpected {
		if strings.Contains(body, u) {
			t.Errorf("body should NOT contain %q\n--- BODY ---\n%s\n--- END ---", u, body)
		}
	}
}
