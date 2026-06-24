// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"testing"
	"time"
)

func TestTransition_FirstReject_NoState_CreatesInitial(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 5, AttemptsPerBot: 2, RotationBots: []string{"@a", "@b"}, RotateOnRepeatFinding: true}
	got := Transition(nil, RejectEvent{
		Findings: []Finding{{Fingerprint: "fp1"}},
		PushSHA:  "sha1",
	}, cfg)
	if got.Loop.AttemptCount != 1 {
		t.Errorf("first reject should set attempt 1, got %d", got.Loop.AttemptCount)
	}
	if got.Mention.CurrentBot != "@a" {
		t.Errorf("first reject should use bots[0], got %q", got.Mention.CurrentBot)
	}
	if got.Mention.PerBotAttempts["@a"] != 1 {
		t.Errorf("per-bot count should be 1, got %d", got.Mention.PerBotAttempts["@a"])
	}
}

func TestTransition_SecondRejectUnderThreshold_SameBot(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 5, AttemptsPerBot: 2, RotationBots: []string{"@a", "@b"}}
	state := &PRState{
		Loop:    LoopCounters{AttemptCount: 1, MaxAttempts: 5},
		Mention: MentionCounters{CurrentBot: "@a", PerBotAttempts: map[string]int{"@a": 1}},
	}
	got := Transition(state, RejectEvent{Findings: []Finding{{Fingerprint: "fp2"}}, PushSHA: "sha2"}, cfg)
	if got.Mention.CurrentBot != "@a" {
		t.Errorf("under threshold should stay on @a, got %q", got.Mention.CurrentBot)
	}
	if got.Mention.PerBotAttempts["@a"] != 2 {
		t.Errorf("per-bot count should be 2, got %d", got.Mention.PerBotAttempts["@a"])
	}
}

func TestTransition_SameFindingTriggersRotation(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 5, AttemptsPerBot: 5, RotationBots: []string{"@a", "@b"}, RotateOnRepeatFinding: true}
	state := &PRState{
		Loop:                LoopCounters{AttemptCount: 1, MaxAttempts: 5},
		Mention:             MentionCounters{CurrentBot: "@a", PerBotAttempts: map[string]int{"@a": 1}},
		FindingFingerprints: FingerprintTrack{PreviousAttempt: []string{"fp1"}},
	}
	got := Transition(state, RejectEvent{Findings: []Finding{{Fingerprint: "fp1"}}, PushSHA: "sha2"}, cfg)
	if got.Mention.CurrentBot != "@b" {
		t.Errorf("same finding should rotate to @b, got %q", got.Mention.CurrentBot)
	}
}

func TestTransition_PerBotThresholdRotation(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 5, AttemptsPerBot: 2, RotationBots: []string{"@a", "@b"}}
	state := &PRState{
		Loop:    LoopCounters{AttemptCount: 2, MaxAttempts: 5},
		Mention: MentionCounters{CurrentBot: "@a", PerBotAttempts: map[string]int{"@a": 2}},
	}
	got := Transition(state, RejectEvent{Findings: []Finding{{Fingerprint: "fp3"}}, PushSHA: "sha3"}, cfg)
	if got.Mention.CurrentBot != "@b" {
		t.Errorf("threshold reached should rotate to @b, got %q", got.Mention.CurrentBot)
	}
}

func TestTransition_LoopExhaustion(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 3, AttemptsPerBot: 1, RotationBots: []string{"@a", "@b", "@c"}, FallbackHuman: "@team"}
	state := &PRState{
		Loop:    LoopCounters{AttemptCount: 2, MaxAttempts: 3},
		Mention: MentionCounters{CurrentBot: "@b", RotationIndex: 1, PerBotAttempts: map[string]int{"@a": 1, "@b": 1}},
	}
	got := Transition(state, RejectEvent{Findings: []Finding{{Fingerprint: "fp"}}, PushSHA: "sha"}, cfg)
	if !got.Loop.Exhausted {
		t.Errorf("attempt_count==max should mark exhausted, got %+v", got.Loop)
	}
	if !got.Mention.FallbackActive {
		t.Errorf("exhaustion should activate fallback, got %+v", got.Mention)
	}
	if got.Mention.CurrentBot != "@team" {
		t.Errorf("exhausted mention should be fallback_human, got %q", got.Mention.CurrentBot)
	}
}

func TestTransition_RotationDisabledStaysOnDefault(t *testing.T) {
	cfg := LoopConfig{MaxAttempts: 5, AttemptsPerBot: 2, RotationBots: nil, DefaultMention: "@nimblegate-bot"}
	state := &PRState{
		Loop:    LoopCounters{AttemptCount: 2, MaxAttempts: 5},
		Mention: MentionCounters{CurrentBot: "@nimblegate-bot", PerBotAttempts: map[string]int{"@nimblegate-bot": 2}},
	}
	got := Transition(state, RejectEvent{Findings: []Finding{{Fingerprint: "fp"}}, PushSHA: "sha"}, cfg)
	if got.Mention.CurrentBot != "@nimblegate-bot" {
		t.Errorf("rotation disabled should stay on default, got %q", got.Mention.CurrentBot)
	}
}

func TestCooldown_BelowThresholdNoTrigger(t *testing.T) {
	cfg := CooldownConfig{ThresholdCount: 3, ThresholdWindow: 5 * time.Minute, Duration: 10 * time.Minute}
	history := []HistoryEntry{
		{N: 1, Timestamp: time.Date(2026, 6, 4, 18, 0, 0, 0, time.UTC)},
		{N: 2, Timestamp: time.Date(2026, 6, 4, 18, 1, 0, 0, time.UTC)},
	}
	got := ComputeCooldown(history, time.Date(2026, 6, 4, 18, 2, 0, 0, time.UTC), cfg)
	if !got.IsZero() {
		t.Errorf("2 rejects in 2min, threshold 3, should not cool down, got %v", got)
	}
}

func TestCooldown_ThresholdReachedSetsUntil(t *testing.T) {
	cfg := CooldownConfig{ThresholdCount: 3, ThresholdWindow: 5 * time.Minute, Duration: 10 * time.Minute}
	now := time.Date(2026, 6, 4, 18, 2, 0, 0, time.UTC)
	history := []HistoryEntry{
		{N: 1, Timestamp: now.Add(-3 * time.Minute)},
		{N: 2, Timestamp: now.Add(-2 * time.Minute)},
		{N: 3, Timestamp: now},
	}
	got := ComputeCooldown(history, now, cfg)
	want := now.Add(10 * time.Minute)
	if !got.Equal(want) {
		t.Errorf("3 rejects within window should set cooldown to now+10min, got %v want %v", got, want)
	}
}

func TestCooldown_OldRejectsOutsideWindowIgnored(t *testing.T) {
	cfg := CooldownConfig{ThresholdCount: 3, ThresholdWindow: 5 * time.Minute, Duration: 10 * time.Minute}
	now := time.Date(2026, 6, 4, 18, 2, 0, 0, time.UTC)
	history := []HistoryEntry{
		{N: 1, Timestamp: now.Add(-1 * time.Hour)}, // outside window
		{N: 2, Timestamp: now.Add(-1 * time.Minute)},
		{N: 3, Timestamp: now},
	}
	got := ComputeCooldown(history, now, cfg)
	if !got.IsZero() {
		t.Errorf("only 2 rejects within window, should not cool down, got %v", got)
	}
}
