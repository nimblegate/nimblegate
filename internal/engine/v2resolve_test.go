// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine_test

import (
	"sort"
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	"nimblegate/internal/stdlib"
)

func TestBuildV2FrameMap_walksEntireV2Tree(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	// Phase A3 classification placed exactly 44 frames into v2 layout.
	if got := len(m.IDToBucket); got != 44 {
		t.Errorf("V2FrameMap has %d entries, want 44 (classification table)", got)
	}
}

func TestBuildV2FrameMap_resolvesKnownFrames(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	// Spot-check the worked examples from the classification table.
	cases := []struct {
		v1ID       string
		wantBucket string
	}{
		{"git/no-force-push-main", "core/no-force-push-main"},
		{"security/no-hardcoded-credentials", "core/no-hardcoded-credentials"},
		{"security/cf-pages-headers-baseline", "platform/cloudflare/cf-pages/headers-baseline"},
		{"security/no-innerHTML-user-input", "domains/security/no-innerHTML-user-input"},
		{"app-correctness/cf-graphql-schema-match", "platform/cloudflare/cf-d1/graphql-schema-match"},
		{"web/html-required-meta", "domains/html/required-meta"},
		{"web/html-seo-meta", "domains/seo/meta-tags-complete"},
		{"commands/apt-purge-preview", "domains/filesystem/apt-purge-preview"},
	}
	for _, c := range cases {
		bucket, ok := m.IDToBucket[c.v1ID]
		if !ok {
			t.Errorf("V1 ID %q not in map", c.v1ID)
			continue
		}
		if got := bucket.String(); got != c.wantBucket {
			t.Errorf("V1 ID %q → bucket %q, want %q", c.v1ID, got, c.wantBucket)
		}
	}
}

func TestEnabledFrameIDs_coreOnlyEnablesCore(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	stdlibFrames, _ := stdlib.Load()
	cfg := &v2.Config{
		Core: v2.CoreSel{Enabled: true},
	}
	cfg.Appframes.Schema.Version = 2

	enabled := m.EnabledFrameIDs(cfg, stdlibFrames)
	sort.Strings(enabled)

	// All core-bucket frames should be enabled; nothing else.
	wantPrefixes := map[string]bool{
		"git/no-force-push-main":            true,
		"git/no-bypass-pre-commit":          true,
		"git/no-amend-pushed-commits":       true,
		"git/folder-branch-lock":            true,
		"filesystem/rm-rf-protected-paths":  true,
		"security/no-hardcoded-credentials": true,
		"security/no-private-keys-in-repo":  true,
	}
	got := make(map[string]bool)
	for _, id := range enabled {
		got[id] = true
	}
	for want := range wantPrefixes {
		if !got[want] {
			t.Errorf("expected %q to be enabled (core bucket); got: %v", want, enabled)
		}
	}
	// And nothing outside core should appear.
	for _, id := range enabled {
		if !wantPrefixes[id] {
			t.Errorf("unexpected frame %q in core-only enabled set", id)
		}
	}
}

func TestEnabledFrameIDs_myappReal(t *testing.T) {
	// The migration outcome on myapp-shape: framework=html,
	// platform=cloudflare exclude=[cf-workers], domains=[security, encoding,
	// documentation, html, seo].
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

	enabled := m.EnabledFrameIDs(cfg, stdlibFrames)

	// Core + cf-pages (cloudflare with cf-workers excluded; cf-d1 still active
	// because not in exclude list) + 5 domains. Doesn't include cf-workers
	// because excluded; doesn't include domains we didn't select.

	// Sanity: must include several known anchor frames.
	wantIncluded := []string{
		"git/no-force-push-main",                  // core
		"security/no-hardcoded-credentials",       // core
		"security/cf-pages-headers-baseline",      // platform/cloudflare/cf-pages
		"app-correctness/cf-graphql-schema-match", // platform/cloudflare/cf-d1 (NOT excluded)
		"security/no-innerHTML-user-input",        // domains/security
		"web/html-required-meta",                  // domains/html
		"web/html-seo-meta",                       // domains/seo
		"encoding/no-bom",                         // domains/encoding
		"documentation/dated-todo",                // domains/documentation
	}
	for _, want := range wantIncluded {
		found := false
		for _, id := range enabled {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q to be enabled in myapp-shape v2 config", want)
		}
	}
	// Sanity: must NOT include domains we didn't select.
	mustNotInclude := []string{
		"network/cidr-host-bits-zero", // domains/network NOT in select list
		"network/no-localhost-in-proxy-config",
	}
	for _, dont := range mustNotInclude {
		for _, id := range enabled {
			if id == dont {
				t.Errorf("frame %q should NOT be enabled (domain not selected)", dont)
			}
		}
	}
}

func TestEnabledFrameIDs_excludedSubBucketSkipped(t *testing.T) {
	m, err := engine.BuildV2FrameMap()
	if err != nil {
		t.Fatalf("BuildV2FrameMap: %v", err)
	}
	stdlibFrames, _ := stdlib.Load()
	cfg := &v2.Config{
		Core:     v2.CoreSel{Enabled: true},
		Platform: v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{
			"cloudflare": {Exclude: []string{"cf-pages"}}, // exclude cf-pages
		},
	}
	cfg.Appframes.Schema.Version = 2

	enabled := m.EnabledFrameIDs(cfg, stdlibFrames)

	// cf-pages-specific frames must NOT appear.
	mustNotInclude := []string{
		"security/cf-pages-headers-baseline",
		"app-correctness/top-of-page-import-safety",
		"app-correctness/prefer-static-public",
		"app-correctness/dynamic-env-declared",
	}
	for _, dont := range mustNotInclude {
		for _, id := range enabled {
			if id == dont {
				t.Errorf("cf-pages frame %q should NOT be enabled (cf-pages excluded)", dont)
			}
		}
	}
	// But cf-d1 frames should still be enabled (not excluded).
	wantIncluded := []string{
		"app-correctness/cf-graphql-schema-match",
		"app-correctness/cf-graphql-dataset-by-window",
	}
	for _, want := range wantIncluded {
		found := false
		for _, id := range enabled {
			if id == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cf-d1 frame %q should still be enabled (only cf-pages excluded)", want)
		}
	}
}

func TestEnabledFrameIDs_perFrameOverrideStripsFromActivePack(t *testing.T) {
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

	enabled := m.EnabledFrameIDs(cfg, stdlibFrames)
	for _, id := range enabled {
		if id == "security/no-innerHTML-user-input" {
			t.Error("no-innerHTML-user-input should be excluded by per-frame override")
		}
	}
}
