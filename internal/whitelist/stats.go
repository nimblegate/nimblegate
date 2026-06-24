// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package whitelist

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// statsFilename is the on-disk JSON file that persists cumulative
// match counts across nimblegate check invocations. Lives under
// .appframes/_canonical/ alongside the whitelist itself.
const statsFilename = "whitelist-stats.json"

// statsVersion is incremented when the on-disk shape changes. Older
// stats files with a different version are treated as zeroed (the
// counters are advisory, not load-bearing - losing them is acceptable).
const statsVersion = 1

// DefaultStaleDays is the threshold beyond which a whitelist entry's
// last-matched age earns the "stale" hint. Projects with longer
// cadences can override via `appframes.toml [whitelist] stale-after-days`.
// Added 2026-05-21 with Phase 1 Slice 9.
const DefaultStaleDays = 90

// EntryStats is the persisted record for one whitelist entry. The
// (Frame, Path, Pattern) triple is the entry's identity - changing
// any of them creates a new stats row.
type EntryStats struct {
	Frame        string    `json:"frame"`
	Path         string    `json:"path"`
	Pattern      string    `json:"pattern,omitempty"`
	MatchedTotal int       `json:"matched_total"`
	FirstMatched time.Time `json:"first_matched,omitempty"`
	LastMatched  time.Time `json:"last_matched,omitempty"`
}

// StatsFile is the top-level JSON shape of whitelist-stats.json.
type StatsFile struct {
	Version int          `json:"version"`
	Entries []EntryStats `json:"entries"`
}

// LoadStats reads whitelist-stats.json from the project's _canonical/
// directory. Returns an empty StatsFile (not nil) when the file
// doesn't exist - first-run case, no prior counts to display.
//
// Parse errors and unknown versions also return an empty file rather
// than failing; the stats are advisory and a corrupted file shouldn't
// break `whitelist list` or any check run. Callers that want to surface
// corruption can check the returned error.
func LoadStats(projectRoot string) (*StatsFile, error) {
	path := statsPath(projectRoot)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &StatsFile{Version: statsVersion}, nil
	}
	if err != nil {
		return &StatsFile{Version: statsVersion}, fmt.Errorf("whitelist-stats: read %s: %w", path, err)
	}
	var sf StatsFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return &StatsFile{Version: statsVersion}, fmt.Errorf("whitelist-stats: parse %s: %w", path, err)
	}
	if sf.Version != statsVersion {
		// Forward-incompatible - reset rather than misinterpret.
		return &StatsFile{Version: statsVersion}, nil
	}
	return &sf, nil
}

// Save atomically writes the StatsFile to whitelist-stats.json via
// temp-file-plus-rename to avoid corruption from concurrent writers
// (single-machine case; multi-machine multi-writer to the same path is
// out of scope). Failures are returned but typically ignored at the
// caller since stats are advisory.
func (sf *StatsFile) Save(projectRoot string) error {
	sf.Version = statsVersion
	path := statsPath(projectRoot)
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("whitelist-stats: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("whitelist-stats: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("whitelist-stats: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("whitelist-stats: rename: %w", err)
	}
	return nil
}

// Merge updates the StatsFile in-place from the current run's matches.
// Each entry in w with a non-zero MatchedCount gets its MatchedTotal
// incremented + LastMatched bumped. New entries (not yet recorded) get
// FirstMatched + LastMatched set to now.
//
// Entries that didn't match this run are left untouched - their
// historical counters persist. That's the point: a one-off run with a
// single match shouldn't erase ten prior runs of evidence.
func (sf *StatsFile) Merge(w *Whitelist, now time.Time) {
	if w == nil {
		return
	}
	index := map[string]*EntryStats{}
	for i := range sf.Entries {
		e := &sf.Entries[i]
		index[entryKey(e.Frame, e.Path, e.Pattern)] = e
	}
	for _, ev := range w.Entries() {
		if ev.MatchedCount == 0 {
			continue
		}
		key := entryKey(ev.Frame, ev.Path, ev.Pattern)
		es, ok := index[key]
		if !ok {
			sf.Entries = append(sf.Entries, EntryStats{
				Frame:        ev.Frame,
				Path:         ev.Path,
				Pattern:      ev.Pattern,
				MatchedTotal: ev.MatchedCount,
				FirstMatched: now,
				LastMatched:  now,
			})
			continue
		}
		es.MatchedTotal += ev.MatchedCount
		es.LastMatched = now
		if es.FirstMatched.IsZero() {
			es.FirstMatched = now
		}
	}
}

// Lookup returns the persisted stats for the given entry triple, or
// nil if no prior matches have been recorded.
func (sf *StatsFile) Lookup(frame, path, pattern string) *EntryStats {
	if sf == nil {
		return nil
	}
	key := entryKey(frame, path, pattern)
	for i := range sf.Entries {
		if entryKey(sf.Entries[i].Frame, sf.Entries[i].Path, sf.Entries[i].Pattern) == key {
			return &sf.Entries[i]
		}
	}
	return nil
}

// IsStale reports whether this entry's last match is older than the
// given threshold in days. Returns false if the entry has never been
// matched (no LastMatched timestamp) - "unused" is a separate signal.
func (es EntryStats) IsStale(now time.Time, staleDays int) bool {
	if staleDays <= 0 {
		staleDays = DefaultStaleDays
	}
	if es.LastMatched.IsZero() {
		return false
	}
	threshold := now.AddDate(0, 0, -staleDays)
	return es.LastMatched.Before(threshold)
}

// entryKey produces a stable identity string for a whitelist entry.
// The triple (frame, path, pattern) uniquely identifies an entry for
// stats purposes - expiry changes don't reset counters because the
// underlying suppression target hasn't moved.
func entryKey(frame, path, pattern string) string {
	return frame + "|" + path + "|" + pattern
}

func statsPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".appframes", "_canonical", statsFilename)
}
