// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package v1tov2 translates v1 appframes.toml configs into v2 axis-selection
// shape. The canonical translation table is documented in the
// kit-architecture-three-axis design spec §9.1.
//
// The translator's input is the v1 applied-kit list (from the [ui] applied_kits
// section that v1 used to record which kits the operator applied). The output
// is a v2.Config with the equivalent axis selections + the per-vendor
// opt-out exclusions that match each kit's intent.
//
// Zero-delta validation (the load-bearing migration gate) is implemented in
// validate.go alongside this file; it compares findings produced by v1
// resolution vs v2 resolution against the same tree and surfaces any
// discrepancy.
package v1tov2

import (
	"fmt"
	"sort"

	v2 "nimblegate/internal/config/v2"
)

// Input bundles the v1-side state that drives translation. Currently just the
// applied-kit list (the dominant signal in v1 config shape); future fields
// can carry the [scan] excludes, [frames.overrides] per-frame settings, etc.,
// which translate verbatim.
type Input struct {
	AppliedKits []string
}

// Result wraps the produced v2.Config plus any warnings emitted during
// translation (e.g., unknown kit names that the translator couldn't map).
// Used by callers that want to surface warnings; Translate returns just the
// Config for the common case.
type Result struct {
	Config   *v2.Config
	Warnings []string
}

// Translate produces a v2.Config from the supplied v1 kit list. Unknown kits
// are silently skipped; use TranslateWithErrors to surface warnings.
func Translate(input Input) *v2.Config {
	return TranslateWithErrors(input).Config
}

// TranslateWithErrors produces both the v2.Config and any warnings (e.g.,
// unknown kit names) for callers that want to surface them - typically the
// `nimblegate migrate-config` CLI.
func TranslateWithErrors(input Input) Result {
	cfg := &v2.Config{
		Core:              v2.CoreSel{Enabled: true},
		PlatformOverrides: make(map[string]v2.VendorOverride),
		Frames:            v2.FramesOverrides{Overrides: make(map[string]v2.FrameOverride)},
	}
	cfg.Appframes.Schema.Version = 2

	domainSet := make(map[string]struct{}) // for set-union semantics

	var warnings []string

	// v1 "core" kit was a bigger bundle than v2 "core/" - it included
	// frames that v2 places in opt-in domains (filesystem, security,
	// network, database) plus universal git/credential frames. To preserve
	// v1 core's frame coverage under v2, any kit that built on v1 core
	// must add these domains explicitly. All of [core, web-app, cf-pages-
	// project, cf-workers-project] include v1 core's frames.
	coreInclusiveDomains := []string{"filesystem", "security", "network", "database"}
	includesV1Core := false

	for _, kit := range input.AppliedKits {
		switch kit {
		case "core":
			// Core is always implicit in v2 (cfg.Core.Enabled = true above).
			// Need to preserve v1 core's domain coverage.
			includesV1Core = true

		case "web-app":
			cfg.Framework.Selected = "html"
			addDomain(domainSet, "html")
			addDomain(domainSet, "documentation")
			includesV1Core = true // v1 web-app embedded v1 core

		case "cf-pages-project":
			cfg.Framework.Selected = "html"
			cfg.Platform.Selected = "cloudflare"
			cfg.PlatformOverrides["cloudflare"] = mergeExclude(cfg.PlatformOverrides["cloudflare"], "cf-workers")
			addDomain(domainSet, "html")
			addDomain(domainSet, "documentation")
			addDomain(domainSet, "seo")
			includesV1Core = true // v1 cf-pages-project embedded v1 core

		case "cf-workers-project":
			cfg.Platform.Selected = "cloudflare"
			cfg.PlatformOverrides["cloudflare"] = mergeExclude(cfg.PlatformOverrides["cloudflare"], "cf-pages")
			addDomain(domainSet, "network")
			addDomain(domainSet, "database")
			includesV1Core = true // v1 cf-workers-project embedded v1 core

		case "security-strict":
			addDomain(domainSet, "security")
			addDomain(domainSet, "encoding")

		case "encoding-strict":
			addDomain(domainSet, "encoding")

		default:
			warnings = append(warnings, fmt.Sprintf("unknown v1 kit %q - no v2 translation rule; frames originally enabled by this kit will not migrate automatically", kit))
		}
	}

	// If any applied kit built on v1 core, add the domains that hold v1
	// core's non-universal frames so coverage matches.
	if includesV1Core {
		for _, d := range coreInclusiveDomains {
			addDomain(domainSet, d)
		}
	}

	// Project the domain set to a sorted slice for stable output.
	cfg.Domains.Selected = sortedKeys(domainSet)

	return Result{Config: cfg, Warnings: warnings}
}

// addDomain inserts a concept into the domain set (deduplicates).
func addDomain(set map[string]struct{}, concept string) {
	set[concept] = struct{}{}
}

// mergeExclude inserts a sub-bucket into the vendor's exclude list, preserving
// previously-set entries and deduplicating.
func mergeExclude(existing v2.VendorOverride, subBucket string) v2.VendorOverride {
	for _, e := range existing.Exclude {
		if e == subBucket {
			return existing
		}
	}
	existing.Exclude = append(existing.Exclude, subBucket)
	sort.Strings(existing.Exclude)
	return existing
}

// sortedKeys returns the map's keys in deterministic sorted order. Used to
// produce stable Domains.Selected output across runs.
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
