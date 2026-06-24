// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine_test

import (
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	"nimblegate/internal/stdlib"
)

func findPack(t *testing.T, statuses []engine.PackStatus, path string) engine.PackStatus {
	t.Helper()
	for _, ps := range statuses {
		if ps.BucketPath == path {
			return ps
		}
	}
	t.Fatalf("pack %q not in status list", path)
	return engine.PackStatus{}
}

func TestComputePackStatus_myappShape(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	stdlibFrames, _ := stdlib.Load()
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

	statuses := m.ComputePackStatus(cfg, stdlibFrames)

	core := findPack(t, statuses, "core")
	if core.State != engine.PackFullyActive {
		t.Errorf("core pack state = %v, want PackFullyActive", core.State)
	}
	if core.Total != 7 {
		t.Errorf("core pack total = %d, want 7", core.Total)
	}

	cfPages := findPack(t, statuses, "platform/cloudflare/cf-pages")
	if cfPages.State != engine.PackFullyActive {
		t.Errorf("cf-pages state = %v, want PackFullyActive (cloudflare selected, cf-pages not excluded)", cfPages.State)
	}

	cfD1 := findPack(t, statuses, "platform/cloudflare/cf-d1")
	if cfD1.State != engine.PackFullyActive {
		t.Errorf("cf-d1 state = %v, want PackFullyActive", cfD1.State)
	}

	secDomain := findPack(t, statuses, "domains/security")
	if secDomain.State != engine.PackFullyActive {
		t.Errorf("domains/security state = %v", secDomain.State)
	}

	netDomain := findPack(t, statuses, "domains/network")
	if netDomain.State != engine.PackInactive {
		t.Errorf("domains/network state = %v, want PackInactive (not in domains.selected)", netDomain.State)
	}
}

func TestComputePackStatus_partialPackInformational(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	stdlibFrames, _ := stdlib.Load()
	disabled := false
	cfg := &v2.Config{
		Core:    v2.CoreSel{Enabled: true},
		Domains: v2.DomainsSel{Selected: []string{"security"}},
		Frames: v2.FramesOverrides{Overrides: map[string]v2.FrameOverride{
			"no-innerHTML-user-input": {Enabled: &disabled},
		}},
	}
	cfg.Appframes.Schema.Version = 2

	statuses := m.ComputePackStatus(cfg, stdlibFrames)
	secDomain := findPack(t, statuses, "domains/security")
	if secDomain.State != engine.PackPartial {
		t.Errorf("partial pack state = %v, want PackPartial", secDomain.State)
	}
	if len(secDomain.ExcludedIDs) == 0 {
		t.Error("expected at least one excluded ID")
	}
}

func TestComputePackStatus_partialWarnAt50Percent(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	stdlibFrames, _ := stdlib.Load()
	disabled := false
	// domains/encoding has 8 frames. Strip 5 of them → 3/8 active = 37%
	// active = PackPartialWarn (>50% stripped).
	cfg := &v2.Config{
		Core:    v2.CoreSel{Enabled: true},
		Domains: v2.DomainsSel{Selected: []string{"encoding"}},
		Frames: v2.FramesOverrides{Overrides: map[string]v2.FrameOverride{
			"no-bom":                    {Enabled: &disabled},
			"no-smart-quotes-in-config": {Enabled: &disabled},
			"yaml-no-tabs":              {Enabled: &disabled},
			"no-mixed-indent":           {Enabled: &disabled},
			"no-en-dash-in-commands":    {Enabled: &disabled},
		}},
	}
	cfg.Appframes.Schema.Version = 2

	statuses := m.ComputePackStatus(cfg, stdlibFrames)
	enc := findPack(t, statuses, "domains/encoding")
	if enc.State != engine.PackPartialWarn {
		t.Errorf("encoding pack state = %v, want PackPartialWarn (5 of 8 stripped = >50%%)", enc.State)
	}
}

func TestPackState_StringStable(t *testing.T) {
	cases := []struct {
		s    engine.PackState
		want string
	}{
		{engine.PackInactive, "inactive"},
		{engine.PackFullyActive, "active"},
		{engine.PackPartial, "partial"},
		{engine.PackPartialWarn, "partial-warn"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("PackState(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}
