// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v1tov2_test

import (
	"reflect"
	"sort"
	"testing"

	"nimblegate/internal/migration/v1tov2"
)

func TestTranslate_coreOnly(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"core"}})
	if !got.Core.Enabled {
		t.Error("Core.Enabled should be true")
	}
	if got.Framework.Selected != "" {
		t.Errorf("Framework should be empty for core-only, got %q", got.Framework.Selected)
	}
	if got.Platform.Selected != "" {
		t.Errorf("Platform should be empty for core-only, got %q", got.Platform.Selected)
	}
	// v1 core kit had frames in 4 domains beyond v2 core/: filesystem,
	// security, network, database. Translator adds these to preserve
	// v1 coverage.
	if !equalSorted(got.Domains.Selected, coreInclusiveDomains) {
		t.Errorf("Domains = %v, want core-inclusive %v", got.Domains.Selected, coreInclusiveDomains)
	}
}

// Kits that build on v1 core inherit the "core-inclusive domains"
// (filesystem, security, network, database). These domains hold v1 core
// frames that v2 places outside core/ - preserving them maintains v1
// coverage under v2 selection.
var coreInclusiveDomains = []string{"database", "filesystem", "network", "security"}

func TestTranslate_webApp(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"core", "web-app"}})
	if got.Framework.Selected != "html" {
		t.Errorf("Framework = %q, want html", got.Framework.Selected)
	}
	// web-app brings: html + documentation. Plus core-inclusive domains.
	wantDomains := append([]string{"documentation", "html"}, coreInclusiveDomains...)
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v (sorted)", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_cfPagesProject(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"cf-pages-project"}})
	if got.Framework.Selected != "html" {
		t.Errorf("Framework = %q, want html", got.Framework.Selected)
	}
	if got.Platform.Selected != "cloudflare" {
		t.Errorf("Platform = %q, want cloudflare", got.Platform.Selected)
	}
	if !equalSorted(got.PlatformOverrides["cloudflare"].Exclude, []string{"cf-workers"}) {
		t.Errorf("platform.cloudflare.exclude = %v, want [cf-workers]", got.PlatformOverrides["cloudflare"].Exclude)
	}
	// cf-pages brings: html + documentation + seo. Plus core-inclusive.
	wantDomains := append([]string{"documentation", "html", "seo"}, coreInclusiveDomains...)
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_cfWorkersProject(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"cf-workers-project"}})
	if got.Platform.Selected != "cloudflare" {
		t.Errorf("Platform = %q, want cloudflare", got.Platform.Selected)
	}
	if !equalSorted(got.PlatformOverrides["cloudflare"].Exclude, []string{"cf-pages"}) {
		t.Errorf("platform.cloudflare.exclude = %v, want [cf-pages]", got.PlatformOverrides["cloudflare"].Exclude)
	}
	// cf-workers brings: network + database. Both already in core-inclusive,
	// so effective domains are just the core-inclusive set.
	wantDomains := coreInclusiveDomains
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_securityStrict(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"security-strict"}})
	wantDomains := []string{"encoding", "security"}
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_encodingStrict(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"encoding-strict"}})
	wantDomains := []string{"encoding"}
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_myappReal(t *testing.T) {
	// The real myapp config: cf-pages-project + security-strict
	got := v1tov2.Translate(v1tov2.Input{
		AppliedKits: []string{"cf-pages-project", "security-strict"},
	})
	if got.Framework.Selected != "html" {
		t.Errorf("Framework = %q, want html", got.Framework.Selected)
	}
	if got.Platform.Selected != "cloudflare" {
		t.Errorf("Platform = %q, want cloudflare", got.Platform.Selected)
	}
	if !equalSorted(got.PlatformOverrides["cloudflare"].Exclude, []string{"cf-workers"}) {
		t.Errorf("platform.cloudflare.exclude = %v", got.PlatformOverrides["cloudflare"].Exclude)
	}
	// Union: web-app (html, doc) + cf-pages (+ seo) + security-strict
	// (security, encoding) + core-inclusive (filesystem, security, network,
	// database, but security already counted). All deduped.
	wantDomains := []string{"database", "documentation", "encoding", "filesystem", "html", "network", "security", "seo"}
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v", got.Domains.Selected, wantDomains)
	}
	if !got.Core.Enabled {
		t.Error("Core.Enabled should be true")
	}
}

func TestTranslate_multipleKitsUnionDomains(t *testing.T) {
	// Multiple kits → domain UNION, not duplication
	got := v1tov2.Translate(v1tov2.Input{
		AppliedKits: []string{"security-strict", "encoding-strict"},
	})
	// security-strict adds {security, encoding}; encoding-strict adds {encoding}
	// Union: {security, encoding} - encoding NOT duplicated
	wantDomains := []string{"encoding", "security"}
	if !equalSorted(got.Domains.Selected, wantDomains) {
		t.Errorf("Domains = %v, want %v (encoding should NOT duplicate)", got.Domains.Selected, wantDomains)
	}
}

func TestTranslate_emitsSchemaVersion2(t *testing.T) {
	got := v1tov2.Translate(v1tov2.Input{AppliedKits: []string{"core"}})
	if got.Appframes.Schema.Version != 2 {
		t.Errorf("Appframes.Schema.Version = %d, want 2", got.Appframes.Schema.Version)
	}
}

func TestTranslate_unknownKitNotInTable(t *testing.T) {
	// A kit not in the table is recorded as a warning but doesn't fail.
	result := v1tov2.TranslateWithErrors(v1tov2.Input{AppliedKits: []string{"some-future-kit"}})
	if len(result.Warnings) == 0 {
		t.Error("expected warning for unknown kit")
	}
	if result.Config == nil {
		t.Fatal("Config should not be nil even with unknown kits")
	}
}

// equalSorted compares two string slices ignoring order.
func equalSorted(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string{}, a...)
	bc := append([]string{}, b...)
	sort.Strings(ac)
	sort.Strings(bc)
	return reflect.DeepEqual(ac, bc)
}
