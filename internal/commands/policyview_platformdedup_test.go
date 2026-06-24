// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"
)

// TestBuildPolicyView_platformVendorDedupedFromSubBucket verifies the bug
// reported 2026-06-06: frames like security/cf-pages-headers-baseline
// (platform: [cloudflare, cf-pages]) were appearing under BOTH "Cf Pages"
// AND "Cloudflare" in the dashboard, doubling the row.
//
// After effectivePlatforms drops the vendor when a sub-bucket is also
// present, those frames should only appear under cf-pages. Cloudflare-only
// frames (cf-graphql-*) must still appear under cloudflare.
func TestBuildPolicyView_platformVendorDedupedFromSubBucket(t *testing.T) {
	vm := buildPolicyView("", "test", nil)

	// Locate the Platform category.
	var platCat *policyCategory
	for i := range vm.Categories {
		if vm.Categories[i].ID == "platform" {
			platCat = &vm.Categories[i]
			break
		}
	}
	if platCat == nil {
		t.Fatal("Platform category missing from policy view")
	}

	// After the nesting reshape, cf-pages lives under cloudflare's Children,
	// not as a sibling subcategory. Build a flat lookup keyed by Name that
	// walks one level of Children too.
	subByName := make(map[string][]string)
	for _, s := range platCat.Subcategories {
		ids := make([]string, len(s.Frames))
		for i, f := range s.Frames {
			ids[i] = f.ID
		}
		subByName[s.Name] = ids
		for _, child := range s.Children {
			cids := make([]string, len(child.Frames))
			for i, f := range child.Frames {
				cids[i] = f.ID
			}
			subByName[child.Name] = cids
		}
	}

	cf, hasCloudflare := subByName["cloudflare"]
	cfPages, hasCfPages := subByName["cf-pages"]
	if !hasCloudflare {
		t.Fatalf("expected 'cloudflare' sub; got subs: %v", subKeys(subByName))
	}
	if !hasCfPages {
		t.Fatalf("expected 'cf-pages' sub (nested under cloudflare); got subs: %v", subKeys(subByName))
	}

	// Structural check: cf-pages must be NESTED inside cloudflare's Children,
	// not present as a top-level platform sibling.
	for _, s := range platCat.Subcategories {
		if s.Name == "cf-pages" {
			t.Error("cf-pages appeared as a top-level platform sibling; expected nested under cloudflare.Children")
		}
	}
	var cloudflareSub *policySubcategory
	for i := range platCat.Subcategories {
		if platCat.Subcategories[i].Name == "cloudflare" {
			cloudflareSub = &platCat.Subcategories[i]
			break
		}
	}
	if cloudflareSub == nil || len(cloudflareSub.Children) == 0 {
		t.Fatal("cloudflare subcategory missing Children: nesting not applied")
	}
	foundCfPagesChild := false
	for _, c := range cloudflareSub.Children {
		if c.Name == "cf-pages" {
			foundCfPagesChild = true
		}
	}
	if !foundCfPagesChild {
		t.Error("cf-pages not found in cloudflare.Children: expected nested entry")
	}

	// Frames with [cloudflare, cf-pages] should appear ONLY under cf-pages,
	// not under cloudflare.
	dupSensitive := []string{
		"app-correctness/dynamic-env-declared",
		"app-correctness/prefer-static-public",
		"app-correctness/top-of-page-import-safety",
		"security/cf-pages-headers-baseline",
	}
	for _, fid := range dupSensitive {
		if containsID(cf, fid) {
			t.Errorf("frame %q leaked into Cloudflare group (should be cf-pages only)", fid)
		}
		if !contains(cfPages, fid) {
			t.Errorf("frame %q missing from Cf Pages group (expected after dedup)", fid)
		}
	}

	// Cloudflare-only frames (vendor in frontmatter but no sub-bucket) must
	// remain visible under the Cloudflare group.
	vendorOnly := []string{
		"app-correctness/cf-graphql-dataset-by-window",
		"app-correctness/cf-graphql-schema-match",
	}
	for _, fid := range vendorOnly {
		if !containsID(cf, fid) {
			t.Errorf("vendor-only frame %q missing from Cloudflare group", fid)
		}
	}
}

func containsID(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func subKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
