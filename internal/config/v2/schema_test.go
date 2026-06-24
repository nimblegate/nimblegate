// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v2_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	v2 "nimblegate/internal/config/v2"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_marketingStaticSite(t *testing.T) {
	// Spec §2.3 example: Marketing static site on Cloudflare Pages (myapp-shape)
	content := `
[appframes.schema]
version = 2

[framework]
selected = "html"

[platform]
selected = "cloudflare"

[platform.cloudflare]
exclude = ["cf-workers"]

[domains]
selected = ["security", "encoding", "documentation", "html", "seo"]

[core]
enabled = true
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Appframes.Schema.Version != 2 {
		t.Errorf("Appframes.Schema.Version = %d, want 2", cfg.Appframes.Schema.Version)
	}
	if cfg.Framework.Selected != "html" {
		t.Errorf("Framework.Selected = %q, want html", cfg.Framework.Selected)
	}
	if cfg.Platform.Selected != "cloudflare" {
		t.Errorf("Platform.Selected = %q, want cloudflare", cfg.Platform.Selected)
	}
	if got := cfg.PlatformOverrides["cloudflare"].Exclude; !reflect.DeepEqual(got, []string{"cf-workers"}) {
		t.Errorf("PlatformOverrides[cloudflare].Exclude = %v, want [cf-workers]", got)
	}
	want := []string{"security", "encoding", "documentation", "html", "seo"}
	if !reflect.DeepEqual(cfg.Domains.Selected, want) {
		t.Errorf("Domains.Selected = %v, want %v", cfg.Domains.Selected, want)
	}
	if !cfg.Core.Enabled {
		t.Error("Core.Enabled = false, want true")
	}
}

func TestLoad_svelteCfPagesWithD1(t *testing.T) {
	// Spec §2.3 example: Svelte app on Cloudflare Pages with D1
	content := `
[appframes.schema]
version = 2

[framework]
selected = "svelte"

[platform]
selected = "cloudflare"

[platform.cloudflare]
exclude = ["cf-workers", "cf-kv"]

[domains]
selected = ["security", "network", "documentation", "html", "seo", "database"]
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Framework.Selected != "svelte" {
		t.Errorf("Framework.Selected = %q", cfg.Framework.Selected)
	}
	if got := cfg.PlatformOverrides["cloudflare"].Exclude; !reflect.DeepEqual(got, []string{"cf-workers", "cf-kv"}) {
		t.Errorf("exclude = %v", got)
	}
	if len(cfg.Domains.Selected) != 6 {
		t.Errorf("Domains.Selected has %d entries, want 6", len(cfg.Domains.Selected))
	}
}

func TestLoad_goBackendAWS(t *testing.T) {
	// Spec §2.3 example: Go backend on AWS Lambda
	content := `
[appframes.schema]
version = 2

[framework]
selected = "go"

[platform]
selected = "aws"

[platform.aws]
exclude = ["aws-rds"]

[domains]
selected = ["security", "network", "database"]
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Platform.Selected != "aws" {
		t.Errorf("Platform.Selected = %q, want aws", cfg.Platform.Selected)
	}
	if got := cfg.PlatformOverrides["aws"].Exclude; !reflect.DeepEqual(got, []string{"aws-rds"}) {
		t.Errorf("aws.exclude = %v", got)
	}
}

func TestLoad_perFrameOverrides(t *testing.T) {
	// Spec §8 + §8.2 example: per-frame override with severity AND enabled flag
	content := `
[appframes.schema]
version = 2

[framework]
selected = "html"

[platform]
selected = "cloudflare"

[domains]
selected = ["security"]

[frames.overrides]
"html-placeholder-content" = { severity = "INFO" }
"cf-d1/sql-injection" = { enabled = false }
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := cfg.Frames.Overrides["html-placeholder-content"]
	if got.Severity == nil || *got.Severity != "INFO" {
		t.Errorf("html-placeholder-content severity = %v, want INFO", got.Severity)
	}
	got = cfg.Frames.Overrides["cf-d1/sql-injection"]
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("cf-d1/sql-injection enabled = %v, want false", got.Enabled)
	}
}

