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

const sliceFilename = "slice-state.json"

// SliceState is the currently-open slice (a declared unit of work). Stored in
// .appframes/_canonical/slice-state.json - local run-state, gitignored.
type SliceState struct {
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
}

// Active reports whether a slice is currently open.
func (s *SliceState) Active() bool { return s != nil && s.Name != "" }

func slicePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".appframes", "_canonical", sliceFilename)
}

// LoadSlice reads the current slice state; a missing file is an inactive slice.
func LoadSlice(projectRoot string) (*SliceState, error) {
	data, err := os.ReadFile(slicePath(projectRoot))
	if errors.Is(err, fs.ErrNotExist) {
		return &SliceState{}, nil
	}
	if err != nil {
		return &SliceState{}, fmt.Errorf("slice-state: read: %w", err)
	}
	var s SliceState
	if err := json.Unmarshal(data, &s); err != nil {
		return &SliceState{}, fmt.Errorf("slice-state: parse: %w", err)
	}
	return &s, nil
}

// SaveSlice atomically writes the slice state.
func (s *SliceState) SaveSlice(projectRoot string) error {
	path := slicePath(projectRoot)
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("slice-state: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("slice-state: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("slice-state: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("slice-state: rename: %w", err)
	}
	return nil
}

// ClearSlice removes the slice marker (no active slice). Idempotent.
func ClearSlice(projectRoot string) error {
	err := os.Remove(slicePath(projectRoot))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("slice-state: clear: %w", err)
	}
	return nil
}

// OpenSince returns the open tasks first seen at or after `since` - i.e. the
// findings introduced during the current slice. Severity → age sorted.
func (l *Ledger) OpenSince(since time.Time) []*Task {
	var out []*Task
	for _, t := range l.OpenTasks() {
		if !t.FirstSeen.Before(since) {
			out = append(out, t)
		}
	}
	return out
}
