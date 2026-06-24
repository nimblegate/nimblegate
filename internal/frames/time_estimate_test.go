// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import "testing"

func TestEffectiveTimeCostHoursPrevented(t *testing.T) {
	cases := []struct {
		name       string
		fm         Frontmatter
		projVal    float64
		projSet    bool
		wantHours  float64
		wantSource TimeEstimateSource
	}{
		{
			name:       "frame frontmatter wins over everything",
			fm:         Frontmatter{Tier: 1, TimeCostHoursPrevented: 8.0},
			projVal:    6.0,
			projSet:    true,
			wantHours:  8.0,
			wantSource: TimeFromFrame,
		},
		{
			name:       "project override used when frame absent",
			fm:         Frontmatter{Tier: 1},
			projVal:    6.0,
			projSet:    true,
			wantHours:  6.0,
			wantSource: TimeFromConfig,
		},
		{
			name:       "tier default when neither frame nor config sets it",
			fm:         Frontmatter{Tier: 1},
			projSet:    false,
			wantHours:  4.0,
			wantSource: TimeFromTier,
		},
		{
			name:       "tier 6 default",
			fm:         Frontmatter{Tier: 6},
			projSet:    false,
			wantHours:  0.1,
			wantSource: TimeFromTier,
		},
		{
			name:       "missing tier falls back to tier 3 default via EffectiveTier()",
			fm:         Frontmatter{},
			projSet:    false,
			wantHours:  0.5,
			wantSource: TimeFromTier,
		},
		{
			name:       "zero frame value defers to project/tier (not 0h)",
			fm:         Frontmatter{Tier: 1, TimeCostHoursPrevented: 0},
			projSet:    false,
			wantHours:  4.0,
			wantSource: TimeFromTier,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, src := c.fm.EffectiveTimeCostHoursPrevented(c.projVal, c.projSet)
			if h != c.wantHours {
				t.Errorf("hours = %v; want %v", h, c.wantHours)
			}
			if src != c.wantSource {
				t.Errorf("source = %q; want %q", src, c.wantSource)
			}
		})
	}
}

func TestDefaultTimeCostHoursPreventedByTier_ConservativeShape(t *testing.T) {
	// Sanity: tier 1 > tier 6 (catastrophic costs more than cosmetic).
	if DefaultTimeCostHoursPreventedByTier[1] <= DefaultTimeCostHoursPreventedByTier[6] {
		t.Errorf("tier-1 default (%v) should exceed tier-6 default (%v)",
			DefaultTimeCostHoursPreventedByTier[1], DefaultTimeCostHoursPreventedByTier[6])
	}
	// Sanity: tier 1 <= 10h (don't ship absurd defaults).
	if DefaultTimeCostHoursPreventedByTier[1] > 10 {
		t.Errorf("tier-1 default %v is implausibly large; cap it", DefaultTimeCostHoursPreventedByTier[1])
	}
}
