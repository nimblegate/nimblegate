// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package v2 loads the v2 appframes.toml schema and exposes its content as a
// typed Go struct. The v2 schema replaces the v1 flat kit abstraction with the
// three-axis (framework × platform × domain) + universal core model per the
// kit-architecture-three-axis design spec (2026-06-05).
//
// Package boundary: v2 is a sibling of internal/config; the existing v1 reader
// stays in internal/config to preserve backwards-compat. Callers wanting
// version-aware loading dispatch via internal/config.ReadAny (see Phase B
// Task B2).
package v2

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/buckets"
)

// Config is the parsed v2 appframes.toml. Field structure mirrors spec §8.
type Config struct {
	Appframes         AppframesMeta             `toml:"appframes"`
	Framework         FrameworkSel              `toml:"framework"`
	Platform          PlatformSel               `toml:"platform"`
	PlatformOverrides map[string]VendorOverride `toml:"-"` // populated from [platform.<vendor>] subtables; see Load
	Domains           DomainsSel                `toml:"domains"`
	Core              CoreSel                   `toml:"core"`
	Frames            FramesOverrides           `toml:"frames"`
	Meta              MetaSection               `toml:"meta"`
	Whitelist         map[string]any            `toml:"whitelist"` // existing schema preserved verbatim
}

// AppframesMeta carries the [nimblegate.*] subtables. Currently only [appframes.schema]
// is used; future subtables (e.g., [nimblegate.profile]) extend here without
// conflicting with the [platform], [framework], etc. axis-selection sections.
type AppframesMeta struct {
	Schema SchemaInfo `toml:"schema"`
}

// SchemaInfo holds [appframes.schema]. Version is the v1-vs-v2 discriminator -
// v2 loader rejects anything where Version != 2.
type SchemaInfo struct {
	Version int `toml:"version"`
}

// FrameworkSel is the single-select framework axis [framework].
type FrameworkSel struct {
	Selected string `toml:"selected"`
}

// PlatformSel is the single-select platform axis [platform]. Per-vendor
// opt-out lives in PlatformOverrides keyed by vendor name (e.g.
// [platform.cloudflare] exclude = ["cf-workers"]).
type PlatformSel struct {
	Selected string `toml:"selected"`
}

// VendorOverride is the per-vendor opt-out list ([platform.<vendor>]).
type VendorOverride struct {
	Exclude []string `toml:"exclude"`
}

// DomainsSel is the multi-select domain axis [domains].
type DomainsSel struct {
	Selected []string `toml:"selected"`
}

// CoreSel is the universal floor [core]. Defaults to enabled when absent.
type CoreSel struct {
	Enabled bool `toml:"enabled"`
}

// FramesOverrides wraps the [frames.overrides] table.
type FramesOverrides struct {
	Overrides map[string]FrameOverride `toml:"overrides"`
}

// FrameOverride is one entry in [frames.overrides]. Pointer fields distinguish
// "field absent" (nil) from "field set to zero value" (non-nil with zero value).
type FrameOverride struct {
	Severity *string `toml:"severity"`
	Enabled  *bool   `toml:"enabled"`
}

// MetaSection wraps [[meta.applied_kit]] array entries - spec §8 + decision #20.
type MetaSection struct {
	AppliedKits []AppliedKit `toml:"applied_kit"`
}

// AppliedKit records a kit application: which kit, what version, what hash
// (per decision #18 hybrid semver+hash), and when. See spec §7.5 for usage
// in update-detection.
type AppliedKit struct {
	ID        string `toml:"id"`
	Semver    string `toml:"semver"`
	Hash      string `toml:"hash"`
	AppliedAt string `toml:"applied_at"`
}

// Load reads the file at path as v2 schema. Returns an error when the schema
// version isn't 2 - caller is expected to dispatch via ReadAny when the source
// version isn't known.
//
// The [platform.<vendor>] subtables don't fit cleanly into a typed struct
// because the vendor name is the table key. Load parses them via a generic
// any-typed map first, then projects to PlatformOverrides for consumers.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config v2: read %s: %w", path, err)
	}

	var cfg Config
	// Default Core to enabled before parsing; explicit false in the TOML overrides.
	cfg.Core.Enabled = true

	if err := toml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("config v2: parse %s: %w", path, err)
	}

	if cfg.Appframes.Schema.Version != 2 {
		return nil, fmt.Errorf("config v2: schema.version = %d, want 2 (use config.ReadAny to dispatch v1)", cfg.Appframes.Schema.Version)
	}

	// Re-parse the platform overrides as a sub-table map. TOML's any-typed map
	// for [platform] would conflict with the typed PlatformSel; instead we walk
	// the raw TOML tree to extract [platform.<vendor>] entries.
	var rawTree map[string]any
	if err := toml.Unmarshal(raw, &rawTree); err != nil {
		return nil, fmt.Errorf("config v2: re-parse for platform overrides: %w", err)
	}
	cfg.PlatformOverrides = make(map[string]VendorOverride)
	if platRaw, ok := rawTree["platform"].(map[string]any); ok {
		for vendor, sub := range platRaw {
			if vendor == "selected" {
				continue
			}
			subMap, ok := sub.(map[string]any)
			if !ok {
				continue
			}
			excludeRaw, ok := subMap["exclude"].([]any)
			if !ok {
				continue
			}
			vo := VendorOverride{}
			for _, e := range excludeRaw {
				if s, ok := e.(string); ok {
					vo.Exclude = append(vo.Exclude, s)
				}
			}
			cfg.PlatformOverrides[vendor] = vo
		}
	}

	return &cfg, nil
}

// Selection projects this Config into the buckets.Selection used by the engine.
// Bridges the config layer to the bucket resolution layer.
func (c *Config) Selection() buckets.Selection {
	sel := buckets.Selection{
		CoreEnabled:     c.Core.Enabled,
		Framework:       c.Framework.Selected,
		Platform:        c.Platform.Selected,
		PlatformExclude: make(map[string][]string),
		Domains:         append([]string{}, c.Domains.Selected...),
		FrameOverrides:  make(map[string]bool),
	}
	for vendor, vo := range c.PlatformOverrides {
		sel.PlatformExclude[vendor] = append([]string{}, vo.Exclude...)
	}
	for frameID, fo := range c.Frames.Overrides {
		if fo.Enabled != nil {
			sel.FrameOverrides[frameID] = *fo.Enabled
		}
	}
	return sel
}
