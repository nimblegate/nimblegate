// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// PRState lives at <policyRoot>/<repo>/pr-comment-state/<pr>.json - one file
// per active PR. Carries the sticky comment ID, the loop counter, the
// rotation index + per-bot attempt counts, the finding-fingerprint history.
//
// Lifecycle: created on first reject, updated on every subsequent reject for
// the same PR, deleted when the PR is accepted (or manually reset).
type PRState struct {
	SchemaVersion       string           `json:"schema_version"`
	PRNumber            int              `json:"pr_number"`
	Repo                string           `json:"repo"`
	Ref                 string           `json:"ref"`
	StickyComment       StickyCommentRef `json:"sticky_comment"`
	Loop                LoopCounters     `json:"loop"`
	Mention             MentionCounters  `json:"mention"`
	FindingFingerprints FingerprintTrack `json:"finding_fingerprints"`
	AttemptHistory      []HistoryEntry   `json:"attempt_history,omitempty"`
}

type StickyCommentRef struct {
	ID            string    `json:"id"`
	URL           string    `json:"url,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	LastUpdatedAt time.Time `json:"last_updated_at,omitempty"`
}

type LoopCounters struct {
	AttemptCount  int       `json:"attempt_count"`
	MaxAttempts   int       `json:"max_attempts"`
	Paused        bool      `json:"paused,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	Exhausted     bool      `json:"exhausted,omitempty"`
}

type MentionCounters struct {
	CurrentBot     string         `json:"current_bot,omitempty"`
	PerBotAttempts map[string]int `json:"per_bot_attempts,omitempty"`
	RotationIndex  int            `json:"rotation_index"`
	FallbackActive bool           `json:"fallback_active,omitempty"`
}

type FingerprintTrack struct {
	PreviousAttempt  []string `json:"previous_attempt,omitempty"`
	AllAttemptsUnion []string `json:"all_attempts_union,omitempty"`
}

type HistoryEntry struct {
	N                   int       `json:"n"`
	SHA                 string    `json:"sha"`
	Timestamp           time.Time `json:"ts"`
	Bot                 string    `json:"bot,omitempty"`
	FindingFingerprints []string  `json:"finding_fingerprints,omitempty"`
	RotatedAfter        bool      `json:"rotated_after,omitempty"`
	RotationReason      string    `json:"rotation_reason,omitempty"`
}

func statePath(policyRoot, repo string, pr int) string {
	return filepath.Join(policyRoot, repo, "pr-comment-state", fmt.Sprintf("%d.json", pr))
}

// WritePRState atomically writes s for (repo, pr) via temp + rename.
// Creates the pr-comment-state/ dir if absent.
func WritePRState(policyRoot, repo string, pr int, s PRState) error {
	if s.SchemaVersion == "" {
		s.SchemaVersion = "1.0"
	}
	dir := filepath.Join(policyRoot, repo, "pr-comment-state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := statePath(policyRoot, repo, pr)
	tmp := path + ".tmp"
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadPRState returns the state for (repo, pr) or nil if it doesn't exist
// (no active loop on this PR yet, or already cleared by an accepted push).
func ReadPRState(policyRoot, repo string, pr int) (*PRState, error) {
	path := statePath(policyRoot, repo, pr)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s PRState
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// DeletePRState clears the state for (repo, pr) - called after the PR's
// push is accepted (loop closed), or by the operator via the dashboard's
// "Reset loop" button.
func DeletePRState(policyRoot, repo string, pr int) error {
	path := statePath(policyRoot, repo, pr)
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// ListLoopsForRef returns the per-PR loop states whose Ref matches the given
// ref - the active fix-loops on the branch a clean push just landed on. The
// caller uses each state's PRNumber + sticky comment id to fire a resolution
// (✅ comment update) and then clears the state. Best-effort and tolerant:
// unreadable or malformed files are skipped, and a missing state dir yields an
// empty slice + no error.
func ListLoopsForRef(policyRoot, repo, ref string) ([]PRState, error) {
	dir := filepath.Join(policyRoot, repo, "pr-comment-state")
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	var out []PRState
	for _, m := range matches {
		b, rerr := os.ReadFile(m)
		if rerr != nil {
			continue
		}
		var s PRState
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		if s.Ref == ref {
			out = append(out, s)
		}
	}
	return out, nil
}
