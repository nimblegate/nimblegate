// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"testing"

	"nimblegate/internal/gateway/upstream"
)

// TestApplyLoopState_PopulatesRotationBanner verifies the rotation info that
// Transition records in PRState is surfaced onto the notification, so
// render's "🔄 Rotated from …" banner can fire.
func TestApplyLoopState_PopulatesRotationBanner(t *testing.T) {
	s := PRState{
		Loop:    LoopCounters{AttemptCount: 3, MaxAttempts: 5},
		Mention: MentionCounters{CurrentBot: "@bot-b"},
		AttemptHistory: []HistoryEntry{
			{N: 1, Bot: "@bot-a"},
			{N: 2, Bot: "@bot-a"},
			{N: 3, Bot: "@bot-b", RotatedAfter: true, RotationReason: "attempt-threshold"},
		},
	}
	var n Notification
	applyLoopState(&n, s, upstream.PRPeople{})

	if n.Mention == nil || n.Mention.Rotation == nil {
		t.Fatal("expected Mention.Rotation to be populated after a rotation")
	}
	r := n.Mention.Rotation
	if r.RotatedFrom != "@bot-a" {
		t.Errorf("RotatedFrom = %q, want @bot-a", r.RotatedFrom)
	}
	if r.RotatedReason != "attempt-threshold" {
		t.Errorf("RotatedReason = %q, want attempt-threshold", r.RotatedReason)
	}
	if r.AttemptIndex != 3 {
		t.Errorf("AttemptIndex = %d, want 3", r.AttemptIndex)
	}
	if n.Mention.CurrentBot != "@bot-b" {
		t.Errorf("CurrentBot = %q, want @bot-b", n.Mention.CurrentBot)
	}

	// No rotation on the last attempt → no banner.
	s2 := PRState{
		Loop:           LoopCounters{AttemptCount: 2, MaxAttempts: 5},
		Mention:        MentionCounters{CurrentBot: "@bot-a"},
		AttemptHistory: []HistoryEntry{{N: 1, Bot: "@bot-a"}, {N: 2, Bot: "@bot-a"}},
	}
	var n2 Notification
	applyLoopState(&n2, s2, upstream.PRPeople{})
	if n2.Mention != nil && n2.Mention.Rotation != nil {
		t.Error("did not expect a rotation banner when the last attempt did not rotate")
	}
}
