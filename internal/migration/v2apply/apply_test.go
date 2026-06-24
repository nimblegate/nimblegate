// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v2apply_test

import (
	"sort"
	"strings"
	"testing"
	"time"

	v2 "nimblegate/internal/config/v2"
	v2kits "nimblegate/internal/kits/v2"
	"nimblegate/internal/migration/v2apply"
)

func emptyCfg() *v2.Config {
	cfg := &v2.Config{
		Core: v2.CoreSel{Enabled: true},
	}
	cfg.Appframes.Schema.Version = 2
	return cfg
}

func staticKit(t *testing.T) v2kits.Kit {
	t.Helper()
	set, err := v2kits.LoadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	k, ok := set.Get("static-cf-pages-marketing")
	if !ok {
		t.Fatal("static-cf-pages-marketing not found in stdlib")
	}
	return k
}

func overlayKit(t *testing.T) v2kits.Kit {
	t.Helper()
	set, _ := v2kits.LoadStdlib()
	k, _ := set.Get("security-strict-overlay")
	return k
}

func TestApply_emptyConfigGetsAllSelections(t *testing.T) {
	cfg := emptyCfg()
	k := staticKit(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	_, err := v2apply.Apply(cfg, k, v2apply.ModeMerge, now)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if cfg.Framework.Selected != "html" {
		t.Errorf("Framework = %q", cfg.Framework.Selected)
	}
	if cfg.Platform.Selected != "cloudflare" {
		t.Errorf("Platform = %q", cfg.Platform.Selected)
	}
	if !contains(cfg.PlatformOverrides["cloudflare"].Exclude, "cf-workers") {
		t.Errorf("platform_exclude missing cf-workers: %v", cfg.PlatformOverrides["cloudflare"].Exclude)
	}
	wantDomains := []string{"documentation", "encoding", "html", "security", "seo"}
	got := append([]string{}, cfg.Domains.Selected...)
	sort.Strings(got)
	if !sliceEqual(got, wantDomains) {
		t.Errorf("Domains = %v, want %v (sorted)", got, wantDomains)
	}
}

func TestApply_recordsInMetaAppliedKits(t *testing.T) {
	cfg := emptyCfg()
	k := staticKit(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	_, err := v2apply.Apply(cfg, k, v2apply.ModeMerge, now)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(cfg.Meta.AppliedKits) != 1 {
		t.Fatalf("Meta.AppliedKits len = %d, want 1", len(cfg.Meta.AppliedKits))
	}
	rec := cfg.Meta.AppliedKits[0]
	if rec.ID != "static-cf-pages-marketing" {
		t.Errorf("ID = %q", rec.ID)
	}
	if rec.Semver == "" {
		t.Error("Semver should be populated from kit")
	}
	if !strings.HasPrefix(rec.Hash, "sha256:") {
		t.Errorf("Hash should be sha256-prefixed, got %q", rec.Hash)
	}
	if rec.AppliedAt != "2026-06-06T12:00:00Z" {
		t.Errorf("AppliedAt = %q", rec.AppliedAt)
	}
}

func TestApply_overlayUnionsWithExistingProjectKit(t *testing.T) {
	// Apply static-cf-pages-marketing, then security-strict-overlay. Overlay
	// has no framework/platform - applying it on top should union domains
	// without changing framework/platform.
	cfg := emptyCfg()
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	_, err := v2apply.Apply(cfg, staticKit(t), v2apply.ModeMerge, now)
	if err != nil {
		t.Fatalf("Apply first kit: %v", err)
	}
	beforeFw := cfg.Framework.Selected
	beforePlat := cfg.Platform.Selected
	beforeDomains := append([]string{}, cfg.Domains.Selected...)

	res, err := v2apply.Apply(cfg, overlayKit(t), v2apply.ModeMerge, now)
	if err != nil {
		t.Fatalf("Apply overlay: %v", err)
	}

	if cfg.Framework.Selected != beforeFw {
		t.Errorf("overlay should not change framework: was %q now %q", beforeFw, cfg.Framework.Selected)
	}
	if cfg.Platform.Selected != beforePlat {
		t.Errorf("overlay should not change platform: was %q now %q", beforePlat, cfg.Platform.Selected)
	}
	// Domains should now include all original + any overlay additions.
	if len(cfg.Domains.Selected) < len(beforeDomains) {
		t.Errorf("overlay reduced domain count")
	}
	if len(cfg.Meta.AppliedKits) != 2 {
		t.Errorf("Meta.AppliedKits len = %d, want 2", len(cfg.Meta.AppliedKits))
	}
	if len(res.Warnings) != 0 {
		t.Errorf("overlay should have no warnings (no conflicts), got: %v", res.Warnings)
	}
}

func TestApply_mergeRejectsFrameworkConflict(t *testing.T) {
	cfg := emptyCfg()
	cfg.Framework.Selected = "svelte"

	// staticKit has framework=html; merge should refuse.
	_, err := v2apply.Apply(cfg, staticKit(t), v2apply.ModeMerge, time.Now())
	if err == nil {
		t.Fatal("expected error on framework conflict in merge mode")
	}
	if !strings.Contains(err.Error(), "framework conflict") {
		t.Errorf("error should mention framework conflict, got: %v", err)
	}
}

func TestApply_overwriteAllowsFrameworkChange(t *testing.T) {
	cfg := emptyCfg()
	cfg.Framework.Selected = "svelte"

	res, err := v2apply.Apply(cfg, staticKit(t), v2apply.ModeOverwrite, time.Now())
	if err != nil {
		t.Fatalf("Apply overwrite: %v", err)
	}
	if cfg.Framework.Selected != "html" {
		t.Errorf("Framework = %q, want html (overwritten)", cfg.Framework.Selected)
	}
	if len(res.Warnings) == 0 {
		t.Error("expected warning when overwrite replaces framework")
	}
	if !strings.Contains(res.Warnings[0], "framework") {
		t.Errorf("warning should mention framework, got: %q", res.Warnings[0])
	}
}

func TestApply_idempotentSameKit(t *testing.T) {
	cfg := emptyCfg()
	k := staticKit(t)
	now := time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC)

	_, err := v2apply.Apply(cfg, k, v2apply.ModeMerge, now)
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	domainsAfterFirst := append([]string{}, cfg.Domains.Selected...)

	// Apply same kit again - should not duplicate domains or fail
	_, err = v2apply.Apply(cfg, k, v2apply.ModeMerge, now)
	if err != nil {
		t.Errorf("second Apply (same kit) should not error: %v", err)
	}
	if len(cfg.Domains.Selected) != len(domainsAfterFirst) {
		t.Errorf("re-apply should not duplicate domains; was %v now %v", domainsAfterFirst, cfg.Domains.Selected)
	}
	if len(cfg.Meta.AppliedKits) != 1 {
		t.Errorf("Meta.AppliedKits should still have 1 entry (replaced, not appended); got %d", len(cfg.Meta.AppliedKits))
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
