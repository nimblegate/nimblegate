// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package buckets parses and represents v2 frame bucket paths.
//
// A bucket path identifies a frame's location in the v2 four-axis tree:
//
//	core/<frame>                                       (2 segments)
//	framework/<lang>/<frame>                           (3 segments - flat under lang)
//	framework/<lang>/<concept-prefixed>/<frame>        (4 segments - sub-concept under lang)
//	platform/<vendor>/<concept-prefixed>/<frame>       (4 segments - required form)
//	domains/<concept>/<frame>                          (3 segments)
//
// Path depth is STRICT per spec §6.5: 3-step depth is the convention, 4-step
// only when subdivision is genuinely required (and platform requires it).
// Naming convention prefers `aws-rds-aurora` over `aws-rds/aurora`.
package buckets

import (
	"fmt"
	"strings"
)

// Axis identifies which of the four top-level buckets a frame lives under.
type Axis int

const (
	// AxisCore is the universal floor - frames always applied (opt-out only).
	AxisCore Axis = iota
	// AxisFramework is the language/framework axis (svelte, react, go, html, ...).
	AxisFramework
	// AxisPlatform is the deploy-target axis (cloudflare, aws, vercel, ...).
	AxisPlatform
	// AxisDomain is the conceptual-concern axis (security, network, seo, ...).
	AxisDomain
)

// String returns the axis prefix used in bucket paths.
func (a Axis) String() string {
	switch a {
	case AxisCore:
		return "core"
	case AxisFramework:
		return "framework"
	case AxisPlatform:
		return "platform"
	case AxisDomain:
		return "domains"
	}
	return "unknown"
}

// Bucket is the parsed representation of a frame's filesystem-location path.
// The bucket determines which selection in appframes.toml activates the frame
// (see Selection.IsBucketActive in selection.go).
type Bucket struct {
	Axis      Axis
	Lang      string // framework axis only (e.g., "svelte", "html", "go")
	Vendor    string // platform axis only (e.g., "cloudflare", "aws")
	SubBucket string // concept-prefixed sub-bucket under framework or platform (e.g., "cf-security", "svelte-security")
	Concept   string // domain axis only (e.g., "security", "seo")
	FrameID   string // last path segment (the frame's local identifier within its bucket)
}

// ParsePath parses a bucket path string into a Bucket. Returns error if the
// path violates depth rules or uses an unknown axis prefix.
//
// Depth enforcement:
//   - core: exactly 2 segments (core/<frame>)
//   - framework: 3 or 4 segments (with or without sub-bucket)
//   - platform: exactly 4 segments (sub-bucket REQUIRED per spec §3.2)
//   - domains: exactly 3 segments (no sub-bucket on domain axis)
//
// Paths above 4 segments are rejected regardless of axis (spec §6.5 strict cap).
func ParsePath(path string) (Bucket, error) {
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return Bucket{}, fmt.Errorf("invalid bucket path: too few segments in %q (need at least 2)", path)
	}
	if len(parts) > 4 {
		return Bucket{}, fmt.Errorf("invalid bucket path: depth above 4 segments in %q (spec §6.5 strict cap)", path)
	}
	for _, p := range parts {
		if p == "" {
			return Bucket{}, fmt.Errorf("invalid bucket path: empty segment in %q", path)
		}
	}

	switch parts[0] {
	case "core":
		if len(parts) != 2 {
			return Bucket{}, fmt.Errorf("invalid core path %q: must be exactly 2 segments (core/<frame>)", path)
		}
		return Bucket{Axis: AxisCore, FrameID: parts[1]}, nil

	case "framework":
		switch len(parts) {
		case 3:
			return Bucket{Axis: AxisFramework, Lang: parts[1], FrameID: parts[2]}, nil
		case 4:
			return Bucket{Axis: AxisFramework, Lang: parts[1], SubBucket: parts[2], FrameID: parts[3]}, nil
		default:
			return Bucket{}, fmt.Errorf("invalid framework path %q: must be 3 or 4 segments", path)
		}

	case "platform":
		if len(parts) != 4 {
			return Bucket{}, fmt.Errorf("invalid platform path %q: must be exactly 4 segments (platform/<vendor>/<sub-bucket>/<frame>)", path)
		}
		return Bucket{Axis: AxisPlatform, Vendor: parts[1], SubBucket: parts[2], FrameID: parts[3]}, nil

	case "domains":
		if len(parts) != 3 {
			return Bucket{}, fmt.Errorf("invalid domains path %q: must be exactly 3 segments (domains/<concept>/<frame>)", path)
		}
		return Bucket{Axis: AxisDomain, Concept: parts[1], FrameID: parts[2]}, nil
	}

	return Bucket{}, fmt.Errorf("invalid bucket path %q: unknown axis %q (expected core, framework, platform, or domains)", path, parts[0])
}

// String reconstructs the bucket path from its components. Reverses ParsePath.
func (b Bucket) String() string {
	switch b.Axis {
	case AxisCore:
		return "core/" + b.FrameID
	case AxisFramework:
		if b.SubBucket == "" {
			return "framework/" + b.Lang + "/" + b.FrameID
		}
		return "framework/" + b.Lang + "/" + b.SubBucket + "/" + b.FrameID
	case AxisPlatform:
		return "platform/" + b.Vendor + "/" + b.SubBucket + "/" + b.FrameID
	case AxisDomain:
		return "domains/" + b.Concept + "/" + b.FrameID
	}
	return ""
}
