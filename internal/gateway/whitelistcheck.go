// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"path/filepath"
	"sync"
	"time"

	"nimblegate/internal/stdlib"
	"nimblegate/internal/whitelist"
)

// WhitelistPath is where the gate reads a repo's whitelist: the policy dir's
// copy, overlaid into every scan, never the one a push carries.
func WhitelistPath(policyRoot, repo string) string {
	return filepath.Join(policyRoot, repo, ".appframes", "_canonical", "whitelist.toml")
}

// CheckWhitelist loads a repo's whitelist the way the gate does. present is
// false when the repo has none. A non-nil error means the gate cannot load it
// and therefore rejects every push to the repo before any frame runs.
func CheckWhitelist(policyRoot, repo string) (present bool, entries int, err error) {
	wl, err := whitelist.Load(WhitelistPath(policyRoot, repo), doctorKnownFrameIDs(policyRoot, repo), time.Now().UTC())
	if err != nil {
		return true, 0, err
	}
	if wl == nil {
		return false, 0, nil
	}
	return true, len(wl.Entries()), nil
}

var (
	stdlibIDsOnce sync.Once
	stdlibIDs     map[string]bool
)

// stdlibFrameIDs is the embedded frame catalog's ID set. The catalog is
// compiled into the binary, so it is parsed once, not per repo per page load.
func stdlibFrameIDs() map[string]bool {
	stdlibIDsOnce.Do(func() {
		stdlibIDs = map[string]bool{}
		if all, err := stdlib.Load(); err == nil {
			for _, f := range all {
				stdlibIDs[f.ID()] = true
			}
		}
	})
	return stdlibIDs
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
