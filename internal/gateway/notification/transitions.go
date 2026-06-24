// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import "time"

// LoopConfig is the subset of per-repo notification config that drives the
// state machine (per spec §6.3). Source: gateway.toml [notification.loop] +
// [notification.mention] + [notification.mention.rotation].
type LoopConfig struct {
	MaxAttempts           int
	AttemptsPerBot        int
	RotationBots          []string // empty = rotation disabled
	RotateOnRepeatFinding bool
	FallbackHuman         string // empty = no fallback; loop exhaustion just stops mentioning bots
	DefaultMention        string // used when RotationBots is empty
}

// RejectEvent is the input to Transition: what just got rejected and why.
type RejectEvent struct {
	Findings []Finding
	PushSHA  string
	Now      time.Time // if zero, time.Now().UTC() is used
}

// CooldownConfig is the subset of [notification.loop] that drives cooldown
// per spec §6.5.
type CooldownConfig struct {
	ThresholdCount  int
	ThresholdWindow time.Duration
	Duration        time.Duration
}

// Transition is the PR loop state machine per spec §6.2. Pure function: given
// the previous state (nil = no prior loop on this PR) and the reject event,
// returns the next state. Caller persists via WritePRState.
//
// The transition order matters: (1) rotation triggers (same-finding fast OR
// per-bot threshold), (2) exhaustion check (if rotation would advance past
// the last bot and fallback_human is configured), (3) update counters + history.
func Transition(prev *PRState, ev RejectEvent, cfg LoopConfig) PRState {
	now := ev.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Fresh PR - first reject creates the initial state.
	if prev == nil {
		bot := cfg.DefaultMention
		if len(cfg.RotationBots) > 0 {
			bot = cfg.RotationBots[0]
		}
		return PRState{
			SchemaVersion: "1.0",
			Loop:          LoopCounters{AttemptCount: 1, MaxAttempts: cfg.MaxAttempts},
			Mention: MentionCounters{
				CurrentBot:     bot,
				PerBotAttempts: map[string]int{bot: 1},
				RotationIndex:  0,
			},
			FindingFingerprints: FingerprintTrack{
				PreviousAttempt:  collectFingerprints(ev.Findings),
				AllAttemptsUnion: collectFingerprints(ev.Findings),
			},
			AttemptHistory: []HistoryEntry{{N: 1, SHA: ev.PushSHA, Timestamp: now, Bot: bot, FindingFingerprints: collectFingerprints(ev.Findings)}},
		}
	}

	// Subsequent reject: advance attempt count first.
	next := *prev
	next.Loop.AttemptCount++
	if next.Loop.MaxAttempts == 0 {
		next.Loop.MaxAttempts = cfg.MaxAttempts
	}

	// Decide rotation. Two triggers, first match wins. Skipped entirely
	// when rotation is disabled (no RotationBots configured).
	rotated := false
	rotationReason := ""
	if len(cfg.RotationBots) > 0 {
		newFingerprints := collectFingerprints(ev.Findings)
		if cfg.RotateOnRepeatFinding && hasOverlap(prev.FindingFingerprints.PreviousAttempt, newFingerprints) {
			rotated, rotationReason = true, "same-finding"
		} else if prev.Mention.PerBotAttempts[prev.Mention.CurrentBot] >= cfg.AttemptsPerBot {
			rotated, rotationReason = true, "attempt-threshold"
		}
	}

	if rotated {
		nextIdx := next.Mention.RotationIndex + 1
		if nextIdx >= len(cfg.RotationBots) {
			// Past the last bot - exhaustion if a fallback is configured.
			if cfg.FallbackHuman != "" {
				next.Loop.Exhausted = true
				next.Mention.FallbackActive = true
				next.Mention.CurrentBot = cfg.FallbackHuman
			} else {
				// No fallback: wrap around (start over with bot 0).
				nextIdx = 0
				next.Mention.RotationIndex = nextIdx
				next.Mention.CurrentBot = cfg.RotationBots[nextIdx]
				next.Mention.PerBotAttempts[next.Mention.CurrentBot] = 1
			}
		} else {
			next.Mention.RotationIndex = nextIdx
			next.Mention.CurrentBot = cfg.RotationBots[nextIdx]
			if next.Mention.PerBotAttempts == nil {
				next.Mention.PerBotAttempts = map[string]int{}
			}
			next.Mention.PerBotAttempts[next.Mention.CurrentBot] = 1
		}
	} else {
		// No rotation: increment current bot's count.
		if next.Mention.PerBotAttempts == nil {
			next.Mention.PerBotAttempts = map[string]int{}
		}
		next.Mention.PerBotAttempts[next.Mention.CurrentBot]++
	}

	// Exhaustion check based on total attempts (independent of rotation).
	if next.Loop.AttemptCount >= cfg.MaxAttempts {
		next.Loop.Exhausted = true
		if cfg.FallbackHuman != "" {
			next.Mention.FallbackActive = true
			next.Mention.CurrentBot = cfg.FallbackHuman
		}
	}

	// Update fingerprint history.
	newFP := collectFingerprints(ev.Findings)
	next.FindingFingerprints.PreviousAttempt = newFP
	next.FindingFingerprints.AllAttemptsUnion = unionFingerprints(prev.FindingFingerprints.AllAttemptsUnion, newFP)

	// Append history entry.
	next.AttemptHistory = append(next.AttemptHistory, HistoryEntry{
		N:                   next.Loop.AttemptCount,
		SHA:                 ev.PushSHA,
		Timestamp:           now,
		Bot:                 next.Mention.CurrentBot,
		FindingFingerprints: newFP,
		RotatedAfter:        rotated,
		RotationReason:      rotationReason,
	})

	return next
}

// ComputeCooldown returns the cooldown-until timestamp when enough rejects
// (ThresholdCount) have arrived within ThresholdWindow ending at now.
// Returns the zero value when the threshold isn't met. Cooldown is per-PR:
// during the cooldown period, queue records still append for audit but the
// daemon skips delivery and the sticky comment is not updated.
func ComputeCooldown(history []HistoryEntry, now time.Time, cfg CooldownConfig) time.Time {
	if cfg.ThresholdCount <= 0 || cfg.ThresholdWindow == 0 {
		return time.Time{}
	}
	cutoff := now.Add(-cfg.ThresholdWindow)
	count := 0
	for _, h := range history {
		if h.Timestamp.After(cutoff) || h.Timestamp.Equal(cutoff) {
			count++
		}
	}
	if count >= cfg.ThresholdCount {
		return now.Add(cfg.Duration)
	}
	return time.Time{}
}

func collectFingerprints(findings []Finding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.Fingerprint != "" {
			out = append(out, f.Fingerprint)
		}
	}
	return out
}

func hasOverlap(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; ok {
			return true
		}
	}
	return false
}

func unionFingerprints(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	for _, s := range b {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
