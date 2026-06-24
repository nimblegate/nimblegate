// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"sort"

	"nimblegate/internal/buckets"
	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/frames"
)

// PackStatus reports how an operator's v2 selection treats a single bucket
// (pack). It's the foundation for the dashboard tree-view's partial-pack
// warning badges per spec §4.5 + §7.5.3, and surfaces in audit logs so trend
// tracking can show whether operators are stripping packs down or filling
// them back in.
type PackStatus struct {
	BucketPath  string   // e.g., "platform/cloudflare/cf-pages"
	Total       int      // total frames in the bucket
	Active      int      // frames actually enabled under the current selection
	ActiveIDs   []string // v1 IDs of active frames (sorted)
	ExcludedIDs []string // v1 IDs of frames excluded by per-frame override (sorted)
	State       PackState
}

// PackState describes the bucket's overall status. See spec §7.5.3 for the
// threshold rule: >50% of pack stripped escalates to WARN-level partial.
type PackState int

const (
	// PackInactive means the bucket isn't selected at all (the framework /
	// platform / domain that contains it isn't in the operator's config).
	PackInactive PackState = iota
	// PackFullyActive means all frames in the bucket are enabled.
	PackFullyActive
	// PackPartial means some frames are stripped via per-frame overrides
	// but the bucket itself is selected. Informational threshold: any
	// frames stripped.
	PackPartial
	// PackPartialWarn means more than 50% of the bucket's frames are
	// stripped - the operator may want to exclude the whole bucket
	// instead. Escalated UI severity per spec §7.5.3.
	PackPartialWarn
)

// String renders the state as a stable label for audit logs / dashboard.
func (s PackState) String() string {
	switch s {
	case PackInactive:
		return "inactive"
	case PackFullyActive:
		return "active"
	case PackPartial:
		return "partial"
	case PackPartialWarn:
		return "partial-warn"
	}
	return "unknown"
}

// ComputePackStatus walks the v2 frame map and returns a per-bucket status
// list reflecting the operator's selection. Only buckets containing at
// least one frame are reported; inactive buckets are reported with
// PackInactive state for UI completeness.
func (m *V2FrameMap) ComputePackStatus(cfg *v2.Config, stdlibFrames []frames.Frame) []PackStatus {
	if cfg == nil || m == nil {
		return nil
	}
	sel := cfg.Selection()

	// Group v1 frame IDs by bucket path. We use the bucket String() form
	// without the frame's last segment as the bucket identity.
	type bucketGroup struct {
		bucket buckets.Bucket
		v1IDs  []string
	}
	groups := make(map[string]*bucketGroup)

	for _, f := range stdlibFrames {
		bucket, ok := m.IDToBucket[f.ID()]
		if !ok {
			continue
		}
		bucketKey := bucketPathOnly(bucket)
		bg, exists := groups[bucketKey]
		if !exists {
			bg = &bucketGroup{bucket: bucket}
			groups[bucketKey] = bg
		}
		bg.v1IDs = append(bg.v1IDs, f.ID())
	}

	var out []PackStatus
	for path, bg := range groups {
		ps := PackStatus{BucketPath: path, Total: len(bg.v1IDs)}
		bucketActive := sel.IsBucketActive(bg.bucket)
		if !bucketActive {
			ps.State = PackInactive
			out = append(out, ps)
			continue
		}
		for _, v1ID := range bg.v1IDs {
			// Per-frame override key uses the frame's v2 short ID (the
			// last segment of the bucket path). For Selection.FrameOverrides
			// we use the v1 FrameID embedded in the bucket entry.
			fb := bg.bucket
			fb.FrameID = m.IDToBucket[v1ID].FrameID
			if sel.IsFrameActive(fb) {
				ps.ActiveIDs = append(ps.ActiveIDs, v1ID)
				ps.Active++
			} else {
				ps.ExcludedIDs = append(ps.ExcludedIDs, v1ID)
			}
		}
		sort.Strings(ps.ActiveIDs)
		sort.Strings(ps.ExcludedIDs)
		switch {
		case ps.Active == ps.Total:
			ps.State = PackFullyActive
		case ps.Active*2 < ps.Total: // >50% excluded
			ps.State = PackPartialWarn
		default:
			ps.State = PackPartial
		}
		out = append(out, ps)
	}

	// Sort output by bucket path for stable output.
	sort.Slice(out, func(i, j int) bool { return out[i].BucketPath < out[j].BucketPath })
	return out
}

// bucketPathOnly returns the bucket's path without the FrameID segment.
// E.g. {Axis: AxisPlatform, Vendor: "cloudflare", SubBucket: "cf-pages",
// FrameID: "headers-baseline"} → "platform/cloudflare/cf-pages".
func bucketPathOnly(b buckets.Bucket) string {
	switch b.Axis {
	case buckets.AxisCore:
		return "core"
	case buckets.AxisFramework:
		if b.SubBucket == "" {
			return "framework/" + b.Lang
		}
		return "framework/" + b.Lang + "/" + b.SubBucket
	case buckets.AxisPlatform:
		return "platform/" + b.Vendor + "/" + b.SubBucket
	case buckets.AxisDomain:
		return "domains/" + b.Concept
	}
	return ""
}
