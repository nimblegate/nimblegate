// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"sort"
	"strings"
	"testing"
)

// TestBuildPolicyView_webRendersUnderDomainHTML confirms the v1 'web'
// category surfaces under Domain > HTML (sub-bucket Name="web", Display
// "HTML"). UI-only relabel - frame frontmatter still says category=web.
func TestBuildPolicyView_webRendersUnderDomainHTML(t *testing.T) {
	vm := buildPolicyView("", "test", nil)
	var domainCat *policyCategory
	for i := range vm.Categories {
		if vm.Categories[i].ID == "domain" {
			domainCat = &vm.Categories[i]
			break
		}
	}
	if domainCat == nil {
		t.Fatal("domain axis missing")
	}
	var htmlSub *policySubcategory
	for i := range domainCat.Subcategories {
		if domainCat.Subcategories[i].Name == "web" {
			htmlSub = &domainCat.Subcategories[i]
			break
		}
	}
	if htmlSub == nil {
		t.Fatalf("'web' sub-bucket missing from Domain; got: %v", subNames(domainCat.Subcategories))
	}
	if htmlSub.Display != "HTML" {
		t.Errorf("domain/web Display = %q, want HTML", htmlSub.Display)
	}
}

// TestBuildPolicyView_platformNestedCloudflareChildren confirms the
// platform tree builds Cloudflare as a parent with cf-* sub-buckets nested
// as Children rather than flat siblings. myapp-shape frames produce
// at minimum a cf-pages child and at least one vendor-direct frame on
// cloudflare itself.
func TestBuildPolicyView_platformNestedCloudflareChildren(t *testing.T) {
	vm := buildPolicyView("", "test", nil)
	var platCat *policyCategory
	for i := range vm.Categories {
		if vm.Categories[i].ID == "platform" {
			platCat = &vm.Categories[i]
			break
		}
	}
	if platCat == nil {
		t.Fatal("platform category missing")
	}

	var cloudflare *policySubcategory
	for i := range platCat.Subcategories {
		if platCat.Subcategories[i].Name == "cloudflare" {
			cloudflare = &platCat.Subcategories[i]
			break
		}
	}
	if cloudflare == nil {
		t.Fatalf("cloudflare sub missing from platform; got: %v", subNames(platCat.Subcategories))
	}

	if len(cloudflare.Frames) == 0 {
		t.Error("cloudflare vendor has no direct Frames: vendor-only frames (cf-graphql-*) should sit here")
	}
	if len(cloudflare.Children) == 0 {
		t.Fatalf("cloudflare has no Children; expected cf-pages nested. Subs: %v", subNames(platCat.Subcategories))
	}

	childNames := make([]string, 0, len(cloudflare.Children))
	for _, c := range cloudflare.Children {
		childNames = append(childNames, c.Name)
	}
	sort.Strings(childNames)
	if !containsName(childNames, "cf-pages") {
		t.Errorf("cf-pages not in cloudflare.Children; got %v", childNames)
	}

	// Sibling-level cf-pages should NOT exist when a cloudflare parent exists.
	for _, s := range platCat.Subcategories {
		if strings.HasPrefix(s.Name, "cf-") {
			t.Errorf("sub-bucket %q appears as platform sibling; should be nested under cloudflare", s.Name)
		}
	}
}

func subNames(subs []policySubcategory) []string {
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, s.Name)
	}
	return out
}

func containsName(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestBuildNestedPlatformSubs_orphanSubBucketWhenNoVendorPresent verifies
// the helper's fallback: if frames tagged only with a sub-bucket exist but
// no vendor-only frames did, the sub-bucket renders as a top-level entry
// rather than getting silently dropped.
func TestBuildNestedPlatformSubs_orphanSubBucketWhenNoVendorPresent(t *testing.T) {
	in := map[string][]policyFrameRef{
		"cf-pages": {{ID: "x/y"}},
	}
	got := buildNestedPlatformSubs(in)
	if len(got) != 1 || got[0].Name != "cf-pages" {
		t.Errorf("orphan sub-bucket should render as top-level; got %+v", got)
	}
}

// TestBuildNestedPlatformSubs_unrelatedVendorFlat verifies non-cloudflare
// vendors with no sub-bucket structure render as flat entries.
func TestBuildNestedPlatformSubs_unrelatedVendorFlat(t *testing.T) {
	in := map[string][]policyFrameRef{
		"aws":    {{ID: "a/b"}, {ID: "a/c"}},
		"vercel": {{ID: "v/x"}},
	}
	got := buildNestedPlatformSubs(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 vendor entries; got %d: %+v", len(got), got)
	}
	for _, s := range got {
		if len(s.Children) != 0 {
			t.Errorf("vendor %q got unexpected Children: %v", s.Name, s.Children)
		}
	}
}
