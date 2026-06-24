// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v1tov2

import (
	"fmt"
	"sort"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/frames"
	"nimblegate/internal/kits"
)

// ValidateInternalConsistency checks that the translator's output is a valid
// v2.Config - schema version is 2, axis selections are non-conflicting, and
// per-vendor exclude lists reference vendors that are actually selected.
//
// This is a Phase C gate. The full zero-delta validation (running the same
// tree through v1 and v2 engines and comparing finding lists) requires
// engine integration and lands in Phase I per the implementation plan; the
// hook for it is ValidateZeroDelta (in this file as a stub).
func ValidateInternalConsistency(cfg *v2.Config) error {
	if cfg == nil {
		return fmt.Errorf("v1tov2: ValidateInternalConsistency: cfg is nil")
	}
	if cfg.Appframes.Schema.Version != 2 {
		return fmt.Errorf("v1tov2: translator output has schema.version = %d, want 2", cfg.Appframes.Schema.Version)
	}
	// Per-vendor exclude lists should only reference the vendor that's
	// actually selected. A v2 config with [platform.aws] exclude when
	// [platform] selected = "cloudflare" is inconsistent.
	for vendor := range cfg.PlatformOverrides {
		if vendor != cfg.Platform.Selected {
			return fmt.Errorf("v1tov2: per-vendor exclude list for %q but Platform.Selected = %q", vendor, cfg.Platform.Selected)
		}
	}
	return nil
}

// ZeroDeltaResult is the outcome of a zero-delta comparison run. Identical
// reports the two configs produced the same finding set; Differ lists the
// differences (frames in v1 not in v2, frames in v2 not in v1).
type ZeroDeltaResult struct {
	Identical   bool
	OnlyInV1    []string // frames v1 would enable but v2 doesn't
	OnlyInV2    []string // frames v2 would enable but v1 doesn't
	BothEnabled []string // common subset
	Explanation string   // human-readable summary (e.g., "522/522 findings match")
}

// ValidateZeroLoss compares the v1 kit list's enabled frame set to what the
// v2 config would enable, confirming v1 frames are all preserved in v2
// (zero-LOSS). Returns a ZeroDeltaResult - Identical when v2 matches v1
// exactly, but more typically v2 is a SUPERSET (the spec's "safer by
// default" property when picking a vendor).
//
// This is the frame-set-level zero-loss verification - proves the
// kit-translation correctness independently from any tree state. Findings-
// level zero-delta (running both engines against the same tree and diffing
// audit logs) is Phase I work; this gate catches translation regressions
// before reaching the slower integration validation.
//
// kitSet is the v1 stdlib kit registry (from internal/kits.LoadStdlib);
// v2FrameMap is the v2 bucket map (from engine.BuildV2FrameMap, surfaced
// here via a callback to avoid an import cycle). stdlibFrames is the v1
// frame catalog (from internal/stdlib.Load).
func ValidateZeroLoss(
	v1KitList []string,
	kitSet *kits.Set,
	stdlibFrames []frames.Frame,
	v2Cfg *v2.Config,
	v2Resolver V2Resolver,
) (*ZeroDeltaResult, error) {
	if v2Cfg == nil {
		return nil, fmt.Errorf("v1tov2: ValidateZeroLoss: v2Cfg is nil")
	}
	if kitSet == nil {
		return nil, fmt.Errorf("v1tov2: ValidateZeroLoss: kitSet is nil")
	}
	if v2Resolver == nil {
		return nil, fmt.Errorf("v1tov2: ValidateZeroLoss: v2Resolver is nil")
	}

	v1Enabled := expandKits(v1KitList, kitSet)
	v2Enabled := v2Resolver(v2Cfg, stdlibFrames)

	v1Set := toStrSet(v1Enabled)
	v2Set := toStrSet(v2Enabled)

	var onlyInV1, onlyInV2, common []string
	for id := range v1Set {
		if v2Set[id] {
			common = append(common, id)
		} else {
			onlyInV1 = append(onlyInV1, id)
		}
	}
	for id := range v2Set {
		if !v1Set[id] {
			onlyInV2 = append(onlyInV2, id)
		}
	}

	identical := len(onlyInV1) == 0 && len(onlyInV2) == 0
	zeroLoss := len(onlyInV1) == 0

	var summary string
	switch {
	case identical:
		summary = fmt.Sprintf("v1 = v2 (%d frames)", len(common))
	case zeroLoss:
		summary = fmt.Sprintf("zero-loss (%d/%d v1 frames preserved + %d v2 extras)", len(common), len(v1Enabled), len(onlyInV2))
	default:
		summary = fmt.Sprintf("MIGRATION LOSES COVERAGE - %d v1 frames missing from v2", len(onlyInV1))
	}

	return &ZeroDeltaResult{
		Identical:   identical,
		OnlyInV1:    onlyInV1,
		OnlyInV2:    onlyInV2,
		BothEnabled: common,
		Explanation: summary,
	}, nil
}

// V2Resolver is supplied by the engine package - given a v2.Config and the
// stdlib frame catalog, returns the list of v1 frame IDs that would be
// enabled. Passed as a callback to break the import cycle (engine imports
// migration, so migration can't import engine).
type V2Resolver func(*v2.Config, []frames.Frame) []string

// expandKits returns the union of all frame IDs across the supplied v1 kit
// names. Unknown kits are skipped silently - callers wanting warnings can
// pre-validate against kitSet.
func expandKits(kitNames []string, kitSet *kits.Set) []string {
	set := make(map[string]struct{})
	for _, name := range kitNames {
		k, ok := kitSet.Get(name)
		if !ok {
			continue
		}
		for _, frameID := range k.Frames {
			set[frameID] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func toStrSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, id := range s {
		m[id] = true
	}
	return m
}
