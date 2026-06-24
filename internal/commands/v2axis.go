// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"nimblegate/internal/frames"
)

// V2 axis classification - UI-only mapping from a frame's v1 frontmatter to
// the v2 three-axis + core model. The frame data isn't touched; this is the
// rule the dashboard uses to render the v2 mental model (Core / Framework /
// Platform / Domain) instead of the original 10 v1 categories.
//
// Classification precedence (most-specific wins):
//
//  1. Frame has any Platform tag         → Platform axis
//  2. Frame has any Framework tag        → Framework axis
//  3. Frame's category in coreCategories → Core axis (universal floor)
//  4. Otherwise                          → Domain axis (uses v1 category)
//
// The v2 stdlib tree at internal/stdlib/v2/ places each frame at exactly one
// path; this classifier follows the same rule (one frame, one axis).

type v2Axis string

const (
	v2AxisCore      v2Axis = "core"
	v2AxisFramework v2Axis = "framework"
	v2AxisPlatform  v2Axis = "platform"
	v2AxisDomain    v2Axis = "domain"
)

// v2AxisOrder is the display order for the 4 top-level axis groups.
// Alphabetical by Display so the dashboard reads top-to-bottom in
// alphabetical order (Core, Domain, Framework, Platform).
var v2AxisOrder = []struct {
	id      v2Axis
	display string
}{
	{v2AxisCore, "Core"},
	{v2AxisDomain, "Domain"},
	{v2AxisFramework, "Framework"},
	{v2AxisPlatform, "Platform"},
}

// canonicalFrameworks is the static list of framework axis values per spec
// §6.1. Surfaced as visible-but-empty sub-buckets under Framework so the
// operator can see what the axis covers even before any frames declare a
// specific framework. NOTE: 'html' lives under Domain, not Framework, per
// the cross-cutting-concern discussion (a SvelteKit project still emits
// HTML that needs alt attributes - HTML isn't the project's framework).
var canonicalFrameworks = []string{"svelte", "astro", "react", "vue", "go", "python"}

// canonicalPlatformVendors is the analogous static list for the Platform
// axis. Visible-but-empty surfacing matches Framework so the operator sees
// the axis shape consistently across both pages.
var canonicalPlatformVendors = []string{"cloudflare", "aws", "vercel", "netlify"}

// coreCategories lists v1 categories whose frames represent the universal
// floor - they apply to every project regardless of stack. git-safety
// frames and shell-command-safety frames belong here per the v2 spec's
// "core stays as the only universal group" decision.
var coreCategories = map[string]bool{
	"git":      true,
	"commands": true,
}

// domainDisplayOverride relabels v1 category names when surfacing them as
// v2 domain sub-buckets. The frame's frontmatter category is unchanged -
// this is purely the dashboard's label. "web" → "HTML" because the v1
// "web" category contained only HTML-output frames (img-alt, required-meta,
// markup-valid, etc), which belong to the "html" domain in v2.
var domainDisplayOverride = map[string]string{
	"web": "HTML",
}

// v2Classification is one frame's resolved axis placement. Vendor and
// SubBucket are populated for Platform; Domain populates Sub. Empty fields
// mean the frame sits at the axis's top level (rare - most frames hit a
// sub-bucket).
type v2Classification struct {
	Axis    v2Axis
	Vendor  string // platform vendor (e.g., "cloudflare"); platform axis only
	Sub     string // platform sub-bucket / framework / domain sub-key
	Display string // sub-bucket display label
}

// classifyFrameAxis applies the precedence rules above to a single frame.
// Pure - no I/O.
func classifyFrameAxis(f frames.Frame) v2Classification {
	plats := effectivePlatforms(f.Frontmatter.Platform)
	if len(plats) > 0 {
		vendor, sub := splitVendorSub(plats)
		return v2Classification{
			Axis:    v2AxisPlatform,
			Vendor:  vendor,
			Sub:     sub,
			Display: titleCase(firstNonEmpty(sub, vendor)),
		}
	}
	if len(f.Frontmatter.Framework) > 0 {
		fw := f.Frontmatter.Framework[0]
		return v2Classification{
			Axis:    v2AxisFramework,
			Sub:     fw,
			Display: titleCase(fw),
		}
	}
	cat := string(f.Frontmatter.Category)
	if coreCategories[cat] {
		return v2Classification{
			Axis:    v2AxisCore,
			Sub:     cat,
			Display: titleCase(cat),
		}
	}
	display := titleCase(cat)
	if override, ok := domainDisplayOverride[cat]; ok {
		display = override
	}
	return v2Classification{
		Axis:    v2AxisDomain,
		Sub:     cat,
		Display: display,
	}
}

// splitVendorSub splits an effectivePlatforms-cleaned list into a vendor
// and (optional) sub-bucket. Inputs after effectivePlatforms are one of:
//   - [vendor]            (no sub-bucket - e.g., [cloudflare])
//   - [sub-bucket]        (sub-bucket whose vendor was stripped - e.g., [cf-pages])
//   - [vendor, other]     (rare multi-vendor frame - first becomes primary)
//
// For sub-bucket-only input, vendorOf supplies the parent vendor so the
// dashboard can still nest under Platform > <vendor> > <sub-bucket>.
func splitVendorSub(plats []string) (vendor, sub string) {
	if len(plats) == 0 {
		return "", ""
	}
	first := plats[0]
	if parent, isSub := vendorOf[first]; isSub {
		return parent, first
	}
	return first, ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
