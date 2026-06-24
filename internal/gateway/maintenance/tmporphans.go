// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// TmpOrphansResult reports per-sweep counts for /health.
type TmpOrphansResult struct {
	Scanned int       // total afgw-* dirs found
	Removed int       // dirs removed (older than maxAge)
	Err     error     // first error encountered (others logged via events)
	Took    time.Time // when this sweep ran
}

// tmpOrphanMaxAge is the threshold below which `/tmp/afgw-*` dirs are left
// alone. 24h is sensible: nimblegate's worktree materialization completes
// in seconds for typical pushes, so anything older than a day is definitely
// orphaned (crashed receive-pack, killed daemon, etc).
const tmpOrphanMaxAge = 24 * time.Hour

// tmpOrphanPrefix is the name pattern nimblegate's PreviewTree / receive-pack
// hook uses for temp dirs. Matches /tmp/afgw-<random>/.
const tmpOrphanPrefix = "afgw-"

// runTmpOrphanCleanup walks the configured tmp dir for afgw-* entries older
// than tmpOrphanMaxAge and removes them. Returns count + first error if any.
// Safe to call on systems where /tmp doesn't exist (returns zero result).
func runTmpOrphanCleanup(now func() time.Time, tmpDir string) TmpOrphansResult {
	res := TmpOrphansResult{Took: now()}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		if os.IsNotExist(err) {
			return res
		}
		res.Err = err
		return res
	}
	cutoff := now().Add(-tmpOrphanMaxAge)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, tmpOrphanPrefix) {
			continue
		}
		res.Scanned++
		full := filepath.Join(tmpDir, name)
		info, err := e.Info()
		if err != nil {
			if res.Err == nil {
				res.Err = err
			}
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			if res.Err == nil {
				res.Err = err
			}
			continue
		}
		res.Removed++
	}
	return res
}
