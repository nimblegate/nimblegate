// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"time"
)

// formatRelativeTimestamp renders an RFC3339Nano timestamp string as a
// human-friendly relative form: "2m ago", "3h ago", "5d ago", or
// "2026-05-15" if the entry is older than 7 days.
//
// Used by `nimblegate status` (LAST column) so users can scan recency at
// a glance instead of decoding RFC3339Nano. The on-disk audit log
// retains RFC3339Nano - this only changes display.
//
// If the input cannot be parsed, returns it unchanged so output never
// shows an empty cell when the upstream data is malformed.
func formatRelativeTimestamp(ts string, now time.Time) string {
	if ts == "" {
		return ts
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		// Tolerate older formats / corruption - never break status output.
		return ts
	}
	return relativeFromDelta(now.Sub(t), t)
}

func relativeFromDelta(d time.Duration, t time.Time) string {
	switch {
	case d < -time.Second:
		// Future timestamps (clock skew). Fall through to absolute.
		return t.Format("2006-01-02")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// formatWatchTimestamp renders an RFC3339Nano timestamp as a short
// local-time prefix for `nimblegate watch` output: HH:MM:SS.mmm.
// When watching live, the date is obvious and nanoseconds are noise.
func formatWatchTimestamp(ts string) string {
	if ts == "" {
		return ts
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("15:04:05.000")
}
