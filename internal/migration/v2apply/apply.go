// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package v2apply implements the set-union + explicit-destructive conflict
// semantics for applying a v2 kit's selections into an operator's
// appframes.toml. See spec §7.3 for the rule set.
package v2apply

import (
	"fmt"
	"sort"
	"time"

	v2 "nimblegate/internal/config/v2"
	v2kits "nimblegate/internal/kits/v2"
)

// Mode controls how single-select axis conflicts are resolved.
type Mode int

const (
	// ModeMerge is the default - set-valued axes union; single-select
	// conflicts ERROR (operator must explicitly opt in).
	ModeMerge Mode = iota
	// ModeOverwrite replaces single-select axes if they conflict, with
	// a WARN surfaced to the operator.
	ModeOverwrite
)

// Result reports what changed during the apply.
type Result struct {
	Warnings        []string // single-select overrides, unknown axes, etc.
	AppliedKit      v2.AppliedKit
	BeforeFramework string
	BeforePlatform  string
	BeforeDomains   []string
}

// Apply unions a v2 kit's selections into the operator's v2.Config per the
// spec §7.3 rules:
//
//   - Set-valued axes (domains, platform_exclude) → true set-union
//     (de-duplicated; no order semantics)
//   - Single-select axes (framework, platform):
//   - Absent in config → set to kit's value
//   - Present and equal → no-op
//   - Present and different → ERROR in ModeMerge; WARN+override in
//     ModeOverwrite
//
// Records the application in cfg.Meta.AppliedKits as an array entry with
// kit_id, semver, hash, and applied_at timestamp.
func Apply(cfg *v2.Config, kit v2kits.Kit, mode Mode, now time.Time) (Result, error) {
	if cfg == nil {
		return Result{}, fmt.Errorf("v2apply: cfg is nil")
	}
	if cfg.PlatformOverrides == nil {
		cfg.PlatformOverrides = make(map[string]v2.VendorOverride)
	}

	result := Result{
		BeforeFramework: cfg.Framework.Selected,
		BeforePlatform:  cfg.Platform.Selected,
		BeforeDomains:   append([]string{}, cfg.Domains.Selected...),
	}

	// Framework - single-select.
	if kit.Selections.Framework != "" {
		switch {
		case cfg.Framework.Selected == "":
			cfg.Framework.Selected = kit.Selections.Framework
		case cfg.Framework.Selected == kit.Selections.Framework:
			// no-op
		default:
			if mode == ModeMerge {
				return Result{}, fmt.Errorf("v2apply: framework conflict - existing %q vs kit %q; use --overwrite to replace", cfg.Framework.Selected, kit.Selections.Framework)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("framework: replaced %q with %q from kit %q", cfg.Framework.Selected, kit.Selections.Framework, kit.KitID))
			cfg.Framework.Selected = kit.Selections.Framework
		}
	}

	// Platform - single-select.
	if kit.Selections.Platform != "" {
		switch {
		case cfg.Platform.Selected == "":
			cfg.Platform.Selected = kit.Selections.Platform
		case cfg.Platform.Selected == kit.Selections.Platform:
			// no-op
		default:
			if mode == ModeMerge {
				return Result{}, fmt.Errorf("v2apply: platform conflict - existing %q vs kit %q; use --overwrite to replace", cfg.Platform.Selected, kit.Selections.Platform)
			}
			result.Warnings = append(result.Warnings, fmt.Sprintf("platform: replaced %q with %q from kit %q", cfg.Platform.Selected, kit.Selections.Platform, kit.KitID))
			cfg.Platform.Selected = kit.Selections.Platform
		}
	}

	// platform_exclude - set-union per vendor.
	for vendor, kitExcludes := range kit.Selections.PlatformExclude {
		current := cfg.PlatformOverrides[vendor]
		seen := make(map[string]bool, len(current.Exclude))
		for _, e := range current.Exclude {
			seen[e] = true
		}
		for _, e := range kitExcludes {
			if !seen[e] {
				current.Exclude = append(current.Exclude, e)
				seen[e] = true
			}
		}
		sort.Strings(current.Exclude)
		cfg.PlatformOverrides[vendor] = current
	}

	// Domains - set-union.
	domainSeen := make(map[string]bool, len(cfg.Domains.Selected))
	for _, d := range cfg.Domains.Selected {
		domainSeen[d] = true
	}
	for _, d := range kit.Selections.Domains {
		if !domainSeen[d] {
			cfg.Domains.Selected = append(cfg.Domains.Selected, d)
			domainSeen[d] = true
		}
	}
	sort.Strings(cfg.Domains.Selected)

	// Record applied kit in [[meta.applied_kit]].
	applied := v2.AppliedKit{
		ID:        kit.KitID,
		Semver:    kit.Semver,
		Hash:      kit.Hash(),
		AppliedAt: now.UTC().Format(time.RFC3339),
	}
	// Replace existing entry for same kit_id (avoid duplicate records).
	replaced := false
	for i := range cfg.Meta.AppliedKits {
		if cfg.Meta.AppliedKits[i].ID == kit.KitID {
			cfg.Meta.AppliedKits[i] = applied
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Meta.AppliedKits = append(cfg.Meta.AppliedKits, applied)
	}

	// Ensure core defaults on
	if !cfg.Core.Enabled {
		cfg.Core.Enabled = true
	}

	result.AppliedKit = applied
	return result, nil
}
