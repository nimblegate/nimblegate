// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"

	"nimblegate/internal/stdlib"
)

// UnresolvableFrameEntries returns the entries in a [frames] enabled list that
// name no stdlib frame. The list is an allowlist matched by exact ID or a
// trailing category/* pattern, so anything else - a kit name, a frame renamed
// or removed in an upgrade - matches nothing and quietly narrows what runs.
// A list made up entirely of such entries disables gating, because a non-empty
// list replaces the empty-means-everything default.
func UnresolvableFrameEntries(enabled []string) []string {
	if len(enabled) == 0 {
		return nil
	}
	// stdlib directly, not roi.StdlibFrameByID: roi imports this package.
	known := map[string]bool{}
	sf, _ := stdlib.Load()
	for _, f := range sf {
		known[f.ID()] = true
	}
	var dead []string
	for _, e := range enabled {
		if strings.HasSuffix(e, "/*") {
			prefix := strings.TrimSuffix(e, "*")
			matched := false
			for id := range known {
				if strings.HasPrefix(id, prefix) {
					matched = true
					break
				}
			}
			if !matched {
				dead = append(dead, e)
			}
			continue
		}
		if !known[e] {
			dead = append(dead, e)
		}
	}
	return dead
}
