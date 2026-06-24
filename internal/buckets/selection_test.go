// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package buckets_test

import (
	"testing"

	"nimblegate/internal/buckets"
)

func mustParse(t *testing.T, path string) buckets.Bucket {
	t.Helper()
	b, err := buckets.ParsePath(path)
	if err != nil {
		t.Fatalf("ParsePath(%q): %v", path, err)
	}
	return b
}

func TestSelection_coreEnabled(t *testing.T) {
	sel := buckets.Selection{CoreEnabled: true}
	b := mustParse(t, "core/git-no-force-push-main")
	if !sel.IsBucketActive(b) {
		t.Error("core bucket should be active when CoreEnabled=true")
	}
}

func TestSelection_coreDisabled(t *testing.T) {
	sel := buckets.Selection{CoreEnabled: false}
	b := mustParse(t, "core/git-no-force-push-main")
	if sel.IsBucketActive(b) {
		t.Error("core bucket should be inactive when CoreEnabled=false")
	}
}

func TestSelection_frameworkSingleMatch(t *testing.T) {
	sel := buckets.Selection{Framework: "svelte"}
	b := mustParse(t, "framework/svelte/svelte-security/scoped-style-xss")
	if !sel.IsBucketActive(b) {
		t.Error("framework bucket should activate when Lang matches Framework selection")
	}
}

func TestSelection_frameworkMismatch(t *testing.T) {
	sel := buckets.Selection{Framework: "react"}
	b := mustParse(t, "framework/svelte/svelte-security/scoped-style-xss")
	if sel.IsBucketActive(b) {
		t.Error("framework bucket should NOT activate when Lang differs from Framework selection")
	}
}

func TestSelection_platformVendorActive(t *testing.T) {
	sel := buckets.Selection{Platform: "cloudflare"}
	b := mustParse(t, "platform/cloudflare/cf-security/api-token-leak")
	if !sel.IsBucketActive(b) {
		t.Error("platform sub-bucket should activate when vendor matches and not excluded")
	}
}

func TestSelection_platformSubBucketExcluded(t *testing.T) {
	sel := buckets.Selection{
		Platform:        "cloudflare",
		PlatformExclude: map[string][]string{"cloudflare": {"cf-workers"}},
	}
	b := mustParse(t, "platform/cloudflare/cf-workers/router-baseline")
	if sel.IsBucketActive(b) {
		t.Error("excluded sub-bucket should NOT be active")
	}
	// Other sub-buckets under the same vendor should still be active.
	other := mustParse(t, "platform/cloudflare/cf-pages/headers-baseline")
	if !sel.IsBucketActive(other) {
		t.Error("non-excluded sub-bucket should still be active under the same vendor")
	}
}

func TestSelection_platformVendorMismatch(t *testing.T) {
	sel := buckets.Selection{Platform: "aws"}
	b := mustParse(t, "platform/cloudflare/cf-security/api-token-leak")
	if sel.IsBucketActive(b) {
		t.Error("platform bucket should NOT activate when vendor differs")
	}
}

func TestSelection_domainsMultiSelect(t *testing.T) {
	sel := buckets.Selection{Domains: []string{"security", "seo"}}

	secBucket := mustParse(t, "domains/security/no-hardcoded-credentials")
	if !sel.IsBucketActive(secBucket) {
		t.Error("security domain should be active when selected")
	}

	seoBucket := mustParse(t, "domains/seo/canonical-present")
	if !sel.IsBucketActive(seoBucket) {
		t.Error("seo domain should be active when selected")
	}

	htmlBucket := mustParse(t, "domains/html/html-required-meta")
	if sel.IsBucketActive(htmlBucket) {
		t.Error("html domain should NOT be active when not selected")
	}
}

func TestSelection_isFrameActive_respectsPerFrameOverride(t *testing.T) {
	sel := buckets.Selection{
		Domains:        []string{"security"},
		FrameOverrides: map[string]bool{"no-hardcoded-credentials": false}, // explicitly off
	}
	b := mustParse(t, "domains/security/no-hardcoded-credentials")
	if sel.IsBucketActive(b) != true {
		t.Error("bucket should still be active (per-frame override is separate)")
	}
	if sel.IsFrameActive(b) {
		t.Error("frame should be inactive when override sets enabled=false")
	}
}

func TestSelection_isFrameActive_noOverride(t *testing.T) {
	sel := buckets.Selection{Domains: []string{"security"}}
	b := mustParse(t, "domains/security/no-hardcoded-credentials")
	if !sel.IsFrameActive(b) {
		t.Error("frame should be active when bucket is active and no override exists")
	}
}

func TestSelection_isFrameActive_inactiveBucketOverridesAll(t *testing.T) {
	// Per-frame enabled=true cannot resurrect a frame whose bucket is inactive.
	sel := buckets.Selection{
		Domains:        []string{},
		FrameOverrides: map[string]bool{"no-hardcoded-credentials": true},
	}
	b := mustParse(t, "domains/security/no-hardcoded-credentials")
	if sel.IsFrameActive(b) {
		t.Error("frame must NOT be active when its bucket is inactive, regardless of per-frame override")
	}
}
