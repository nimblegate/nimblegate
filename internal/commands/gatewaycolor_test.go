// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func hexLum(t *testing.T, hex string) float64 {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		t.Fatalf("bad hex %q: %v", hex, err)
	}
	lin := func(v int) float64 {
		c := float64(v) / 255.0
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

func TestDayPaletteReadable(t *testing.T) {
	if len(gwDayColors) < 2 {
		t.Fatalf("need >=2 day colors, got %d", len(gwDayColors))
	}
	const bgLum = 0.00556
	for i, hex := range gwDayColors {
		ratio := (hexLum(t, hex) + 0.05) / (bgLum + 0.05)
		if ratio < 4.5 {
			t.Errorf("day %d color %s contrast %.2f < 4.5:1 on #0f1115", i, hex, ratio)
		}
	}
	for i := range gwDayColors {
		if !strings.Contains(gwDayColorStyle, fmt.Sprintf(".gw-dc-%d{", i)) {
			t.Errorf("gwDayColorStyle missing .gw-dc-%d rule", i)
		}
	}
}

func TestHourPaletteReadable(t *testing.T) {
	if len(gwHourColors) != 24 {
		t.Fatalf("want 24 hour colors, got %d", len(gwHourColors))
	}
	const bgLum = 0.00556 // relative luminance of #0f1115
	for h, hex := range gwHourColors {
		ratio := (hexLum(t, hex) + 0.05) / (bgLum + 0.05)
		if ratio < 4.5 {
			t.Errorf("hour %d color %s contrast %.2f < 4.5:1 on #0f1115", h, hex, ratio)
		}
	}
	for h := 0; h < 24; h++ {
		if !strings.Contains(gwColorStyle, fmt.Sprintf(".gw-tc-%d{", h)) {
			t.Errorf("gwColorStyle missing .gw-tc-%d rule", h)
		}
	}
}
