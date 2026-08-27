// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"nimblegate/internal/gateway"
	"nimblegate/internal/kits"
)

// RepairFrameAllowlists rewrites kit names sitting in a repo's [frames] enabled
// list into that kit's frame IDs, and records the kit under [ui] applied_kits.
//
// "Apply recommended" wrote kit names straight into the ID allowlist. Those
// match no frame, and because a non-empty list replaces the empty-means-every-
// frame default, a repo whose list held ONLY kit names ran no checks at all.
// Warning about it is not enough: such a repo is ungated until someone acts, so
// the daemon repairs it at startup. Entries that are neither a kit nor a known
// frame are left alone and reported - they may be a category/* pattern or a
// deliberate custom ID, and silently deleting policy is its own failure.
func RepairFrameAllowlists(policyRoot string) (repaired []string, err error) {
	ks, err := kits.LoadStdlib()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(policyRoot, "_repos"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		repo := e.Name()
		fp, lerr := gateway.LoadFramePolicy(policyRoot, repo)
		if lerr != nil || len(fp.Enabled) == 0 {
			continue
		}
		var out []string
		var applied []string
		have := map[string]bool{}
		changed := false
		for _, entry := range fp.Enabled {
			k, isKit := ks.Get(entry)
			if !isKit {
				if !have[entry] {
					have[entry] = true
					out = append(out, entry)
				}
				continue
			}
			changed = true
			applied = append(applied, k.Name)
			for _, id := range k.Frames {
				if !have[id] {
					have[id] = true
					out = append(out, id)
				}
			}
		}
		if !changed {
			continue
		}
		fp.Enabled = out
		if serr := fp.Save(policyRoot, repo); serr != nil {
			return repaired, fmt.Errorf("repair %s: %w", repo, serr)
		}
		cfgPath := filepath.Join(policyRoot, repo, "appframes.toml")
		for _, k := range applied {
			if aerr := addAppliedKit(cfgPath, k); aerr != nil {
				return repaired, fmt.Errorf("repair %s applied_kits: %w", repo, aerr)
			}
		}
		repaired = append(repaired, fmt.Sprintf("%s (%v -> %d frames)", repo, applied, len(out)))
	}
	return repaired, nil
}
