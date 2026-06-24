// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine_test

import (
	"strings"
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	v2kits "nimblegate/internal/kits/v2"
	"nimblegate/internal/stdlib"
)

func TestRenderTreeHTML_emptyRootsShowsHint(t *testing.T) {
	html := string(engine.RenderTreeHTML(nil))
	if !strings.Contains(html, "nimblegate-tree-empty") {
		t.Errorf("expected empty hint class; got: %s", html)
	}
	if !strings.Contains(html, "appframes.toml") {
		t.Errorf("expected toml hint; got: %s", html)
	}
}

func TestRenderTreeHTML_myappShapeContainsExpectedNodes(t *testing.T) {
	m, _ := engine.BuildV2FrameMap()
	stdlibFrames, _ := stdlib.Load()
	cfg := &v2.Config{
		Core:      v2.CoreSel{Enabled: true},
		Framework: v2.FrameworkSel{Selected: "html"},
		Platform:  v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{
			"cloudflare": {Exclude: []string{"cf-workers"}},
		},
		Domains: v2.DomainsSel{Selected: []string{"security", "encoding"}},
	}
	cfg.Appframes.Schema.Version = 2

	roots := m.BuildTree(cfg, stdlibFrames)
	html := string(engine.RenderTreeHTML(roots))

	for _, want := range []string{
		"nimblegate-tree",
		"core",
		"platform/cloudflare",
		"domains",
		"domains/security",
		"gw-tree-active",
		"data-total=",
		"data-active=",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q in:\n%s", want, html)
		}
	}
}

func TestRenderTreeHTML_partialNodeCarriesMissingAttribute(t *testing.T) {
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
	html := string(engine.RenderTreeHTML(roots))

	if !strings.Contains(html, `data-missing="`) {
		t.Errorf("expected data-missing attribute on partial node; got:\n%s", html)
	}
	if !strings.Contains(html, "security/no-innerHTML-user-input") {
		t.Errorf("expected stripped frame ID in data-missing; got:\n%s", html)
	}
	if !strings.Contains(html, "gw-tree-partial") {
		t.Errorf("expected partial state class; got:\n%s", html)
	}
}

func TestRenderTreeHTML_escapesLabels(t *testing.T) {
	// Hand-construct a tree node with HTML-sensitive characters in the label
	// to verify escaping (defense-in-depth - labels come from the bucket
	// model which doesn't currently produce these, but RenderTreeHTML must
	// be safe even if the model changes).
	roots := []engine.TreeNode{{
		Label:  `core<script>alert("x")</script>`,
		State:  engine.NodeFullyActive,
		Total:  1,
		Active: 1,
	}}
	html := string(engine.RenderTreeHTML(roots))
	if strings.Contains(html, "<script>") {
		t.Errorf("RenderTreeHTML did not escape label: %s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>; got: %s", html)
	}
}

func TestRenderKitInferenceHTML_emptyShowsEmptyState(t *testing.T) {
	html := string(engine.RenderKitInferenceHTML(nil, true))
	if !strings.Contains(html, "nimblegate-kits-empty") {
		t.Errorf("expected empty state class; got: %s", html)
	}
}

func TestRenderKitInferenceHTML_myappShowsFullyMatchedKit(t *testing.T) {
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
	html := string(engine.RenderKitInferenceHTML(matches, true))

	for _, want := range []string{
		"nimblegate-kits",
		"static-cf-pages-marketing",
		"gw-kit-fully-matched",
		"frames active",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q in:\n%s", want, html)
		}
	}
}

func TestRenderKitInferenceHTML_filtersNoMatchWhenOnlyShowMatched(t *testing.T) {
	// Hand-craft a list with one MatchNone - confirm it's filtered when
	// onlyShowMatched=true and shown when false.
	matches := []engine.KitMatch{
		{KitID: "fake-kit", Status: engine.MatchNone, Total: 5, Active: 0},
	}
	hidden := string(engine.RenderKitInferenceHTML(matches, true))
	shown := string(engine.RenderKitInferenceHTML(matches, false))

	if strings.Contains(hidden, "fake-kit") {
		t.Errorf("onlyShowMatched=true should hide MatchNone; got: %s", hidden)
	}
	if !strings.Contains(shown, "fake-kit") {
		t.Errorf("onlyShowMatched=false should show MatchNone; got: %s", shown)
	}
}

func TestRenderKitInferenceHTML_partialMatchShowsMissingDetails(t *testing.T) {
	matches := []engine.KitMatch{
		{
			KitID:   "partial-kit",
			Display: "Partial Kit",
			Semver:  "1.0.0",
			Status:  engine.MatchPartial,
			Total:   3,
			Active:  2,
			Missing: []string{"security/no-innerHTML-user-input"},
		},
	}
	html := string(engine.RenderKitInferenceHTML(matches, true))

	for _, want := range []string{
		"<details",
		"summary",
		"1 frame(s) the kit would add",
		"security/no-innerHTML-user-input",
		"Partial Kit",
		"v1.0.0",
		"gw-kit-partial",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML missing %q in:\n%s", want, html)
		}
	}
}
