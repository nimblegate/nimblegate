// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine_test

import (
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	"nimblegate/internal/stdlib"
)

func findRoot(t *testing.T, roots []engine.TreeNode, label string) engine.TreeNode {
	t.Helper()
	for _, r := range roots {
		if r.Label == label {
			return r
		}
	}
	t.Fatalf("root node %q not found", label)
	return engine.TreeNode{}
}

func findChild(t *testing.T, root engine.TreeNode, label string) engine.TreeNode {
	t.Helper()
	for _, c := range root.Children {
		if c.Label == label {
			return c
		}
	}
	t.Fatalf("child %q not found under %q (children: %v)", label, root.Label, childLabels(root))
	return engine.TreeNode{}
}

func childLabels(n engine.TreeNode) []string {
	out := make([]string, 0, len(n.Children))
	for _, c := range n.Children {
		out = append(out, c.Label)
	}
	return out
}

func TestBuildTree_myappShape(t *testing.T) {
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

	roots := m.BuildTree(cfg, stdlibFrames)

	// Expect 4 root nodes: core, framework/html (may be empty in v2 stdlib),
	// platform/cloudflare, domains. We don't yet have framework/html frames
	// since the v2 stdlib doesn't have any framework/html/ files, so this
	// root may not appear.
	rootLabels := make([]string, len(roots))
	for i, r := range roots {
		rootLabels[i] = r.Label
	}

	core := findRoot(t, roots, "core")
	if core.State != engine.NodeFullyActive {
		t.Errorf("core state = %v, want NodeFullyActive", core.State)
	}
	if core.Total != 7 {
		t.Errorf("core total = %d, want 7", core.Total)
	}
	if core.Active != 7 {
		t.Errorf("core active = %d, want 7", core.Active)
	}

	platform := findRoot(t, roots, "platform/cloudflare")
	// v2 stdlib currently has cf-pages + cf-d1 sub-buckets but no
	// cf-workers frames mapped, so cf-workers doesn't appear in the tree.
	// (When future cf-workers frames are added, they'd appear as excluded.)
	cfPages := findChild(t, platform, "platform/cloudflare/cf-pages")
	if cfPages.State != engine.NodeFullyActive {
		t.Errorf("cf-pages state = %v, want NodeFullyActive (not excluded)", cfPages.State)
	}
	cfD1 := findChild(t, platform, "platform/cloudflare/cf-d1")
	if cfD1.State != engine.NodeFullyActive {
		t.Errorf("cf-d1 state = %v, want NodeFullyActive (cloudflare selected, cf-d1 not excluded)", cfD1.State)
	}

	domains := findRoot(t, roots, "domains")
	secNode := findChild(t, domains, "domains/security")
	if secNode.State != engine.NodeFullyActive {
		t.Errorf("domains/security state = %v, want NodeFullyActive", secNode.State)
	}

	// Network domain should be inactive (not in cfg.Domains.Selected)
	netNode := findChild(t, domains, "domains/network")
	if netNode.State != engine.NodeInactive {
		t.Errorf("domains/network state = %v, want NodeInactive (not selected)", netNode.State)
	}
}

func TestBuildTree_perFrameOverrideMakesPackPartial(t *testing.T) {
	m, _ := engine.BuildV2FrameMap()
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

	roots := m.BuildTree(cfg, stdlibFrames)
	domains := findRoot(t, roots, "domains")
	secNode := findChild(t, domains, "domains/security")

	if secNode.State != engine.NodePartial {
		t.Errorf("partial pack state = %v, want NodePartial", secNode.State)
	}
	// MissingIDs should list the stripped frame
	hasMissing := false
	for _, m := range secNode.MissingIDs {
		if m == "security/no-innerHTML-user-input" {
			hasMissing = true
			break
		}
	}
	if !hasMissing {
		t.Errorf("expected security/no-innerHTML-user-input in MissingIDs, got: %v", secNode.MissingIDs)
	}
}

func TestBuildTree_partialWarnAt50Percent(t *testing.T) {
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	disabled := false
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

	roots := m.BuildTree(cfg, stdlibFrames)
	domains := findRoot(t, roots, "domains")
	encNode := findChild(t, domains, "domains/encoding")
	if encNode.State != engine.NodePartialWarn {
		t.Errorf("encoding state = %v, want NodePartialWarn (5/8 stripped = >50%%)", encNode.State)
	}
}

func TestBuildTree_emptyConfigShowsCoreAndInactiveDomains(t *testing.T) {
	// With no framework/platform/domains selected, the tree shows core
	// (always-on) plus a domains root with every domain visible-but-inactive.
	// This is a discovery surface - operators see what they could enable
	// without hunting through documentation.
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	cfg := &v2.Config{
		Core: v2.CoreSel{Enabled: true},
	}
	cfg.Appframes.Schema.Version = 2

	roots := m.BuildTree(cfg, stdlibFrames)
	// Should see core + domains. No framework root (none selected). No platform root.
	rl := rootsLabels(roots)
	hasCore := false
	hasDomains := false
	for _, l := range rl {
		switch l {
		case "core":
			hasCore = true
		case "domains":
			hasDomains = true
		case "framework", "platform":
			t.Errorf("unexpected root %q for empty config", l)
		}
	}
	if !hasCore {
		t.Errorf("core root missing; got: %v", rl)
	}
	if !hasDomains {
		t.Errorf("domains root missing; got: %v", rl)
	}

	// Every domain should appear as NodeInactive.
	domains := findRoot(t, roots, "domains")
	for _, c := range domains.Children {
		if c.State != engine.NodeInactive {
			t.Errorf("unselected domain %q state = %v, want NodeInactive", c.Label, c.State)
		}
	}
}

func TestNodeState_StringStable(t *testing.T) {
	cases := []struct {
		s    engine.NodeState
		want string
	}{
		{engine.NodeInactive, "inactive"},
		{engine.NodeFullyActive, "active"},
		{engine.NodePartial, "partial"},
		{engine.NodePartialWarn, "partial-warn"},
		{engine.NodeExcluded, "excluded"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("NodeState(%d).String() = %q, want %q", c.s, got, c.want)
		}
	}
}

func rootsLabels(roots []engine.TreeNode) []string {
	out := make([]string, len(roots))
	for i, r := range roots {
		out[i] = r.Label
	}
	return out
}