func TestLoad_appliedKitsArray(t *testing.T) {
	// Spec §8 - decision #20 - array form for applied-kit metadata
	content := `
[appframes.schema]
version = 2

[framework]
selected = "svelte"

[platform]
selected = "cloudflare"

[domains]
selected = ["security"]

[[meta.applied_kit]]
id = "svelte-cf-pages-marketing"
semver = "1.2"
hash = "sha256:abc123"
applied_at = "2026-06-05T15:30:00Z"

[[meta.applied_kit]]
id = "security-strict-extra"
semver = "1.0"
hash = "sha256:def456"
applied_at = "2026-06-05T15:30:00Z"
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Meta.AppliedKits) != 2 {
		t.Fatalf("AppliedKits has %d entries, want 2", len(cfg.Meta.AppliedKits))
	}
	first := cfg.Meta.AppliedKits[0]
	if first.ID != "svelte-cf-pages-marketing" {
		t.Errorf("AppliedKits[0].ID = %q", first.ID)
	}
	if first.Semver != "1.2" {
		t.Errorf("AppliedKits[0].Semver = %q", first.Semver)
	}
	if first.Hash != "sha256:abc123" {
		t.Errorf("AppliedKits[0].Hash = %q", first.Hash)
	}
}

func TestLoad_rejectsSchemaVersion1(t *testing.T) {
	// A v1 config should be rejected by the v2 loader - caller uses ReadAny to dispatch.
	content := `
applied_kits = ["cf-pages-project"]
`
	path := writeTemp(t, "appframes.toml", content)
	_, err := v2.Load(path)
	if err == nil {
		t.Fatal("expected error loading v1 config with v2 loader")
	}
}

func TestLoad_rejectsMissingSchemaVersion(t *testing.T) {
	// Even with v2-shaped sections, missing [appframes.schema].version = 2 is a v1 implicit.
	content := `
[framework]
selected = "html"
`
	path := writeTemp(t, "appframes.toml", content)
	_, err := v2.Load(path)
	if err == nil {
		t.Fatal("expected error when schema.version != 2")
	}
}

func TestSelection_buildsFromConfig(t *testing.T) {
	// Converting v2.Config to buckets.Selection - the load-bearing bridge.
	content := `
[appframes.schema]
version = 2

[framework]
selected = "html"

[platform]
selected = "cloudflare"

[platform.cloudflare]
exclude = ["cf-workers"]

[domains]
selected = ["security", "html"]

[core]
enabled = true

[frames.overrides]
"placeholder-content" = { enabled = false }
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sel := cfg.Selection()
	if !sel.CoreEnabled {
		t.Error("CoreEnabled should be true")
	}
	if sel.Framework != "html" {
		t.Errorf("Framework = %q", sel.Framework)
	}
	if sel.Platform != "cloudflare" {
		t.Errorf("Platform = %q", sel.Platform)
	}
	if got := sel.PlatformExclude["cloudflare"]; !reflect.DeepEqual(got, []string{"cf-workers"}) {
		t.Errorf("PlatformExclude[cloudflare] = %v", got)
	}
	if !reflect.DeepEqual(sel.Domains, []string{"security", "html"}) {
		t.Errorf("Domains = %v", sel.Domains)
	}
	if v, ok := sel.FrameOverrides["placeholder-content"]; !ok || v {
		t.Errorf("FrameOverrides[placeholder-content] = %v ok=%v, want false true", v, ok)
	}
}

func TestLoad_coreDefaultsToEnabledWhenAbsent(t *testing.T) {
	// [core] section optional; defaults to enabled = true.
	content := `
[appframes.schema]
version = 2

[framework]
selected = "html"

[domains]
selected = ["security"]
`
	path := writeTemp(t, "appframes.toml", content)
	cfg, err := v2.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Core.Enabled {
		t.Error("Core.Enabled should default to true when [core] section absent")
	}
}
