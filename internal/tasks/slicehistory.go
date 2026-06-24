// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const historyVersion = 1
const sliceHistoryFilename = "slice-history.json"

// CompletedSlice is a closed review slice with the findings it introduced.
// Recorded at `nimblegate slice done`; used by `nimblegate slice summary` to
// surface per-slice finding density + anomalies.
type CompletedSlice struct {
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Total     int       `json:"total"`
	Dangerous int       `json:"dangerous"`
	Advisory  int       `json:"advisory"`
}

// SliceHistory is the append-only log of completed slices (local run-state).
type SliceHistory struct {
	Version int              `json:"version"`
	Slices  []CompletedSlice `json:"slices"`
}

func historyPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".appframes", "_canonical", sliceHistoryFilename)
}

// LoadHistory reads the slice history; missing/corrupt → empty (advisory data).
func LoadHistory(projectRoot string) (*SliceHistory, error) {
	data, err := os.ReadFile(historyPath(projectRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return &SliceHistory{Version: historyVersion}, nil
	}
	if err != nil {
		return &SliceHistory{Version: historyVersion}, fmt.Errorf("slice-history: read: %w", err)
	}
	var h SliceHistory
	if err := json.Unmarshal(data, &h); err != nil {
		return &SliceHistory{Version: historyVersion}, nil
	}
	return &h, nil
}

// Append adds a completed slice (in memory; call Save to persist).
func (h *SliceHistory) Append(cs CompletedSlice) {
	h.Slices = append(h.Slices, cs)
}

// Save atomically writes the history.
func (h *SliceHistory) Save(projectRoot string) error {
	h.Version = historyVersion
	path := historyPath(projectRoot)
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return fmt.Errorf("slice-history: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("slice-history: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("slice-history: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("slice-history: rename: %w", err)
	}
	return nil
}

// Mean returns the average Total findings per completed slice (0 if none).
func (h *SliceHistory) Mean() float64 {
	if len(h.Slices) == 0 {
		return 0
	}
	sum := 0
	for _, s := range h.Slices {
		sum += s.Total
	}
	return float64(sum) / float64(len(h.Slices))
}

// Anomalies returns, per slice (parallel to Slices), whether that slice's
// finding count is anomalously high - a signal to examine it more carefully.
func (h *SliceHistory) Anomalies() []bool {
	mean := h.Mean()
	n := len(h.Slices)
	out := make([]bool, n)
	for i, s := range h.Slices {
		out[i] = anomalous(s.Total, mean, n)
	}
	return out
}

// anomalous flags a slice whose finding count is well above the project's
// average - a mechanical signal to examine it more carefully. Requires at
// least 3 completed slices (otherwise the "average" is noise); then flags a
// slice with ≥ 2× the mean and ≥ 3 findings absolute (avoid small-number
// false alarms like 2-vs-1).
func anomalous(total int, mean float64, n int) bool {
	if n < 3 || mean <= 0 {
		return false
	}
	return float64(total) >= 2*mean && total >= 3
}
