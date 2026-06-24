// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"testing"
	"time"
)

func TestPRState_WriteThenRead(t *testing.T) {
	dir := t.TempDir()
	s := PRState{
		SchemaVersion: "1.0",
		PRNumber:      42,
		Repo:          "nimblegate",
		Ref:           "refs/heads/main",
		StickyComment: StickyCommentRef{
			ID:            "comment_4598721",
			URL:           "https://github.com/.../#issuecomment-4598721",
			CreatedAt:     time.Date(2026, 6, 4, 18, 23, 45, 0, time.UTC),
			LastUpdatedAt: time.Date(2026, 6, 4, 18, 42, 10, 0, time.UTC),
		},
		Loop: LoopCounters{AttemptCount: 3, MaxAttempts: 5},
		Mention: MentionCounters{
			CurrentBot:     "@cursor-bot",
			PerBotAttempts: map[string]int{"@claude-code-bot": 2, "@cursor-bot": 1},
			RotationIndex:  1,
		},
		FindingFingerprints: FingerprintTrack{
			PreviousAttempt:  []string{"sha256:e7f8a9"},
			AllAttemptsUnion: []string{"sha256:e7f8a9"},
		},
	}
	if err := WritePRState(dir, "nimblegate", 42, s); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadPRState(dir, "nimblegate", 42)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.PRNumber != 42 || got.StickyComment.ID != "comment_4598721" {
		t.Errorf("roundtrip wrong: %+v", got)
	}
	if got.Mention.PerBotAttempts["@claude-code-bot"] != 2 {
		t.Errorf("per-bot map wrong: %+v", got.Mention)
	}
}

func TestPRState_ReadMissingReturnsNilNoErr(t *testing.T) {
	got, err := ReadPRState(t.TempDir(), "nimblegate", 999)
	if err != nil {
		t.Errorf("missing state should be nil, nil - got err: %v", err)
	}
	if got != nil {
		t.Errorf("missing state should be nil, got %+v", got)
	}
}

func TestPRState_DeletePRState(t *testing.T) {
	dir := t.TempDir()
	_ = WritePRState(dir, "nimblegate", 42, PRState{PRNumber: 42})
	if err := DeletePRState(dir, "nimblegate", 42); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, _ := ReadPRState(dir, "nimblegate", 42)
	if got != nil {
		t.Errorf("after delete state should be nil, got %+v", got)
	}
}
