// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v2_test

import (
	"strings"
	"testing"

	v2kits "nimblegate/internal/kits/v2"
)

func TestLoadStdlib_loadsAllKits(t *testing.T) {
	set, err := v2kits.LoadStdlib()
	if err != nil {
		t.Fatalf("LoadStdlib: %v", err)
	}
	ids := set.IDs()
	if len(ids) == 0 {
		t.Fatal("expected at least one v2 stdlib kit")
	}
	for _, id := range ids {
		k, _ := set.Get(id)
		if k.Display == "" {
			t.Errorf("kit %q missing display", id)
		}
		if k.Semver == "" {
			t.Errorf("kit %q missing semver", id)
		}
	}
}

func TestLoadStdlib_staticCfPagesMarketingPresent(t *testing.T) {
	// myapp-shape default kit. Reflects the canonical translation
	// of cf-pages-project + security-strict from the v1 catalog.
	set, err := v2kits.LoadStdlib()
	if err != nil {
		t.Fatalf("LoadStdlib: %v", err)
	}
	k, ok := set.Get("static-cf-pages-marketing")
	if !ok {
		t.Fatal("expected static-cf-pages-marketing kit in stdlib")
	}
	if k.Selections.Framework != "html" {
		t.Errorf("Framework = %q, want html", k.Selections.Framework)
	}
	if k.Selections.Platform != "cloudflare" {
		t.Errorf("Platform = %q, want cloudflare", k.Selections.Platform)
	}
	wantExclude := []string{"cf-workers"}
	if !stringSlicesEqualUnordered(k.Selections.PlatformExclude["cloudflare"], wantExclude) {
		t.Errorf("platform_exclude.cloudflare = %v, want %v", k.Selections.PlatformExclude["cloudflare"], wantExclude)
	}
	wantDomains := []string{"security", "encoding", "documentation", "html", "seo"}
	if !stringSlicesEqualUnordered(k.Selections.Domains, wantDomains) {
		t.Errorf("Domains = %v, want %v", k.Selections.Domains, wantDomains)
	}
}

func TestLoadStdlib_securityStrictOverlayHasOnlyDomains(t *testing.T) {
	// Overlay kits intentionally have no framework/platform - they're
	// designed to be applied on top of any project kit.
	set, _ := v2kits.LoadStdlib()
	k, ok := set.Get("security-strict-overlay")
	if !ok {
		t.Fatal("expected security-strict-overlay kit in stdlib")
	}
	if k.Selections.Framework != "" {
		t.Errorf("overlay kit should have empty Framework, got %q", k.Selections.Framework)
	}
	if k.Selections.Platform != "" {
		t.Errorf("overlay kit should have empty Platform, got %q", k.Selections.Platform)
	}
	if len(k.Selections.Domains) == 0 {
		t.Error("overlay kit should have at least one domain")
	}
}

func TestHash_stableAcrossRuns(t *testing.T) {
	set, _ := v2kits.LoadStdlib()
	k, _ := set.Get("static-cf-pages-marketing")
	h1 := k.Hash()
	h2 := k.Hash()
	if h1 != h2 {
		t.Errorf("Hash not deterministic: %q vs %q", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Errorf("Hash should be sha256-prefixed, got %q", h1)
	}
}

func TestHash_differsBetweenKits(t *testing.T) {
	set, _ := v2kits.LoadStdlib()
	staticKit, _ := set.Get("static-cf-pages-marketing")
	svelteKit, _ := set.Get("svelte-cf-pages-marketing")
	if staticKit.Hash() == svelteKit.Hash() {
		t.Error("different kits should have different hashes")
	}
}

func TestHash_changesWhenSelectionsChange(t *testing.T) {
	base := v2kits.Kit{
		KitID:  "test",
		Semver: "1.0",
		Selections: v2kits.Selections{
			Domains: []string{"security"},
		},
	}
	withMore := base
	withMore.Selections.Domains = []string{"security", "encoding"}
	if base.Hash() == withMore.Hash() {
		t.Error("kits with different domains should have different hashes")
	}
}

func TestHash_orderIndependent(t *testing.T) {
	// Reordering domains shouldn't change hash - set semantics.
	a := v2kits.Kit{
		KitID:  "test",
		Semver: "1.0",
		Selections: v2kits.Selections{
			Domains: []string{"security", "encoding", "html"},
		},
	}
	b := v2kits.Kit{
		KitID:  "test",
		Semver: "1.0",
		Selections: v2kits.Selections{
			Domains: []string{"html", "security", "encoding"},
		},
	}
	if a.Hash() != b.Hash() {
		t.Errorf("hash should be order-independent on Domains, got %q vs %q", a.Hash(), b.Hash())
	}
}

func stringSlicesEqualUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	for _, s := range b {
		if !seen[s] {
			return false
		}
	}
	return true
}
