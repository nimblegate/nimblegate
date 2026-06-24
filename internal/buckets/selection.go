// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package buckets

// Selection captures the operator's axis selections from appframes.toml v2.
// IsBucketActive resolves which frames execute under this selection.
//
// Semantics (locked decisions from spec §1.5):
//   - core: always active when CoreEnabled is true (opt-out only)
//   - framework: single-select; bucket active when Lang matches Framework
//   - platform: single-select vendor + per-vendor opt-out sub-bucket list;
//     bucket active when Vendor matches Platform AND SubBucket is NOT in
//     PlatformExclude[Vendor]
//   - domains: multi-select; bucket active when Concept is in Domains
//   - per-frame override: FrameOverrides[frame_id] = false strips an
//     individual frame even when its bucket is active; FrameOverrides[id] = true
//     does NOT resurrect a frame whose bucket is inactive (bucket activation
//     wins over per-frame override on the false → true direction)
type Selection struct {
	CoreEnabled     bool
	Framework       string              // single-select; "" = no framework axis selected
	Platform        string              // single-select vendor; "" = no platform axis selected
	PlatformExclude map[string][]string // vendor → list of sub-buckets excluded under that vendor
	Domains         []string            // multi-select concept list
	FrameOverrides  map[string]bool     // frame_id → enabled (per-frame strip mechanism)
}

// IsBucketActive reports whether the bucket's frames execute under this
// selection. Does NOT consider per-frame overrides (use IsFrameActive for that).
func (s Selection) IsBucketActive(b Bucket) bool {
	switch b.Axis {
	case AxisCore:
		return s.CoreEnabled

	case AxisFramework:
		return b.Lang == s.Framework && s.Framework != ""

	case AxisPlatform:
		if b.Vendor != s.Platform || s.Platform == "" {
			return false
		}
		for _, excluded := range s.PlatformExclude[b.Vendor] {
			if excluded == b.SubBucket {
				return false
			}
		}
		return true

	case AxisDomain:
		for _, d := range s.Domains {
			if d == b.Concept {
				return true
			}
		}
		return false
	}
	return false
}

// IsFrameActive checks bucket activation AND per-frame override. An individual
// frame can be stripped from an active bucket via FrameOverrides[frame_id]=false.
// Per-frame override cannot resurrect a frame whose bucket is inactive (false ↦
// false; bucket activation is the precondition).
func (s Selection) IsFrameActive(b Bucket) bool {
	if !s.IsBucketActive(b) {
		return false
	}
	if enabled, ok := s.FrameOverrides[b.FrameID]; ok {
		return enabled
	}
	return true
}
