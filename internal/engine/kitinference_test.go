// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine_test

import (
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	v2kits "nimblegate/internal/kits/v2"
	"nimblegate/internal/stdlib"
)

func findMatch(matches []engine.KitMatch, kitID string) (engine.KitMatch, bool) {
	for _, km := range matches {
		if km.KitID == kitID {
			return km, true
		}
	}
	return engine.KitMatch{}, false
}

func TestInferKitMatches_myappShapeMatchesStaticKit(t *testing.T) {
	// Build myapp-shape config and confirm static-cf-pages-marketing
	// kit shows as MatchFully (operator's config covers the kit's set).
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	kitSet, _ := v2kits.LoadStdlib()

	cfg := &v2.Config{
		Core:      v2.CoreSel{Enabled: true},
		Framework: v2.FrameworkSel{Selected: "html"},
		Platform:  v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{
			"cloudflare": {Exclude: []string{"cf-workers"}},
		},
		Domains: v2.DomainsSel{Selected: []string{"security", "encoding", "documentation", "html", "seo"}},
	}
	cfg.Appframes.Schema.Version = 2

	matches := m.InferKitMatches(cfg, stdlibFrames, kitSet)

	static, ok := findMatch(matches, "static-cf-pages-marketing")
	if !ok {
		t.Fatal("static-cf-pages-marketing not found in match list")
	}
	if static.Status != engine.MatchFully {
		t.Errorf("static-cf-pages-marketing status = %v, want MatchFully", static.Status)
	}
	if static.Active != static.Total {
		t.Errorf("expected Active == Total for fully-matched kit; got %d/%d", static.Active, static.Total)
	}
}

func TestInferKitMatches_partialMatchWhenDomainsMissing(t *testing.T) {
	// Config with framework=html + platform=cloudflare but only some of
	// the static-cf-pages-marketing kit's domains. Should be MatchPartial.
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	kitSet, _ := v2kits.LoadStdlib()

	cfg := &v2.Config{
		Core:      v2.CoreSel{Enabled: true},
		Framework: v2.FrameworkSel{Selected: "html"},
		Platform:  v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{
			"cloudflare": {Exclude: []string{"cf-workers"}},
		},
		Domains: v2.DomainsSel{Selected: []string{"security"}}, // missing encoding/docs/html/seo
	}
	cfg.Appframes.Schema.Version = 2

	matches := m.InferKitMatches(cfg, stdlibFrames, kitSet)
	static, ok := findMatch(matches, "static-cf-pages-marketing")
	if !ok {
		t.Fatal("static-cf-pages-marketing should appear in matches")
	}
	if static.Status != engine.MatchPartial {
		t.Errorf("static-cf-pages-marketing status = %v, want MatchPartial (missing domains)", static.Status)
	}
	if len(static.Missing) == 0 {
		t.Error("expected non-empty Missing list for partial match")
	}
}

func TestInferKitMatches_emptyConfigShowsPartialViaSharedCore(t *testing.T) {
	// Config with only core enabled: every kit's resolved frame set includes
	// core frames (because their selections imply Core.Enabled too), so all
	// kits surface as MatchPartial via shared-core. No kit reaches MatchFully
	// because their framework/platform/domain selections aren't met.
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	kitSet, _ := v2kits.LoadStdlib()

	cfg := &v2.Config{
		Core: v2.CoreSel{Enabled: true},
	}
	cfg.Appframes.Schema.Version = 2

	matches := m.InferKitMatches(cfg, stdlibFrames, kitSet)

	// Nothing should be MatchFully - operator hasn't selected any axis.
	for _, km := range matches {
		if km.Status == engine.MatchFully {
			t.Errorf("kit %q matched FULLY for empty config - selections not in cfg", km.KitID)
		}
	}
	// At least one kit should match partially via shared core (the catalog
	// always implies core in its resolved set).
	partials := 0
	for _, km := range matches {
		if km.Status == engine.MatchPartial {
			partials++
		}
	}
	if partials == 0 {
		t.Error("expected at least one partial match via shared core; got none")
	}
}

func TestInferKitMatches_sortFullyFirstThenPartial(t *testing.T) {
	// myapp-shape: should have static-cf-pages-marketing fully matched
	// + possibly other partial matches. Verify ordering.
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	kitSet, _ := v2kits.LoadStdlib()

	cfg := &v2.Config{
		Core:      v2.CoreSel{Enabled: true},
		Framework: v2.FrameworkSel{Selected: "html"},
		Platform:  v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{
			"cloudflare": {Exclude: []string{"cf-workers"}},
		},
		Domains: v2.DomainsSel{Selected: []string{"security", "encoding", "documentation", "html", "seo"}},
	}
	cfg.Appframes.Schema.Version = 2

	matches := m.InferKitMatches(cfg, stdlibFrames, kitSet)

	// Walk matches: MatchFully should never appear after MatchPartial; either
	// should never appear after MatchNone.
	lastStatus := engine.MatchFully
	for i, km := range matches {
		if km.Status > lastStatus {
			t.Errorf("match %d (%q, %v) appears AFTER %v - sort order broken", i, km.KitID, km.Status, lastStatus)
		}
		lastStatus = km.Status
	}
}

func TestMatchStatus_StringStable(t *testing.T) {
	cases := []struct {
		s    engine.MatchStatus
		want string
	}{
		{engine.MatchNone, "no-match"},
		{engine.MatchPartial, "partial"},
		{engine.MatchFully, "fully-matched"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("MatchStatus(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}
