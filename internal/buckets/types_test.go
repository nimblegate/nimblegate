// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package buckets_test

import (
	"strings"
	"testing"

	"nimblegate/internal/buckets"
)

func TestParsePath_core(t *testing.T) {
	b, err := buckets.ParsePath("core/git-no-force-push-main")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if b.Axis != buckets.AxisCore {
		t.Errorf("axis = %v, want AxisCore", b.Axis)
	}
	if b.FrameID != "git-no-force-push-main" {
		t.Errorf("FrameID = %q, want git-no-force-push-main", b.FrameID)
	}
	if got := b.String(); got != "core/git-no-force-push-main" {
		t.Errorf("String round-trip = %q", got)
	}
}

func TestParsePath_frameworkFlat(t *testing.T) {
	// framework/<lang>/<frame> - 3 segments
	b, err := buckets.ParsePath("framework/html/html-required-meta")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if b.Axis != buckets.AxisFramework {
		t.Errorf("axis = %v, want AxisFramework", b.Axis)
	}
	if b.Lang != "html" {
		t.Errorf("Lang = %q", b.Lang)
	}
	if b.SubBucket != "" {
		t.Errorf("SubBucket = %q, want empty for flat framework path", b.SubBucket)
	}
	if b.FrameID != "html-required-meta" {
		t.Errorf("FrameID = %q", b.FrameID)
	}
}

func TestParsePath_frameworkSubBucket(t *testing.T) {
	// framework/<lang>/<concept-prefixed>/<frame> - 4 segments
	b, err := buckets.ParsePath("framework/svelte/svelte-security/scoped-style-xss")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if b.Lang != "svelte" {
		t.Errorf("Lang = %q", b.Lang)
	}
	if b.SubBucket != "svelte-security" {
		t.Errorf("SubBucket = %q", b.SubBucket)
	}
	if b.FrameID != "scoped-style-xss" {
		t.Errorf("FrameID = %q", b.FrameID)
	}
}

func TestParsePath_platformVendorSub(t *testing.T) {
	// platform/<vendor>/<concept-prefixed>/<frame> - 4 segments, REQUIRED form
	b, err := buckets.ParsePath("platform/cloudflare/cf-security/api-token-leak")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if b.Axis != buckets.AxisPlatform {
		t.Fatalf("axis = %v, want AxisPlatform", b.Axis)
	}
	if b.Vendor != "cloudflare" {
		t.Errorf("Vendor = %q", b.Vendor)
	}
	if b.SubBucket != "cf-security" {
		t.Errorf("SubBucket = %q", b.SubBucket)
	}
	if b.FrameID != "api-token-leak" {
		t.Errorf("FrameID = %q", b.FrameID)
	}
}

func TestParsePath_domains(t *testing.T) {
	// domains/<concept>/<frame> - 3 segments
	b, err := buckets.ParsePath("domains/security/no-hardcoded-credentials")
	if err != nil {
		t.Fatalf("ParsePath: %v", err)
	}
	if b.Axis != buckets.AxisDomain {
		t.Errorf("axis = %v, want AxisDomain", b.Axis)
	}
	if b.Concept != "security" {
		t.Errorf("Concept = %q", b.Concept)
	}
	if b.FrameID != "no-hardcoded-credentials" {
		t.Errorf("FrameID = %q", b.FrameID)
	}
}

func TestParsePath_rejectsCorePathTooShort(t *testing.T) {
	_, err := buckets.ParsePath("core")
	if err == nil {
		t.Fatal("expected error for 1-segment path")
	}
	if !strings.Contains(err.Error(), "depth") && !strings.Contains(err.Error(), "segments") {
		t.Errorf("error mention depth/segments, got: %v", err)
	}
}

func TestParsePath_rejectsPlatformWithoutSubBucket(t *testing.T) {
	// platform/<vendor>/<frame> - 3 segments - INVALID per spec (platform requires 4)
	_, err := buckets.ParsePath("platform/cloudflare/api-token-leak")
	if err == nil {
		t.Fatal("expected error for platform path without sub-bucket")
	}
}

func TestParsePath_rejectsDepthAbove4(t *testing.T) {
	_, err := buckets.ParsePath("platform/cloudflare/cf-d1/extra/sql-injection")
	if err == nil {
		t.Fatal("expected error for 5-segment path")
	}
}

func TestParsePath_rejectsUnknownAxis(t *testing.T) {
	_, err := buckets.ParsePath("misc/something/frame")
	if err == nil {
		t.Fatal("expected error for unknown axis prefix")
	}
}

func TestString_roundTripsAllShapes(t *testing.T) {
	cases := []string{
		"core/git-no-force-push-main",
		"framework/html/html-required-meta",
		"framework/svelte/svelte-security/scoped-style-xss",
		"platform/cloudflare/cf-security/api-token-leak",
		"platform/aws/aws-lambda/cold-start-secret",
		"domains/security/no-hardcoded-credentials",
		"domains/seo/canonical-present",
	}
	for _, want := range cases {
		b, err := buckets.ParsePath(want)
		if err != nil {
			t.Errorf("ParsePath(%q): %v", want, err)
			continue
		}
		got := b.String()
		if got != want {
			t.Errorf("round-trip: ParsePath(%q).String() = %q", want, got)
		}
	}
}
