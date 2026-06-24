// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import "testing"

func TestIsGated(t *testing.T) {
	cases := []struct {
		in   Lifecycle
		want bool
	}{
		{LifecycleActive, true},
		{LifecycleCandidate, true},
		{"", true}, // empty defaults to active for backward compat
		{LifecycleProposed, false},
		{LifecycleDeprecated, false},
		{LifecycleArchived, false},
		{Lifecycle("nonsense"), false},
	}
	for _, c := range cases {
		if got := IsGated(c.in); got != c.want {
			t.Errorf("IsGated(%q): got %v, want %v", c.in, got, c.want)
		}
	}
}
