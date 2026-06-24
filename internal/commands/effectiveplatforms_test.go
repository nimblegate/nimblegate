// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"reflect"
	"sort"
	"testing"
)

func TestEffectivePlatforms_vendorPlusSubBucketDropsVendor(t *testing.T) {
	// Frames like prefer-static-public list both "cloudflare" + "cf-pages".
	// The vendor should be suppressed so the dashboard only shows the frame
	// under "Cf Pages", not duplicated under "Cloudflare" too.
	got := effectivePlatforms([]string{"cloudflare", "cf-pages"})
	if !reflect.DeepEqual(got, []string{"cf-pages"}) {
		t.Errorf("got %v, want [cf-pages]", got)
	}
}

func TestEffectivePlatforms_vendorOnlyKept(t *testing.T) {
	// cf-graphql-* style frames list "cloudflare" alone - no sub-bucket.
	// These must STAY under the Cloudflare group.
	got := effectivePlatforms([]string{"cloudflare"})
	if !reflect.DeepEqual(got, []string{"cloudflare"}) {
		t.Errorf("got %v, want [cloudflare]", got)
	}
}

func TestEffectivePlatforms_subBucketOnlyKept(t *testing.T) {
	// Frames could list a sub-bucket alone (no vendor) - pass through.
	got := effectivePlatforms([]string{"cf-workers"})
	if !reflect.DeepEqual(got, []string{"cf-workers"}) {
		t.Errorf("got %v, want [cf-workers]", got)
	}
}

func TestEffectivePlatforms_unrelatedPlatformsUnaffected(t *testing.T) {
	// aws + vercel + netlify aren't in the vendor map; both should pass.
	got := effectivePlatforms([]string{"aws", "vercel"})
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"aws", "vercel"}) {
		t.Errorf("got %v, want [aws vercel]", got)
	}
}

func TestEffectivePlatforms_multipleSubBucketsBothKept(t *testing.T) {
	// Future case: a frame could list multiple sub-buckets of the same
	// vendor (e.g., cf-pages + cf-workers + cloudflare). Vendor drops;
	// both sub-buckets stay.
	got := effectivePlatforms([]string{"cloudflare", "cf-pages", "cf-workers"})
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"cf-pages", "cf-workers"}) {
		t.Errorf("got %v, want [cf-pages cf-workers]", got)
	}
}

func TestEffectivePlatforms_emptyInput(t *testing.T) {
	got := effectivePlatforms(nil)
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestEffectivePlatforms_singleVendorPlusUnrelatedKept(t *testing.T) {
	// A frame could list cloudflare + aws (multi-platform frame). No
	// sub-bucket of either → neither suppressed.
	got := effectivePlatforms([]string{"cloudflare", "aws"})
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"aws", "cloudflare"}) {
		t.Errorf("got %v, want [aws cloudflare]", got)
	}
}
