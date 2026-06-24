// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"sort"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/frames"
	v2kits "nimblegate/internal/kits/v2"
)

// MatchStatus describes how an operator's current config relates to a
// candidate stdlib kit per spec §7.5.1.
type MatchStatus int

const (
	// MatchNone - none of the kit's frames are active in the operator's config.
	MatchNone MatchStatus = iota
	// MatchPartial - some of the kit's frames are active, some aren't
	// (operator has the kit's overall shape but stripped some frames).
	MatchPartial
	// MatchFully - every frame in the kit's resolved set is active in
	// the operator's config (and operator may also have extras beyond
	// the kit). Spec §7.5.1: exact match OR strict superset both surface
	// as "kit applied" here for operator simplicity.
	MatchFully
)

// String returns a stable label for the match status.
func (m MatchStatus) String() string {
	switch m {
	case MatchNone:
		return "no-match"
	case MatchPartial:
		return "partial"
	case MatchFully:
		return "fully-matched"
	}
	return "unknown"
}

// KitMatch reports the comparison between a single candidate kit and the
// operator's current config. Used by the dashboard /policy page to surface
// "this config matches: <kit_id>" or "partially matches: <kit_id>" without
// requiring the kit to have been explicitly applied (reverse direction
// inference per spec §7.5.1).
type KitMatch struct {
	KitID   string
	Display string
	Semver  string
	Status  MatchStatus
	Total   int      // total frames the kit's selections would enable
	Active  int      // frames from kit's set that are active in operator's config
	Missing []string // v1 frame IDs the kit would enable but config doesn't
}

// InferKitMatches walks the supplied kit set, computes each kit's effective
// frame set under v2 resolution, and returns the match status against the
// operator's actual config. Output is sorted: fully-matched first, then
// partials, then no-match (omitted by default to keep output focused).
//
// Per spec §7.5.1: shows a kit when at least ONE frame from the kit's
// resolved set is active in the operator's config. Fully-matched kits
// surface as "applied" in the UI even when the operator extended beyond
// the kit's defaults (strict-superset case).
func (m *V2FrameMap) InferKitMatches(cfg *v2.Config, stdlibFrames []frames.Frame, kitSet *v2kits.Set) []KitMatch {
	if cfg == nil || m == nil || kitSet == nil {
		return nil
	}
	// Resolve operator's active frame set once.
	opActive := make(map[string]bool)
	for _, fid := range m.EnabledFrameIDs(cfg, stdlibFrames) {
		opActive[fid] = true
	}

	var matches []KitMatch
	for _, kit := range kitSet.All() {
		// Build a synthetic config from the kit's selections and resolve
		// its frame set under v2.
		kitCfg := &v2.Config{
			Core:      v2.CoreSel{Enabled: true},
			Framework: v2.FrameworkSel{Selected: kit.Selections.Framework},
			Platform:  v2.PlatformSel{Selected: kit.Selections.Platform},
			Domains:   v2.DomainsSel{Selected: append([]string{}, kit.Selections.Domains...)},
		}
		kitCfg.Appframes.Schema.Version = 2
		if len(kit.Selections.PlatformExclude) > 0 {
			kitCfg.PlatformOverrides = make(map[string]v2.VendorOverride)
			for vendor, exc := range kit.Selections.PlatformExclude {
				kitCfg.PlatformOverrides[vendor] = v2.VendorOverride{Exclude: append([]string{}, exc...)}
			}
		}
		kitActive := m.EnabledFrameIDs(kitCfg, stdlibFrames)

		var active, missing []string
		for _, fid := range kitActive {
			if opActive[fid] {
				active = append(active, fid)
			} else {
				missing = append(missing, fid)
			}
		}
		sort.Strings(missing)

		status := MatchNone
		switch {
		case len(active) == len(kitActive) && len(kitActive) > 0:
			status = MatchFully
		case len(active) > 0:
			status = MatchPartial
		}

		matches = append(matches, KitMatch{
			KitID:   kit.KitID,
			Display: kit.Display,
			Semver:  kit.Semver,
			Status:  status,
			Total:   len(kitActive),
			Active:  len(active),
			Missing: missing,
		})
	}

	// Sort: MatchFully first, then MatchPartial, then MatchNone. Within a
	// group, sort by Active descending then KitID ascending for stability.
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Status != matches[j].Status {
			return matches[i].Status > matches[j].Status
		}
		if matches[i].Active != matches[j].Active {
			return matches[i].Active > matches[j].Active
		}
		return matches[i].KitID < matches[j].KitID
	})
	return matches
}
