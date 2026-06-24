// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/frames"
)

func makeFrame(name string, cat frames.Category, plat, fw []string) frames.Frame {
	return frames.Frame{
		Frontmatter: frames.Frontmatter{
			Name:      name,
			Category:  cat,
			Platform:  plat,
			Framework: fw,
		},
	}
}

func TestClassifyFrameAxis_platformOnlyVendor(t *testing.T) {
	f := makeFrame("cf-graphql-schema-match", "app-correctness", []string{"cloudflare"}, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisPlatform {
		t.Errorf("axis = %v, want platform", got.Axis)
	}
	if got.Vendor != "cloudflare" {
		t.Errorf("vendor = %q, want cloudflare", got.Vendor)
	}
	if got.Sub != "" {
		t.Errorf("sub = %q, want empty (vendor-only)", got.Sub)
	}
}

func TestClassifyFrameAxis_platformVendorPlusSubBucket(t *testing.T) {
	// effectivePlatforms drops the vendor when sub-bucket is also tagged;
	// classifyFrameAxis then resolves [cf-pages] alone back to its parent
	// vendor cloudflare via vendorOf, with cf-pages as the sub.
	f := makeFrame("cf-pages-headers-baseline", "security", []string{"cloudflare", "cf-pages"}, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisPlatform {
		t.Errorf("axis = %v, want platform", got.Axis)
	}
	if got.Vendor != "cloudflare" {
		t.Errorf("vendor = %q, want cloudflare", got.Vendor)
	}
	if got.Sub != "cf-pages" {
		t.Errorf("sub = %q, want cf-pages", got.Sub)
	}
}

func TestClassifyFrameAxis_framework(t *testing.T) {
	f := makeFrame("svelte-specific", "app-correctness", nil, []string{"svelte"})
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisFramework {
		t.Errorf("axis = %v, want framework", got.Axis)
	}
	if got.Sub != "svelte" {
		t.Errorf("sub = %q, want svelte", got.Sub)
	}
}

func TestClassifyFrameAxis_platformTrumpsFramework(t *testing.T) {
	// Precedence: a frame tagged with BOTH platform and framework lands on
	// platform (more specific identity per the v2 stdlib placement rules).
	f := makeFrame("hybrid", "app-correctness", []string{"cloudflare"}, []string{"svelte"})
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisPlatform {
		t.Errorf("axis = %v, want platform (precedence over framework)", got.Axis)
	}
}

func TestClassifyFrameAxis_coreFromGitCategory(t *testing.T) {
	f := makeFrame("folder-branch-lock", "git", nil, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisCore {
		t.Errorf("axis = %v, want core (git is universal floor)", got.Axis)
	}
	if got.Sub != "git" {
		t.Errorf("sub = %q, want git", got.Sub)
	}
}

func TestClassifyFrameAxis_coreFromCommandsCategory(t *testing.T) {
	f := makeFrame("rm-rf-warn", "commands", nil, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisCore {
		t.Errorf("axis = %v, want core (commands is universal floor)", got.Axis)
	}
}

func TestClassifyFrameAxis_domainSecurity(t *testing.T) {
	f := makeFrame("no-hardcoded-credentials", "security", nil, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisDomain {
		t.Errorf("axis = %v, want domain", got.Axis)
	}
	if got.Sub != "security" {
		t.Errorf("sub = %q, want security", got.Sub)
	}
	if got.Display != "Security" {
		t.Errorf("display = %q, want Security", got.Display)
	}
}

func TestClassifyFrameAxis_domainWebRelabelsAsHTML(t *testing.T) {
	// v1 category 'web' relabels to "HTML" display in v2 domain axis
	// (frame frontmatter still says category=web; UI-only override).
	f := makeFrame("html-img-alt", "web", nil, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisDomain {
		t.Errorf("axis = %v, want domain", got.Axis)
	}
	if got.Sub != "web" {
		t.Errorf("sub = %q, want web (id unchanged)", got.Sub)
	}
	if got.Display != "HTML" {
		t.Errorf("display = %q, want HTML (v1 web → v2 html relabel)", got.Display)
	}
}

func TestClassifyFrameAxis_domainEncoding(t *testing.T) {
	f := makeFrame("no-bom", "encoding", nil, nil)
	got := classifyFrameAxis(f)
	if got.Axis != v2AxisDomain || got.Sub != "encoding" || got.Display != "Encoding" {
		t.Errorf("classification = %+v, want domain/encoding/Encoding", got)
	}
}

func TestSplitVendorSub_vendorOnly(t *testing.T) {
	vendor, sub := splitVendorSub([]string{"cloudflare"})
	if vendor != "cloudflare" || sub != "" {
		t.Errorf("got vendor=%q sub=%q, want cloudflare/empty", vendor, sub)
	}
}

func TestSplitVendorSub_subBucketResolvesToParent(t *testing.T) {
	vendor, sub := splitVendorSub([]string{"cf-pages"})
	if vendor != "cloudflare" || sub != "cf-pages" {
		t.Errorf("got vendor=%q sub=%q, want cloudflare/cf-pages", vendor, sub)
	}
}

func TestSplitVendorSub_unknownVendorPassesThrough(t *testing.T) {
	vendor, sub := splitVendorSub([]string{"vercel"})
	if vendor != "vercel" || sub != "" {
		t.Errorf("got vendor=%q sub=%q, want vercel/empty", vendor, sub)
	}
}

func TestSplitVendorSub_empty(t *testing.T) {
	vendor, sub := splitVendorSub(nil)
	if vendor != "" || sub != "" {
		t.Errorf("got vendor=%q sub=%q, want empty/empty", vendor, sub)
	}
}
