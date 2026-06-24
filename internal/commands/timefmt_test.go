// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"strings"
	"testing"
	"time"
)

func TestFormatRelativeTimestamp(t *testing.T) {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ts   string
		want string
	}{
		{"empty input passes through", "", ""},
		{"30 seconds → just now", "2026-05-17T11:59:30Z", "just now"},
		{"2 minutes → 2m ago", "2026-05-17T11:58:00Z", "2m ago"},
		{"3 hours → 3h ago", "2026-05-17T09:00:00Z", "3h ago"},
		{"2 days → 2d ago", "2026-05-15T12:00:00Z", "2d ago"},
		{"7 days → absolute date", "2026-05-10T12:00:00Z", "2026-05-10"},
		{"future clock skew → absolute", "2027-01-01T00:00:00Z", "2027-01-01"},
		{"malformed input passes through", "not-a-timestamp", "not-a-timestamp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatRelativeTimestamp(tc.ts, now)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatWatchTimestamp(t *testing.T) {
	ts := "2026-05-17T12:34:56.789012345Z"
	got := formatWatchTimestamp(ts)
	// Local time conversion means we can't assert exact value, but it
	// must contain the millisecond fragment and look like HH:MM:SS.mmm.
	if !strings.Contains(got, ":34:56.789") {
		t.Errorf("watch timestamp doesn't contain HH:MM:SS.mmm form: %q", got)
	}
}

func TestFormatWatchTimestamp_EmptyAndMalformed(t *testing.T) {
	if formatWatchTimestamp("") != "" {
		t.Error("empty input should round-trip")
	}
	if formatWatchTimestamp("garbage") != "garbage" {
		t.Error("malformed input should round-trip")
	}
}
