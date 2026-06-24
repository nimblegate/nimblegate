// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package state owns nimblegate's runtime pause/resume mechanism.
//
// Two scopes share one in-memory representation:
//   - Global: serialized to ~/.appframes/state.json, affects every onboarded
//     project on this machine.
//   - Project: serialized as a marker file at .appframes/paused inside a
//     project root, affects only that project.
//
// Both files are absent in the unpaused state - IsPaused returns the zero
// Status with no error. Corrupt or unreadable state fails closed: the
// reader returns Status{} and an error; callers treat that as "not paused"
// (enforcement stays on) and surface the error to the user where it can be
// noticed (status banner, doctor).
//
// The shim and gate paths (intercept.go, check.go) call IsPaused once at
// entry; when AnyPaused() is true they fall through to the underlying
// command without running frames or writing audit entries. Pause windows
// therefore appear as gaps in the audit log, which is the intended
// behavior - "nimblegate was not enforcing" is the literal truth.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	stateDirName     = ".appframes"
	stateFileName    = "state.json"
	projectMarker    = "paused" // lives under <projectRoot>/.appframes/paused
	currentSchemaVer = 1
)

// Status captures both scopes' pause state for one (projectRoot, machine) pair.
// Zero value = nothing paused.
type Status struct {
	GlobalPaused    bool
	GlobalPausedAt  time.Time
	GlobalReason    string
	ProjectPaused   bool
	ProjectPausedAt time.Time
	ProjectReason   string
}

// AnyPaused reports whether either scope is paused. The shim / hook fast
// path branches on this.
func (s Status) AnyPaused() bool {
	return s.GlobalPaused || s.ProjectPaused
}

// Store owns the on-disk state. Home is the directory that holds the
// .appframes/ global state subdir (== os.UserHomeDir() in production).
type Store struct {
	home string
}

// NewStore returns a Store rooted at the user's home directory.
func NewStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home: %w", err)
	}
	return &Store{home: home}, nil
}

// NewStoreAt returns a Store rooted at home - for tests, allowing a temp dir
// to stand in for the real home.
func NewStoreAt(home string) *Store {
	return &Store{home: home}
}

// GlobalStateFile returns the path to ~/.appframes/state.json.
func (s *Store) GlobalStateFile() string {
	return filepath.Join(s.home, stateDirName, stateFileName)
}

// ProjectMarkerFile returns the path to <projectRoot>/.appframes/paused.
// Returns empty string if projectRoot is empty (caller is outside a project).
func ProjectMarkerFile(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	return filepath.Join(projectRoot, stateDirName, projectMarker)
}

// IsPaused reads both global and project scope and returns a combined Status.
// projectRoot may be empty (skips the project-scope read).
//
// Errors from reading malformed state are returned to the caller, but the
// returned Status reflects what was successfully read. Treat a non-nil error
// as "show this to the user" - do not interpret it as "paused".
func (s *Store) IsPaused(projectRoot string) (Status, error) {
	var st Status
	var errs []error

	g, err := s.readGlobal()
	if err != nil {
		errs = append(errs, fmt.Errorf("global state: %w", err))
	} else {
		st.GlobalPaused = g.Paused
		st.GlobalPausedAt = g.PausedAt
		st.GlobalReason = g.Reason
	}

	if projectRoot != "" {
		p, err := readProjectMarker(projectRoot)
		if err != nil {
			errs = append(errs, fmt.Errorf("project marker: %w", err))
		} else if p != nil {
			st.ProjectPaused = true
			st.ProjectPausedAt = p.PausedAt
			st.ProjectReason = p.Reason
		}
	}

	if len(errs) > 0 {
		return st, errors.Join(errs...)
	}
	return st, nil
}

// PauseGlobal writes the global state.json with paused=true. Reason may be
// empty. Existing state is overwritten (idempotent: re-pausing updates
// PausedAt to now).
func (s *Store) PauseGlobal(reason string, now time.Time) error {
	g := globalState{
		Version:  currentSchemaVer,
		Paused:   true,
		PausedAt: now.UTC(),
		Reason:   reason,
	}
	return s.writeGlobal(g)
}

// ResumeGlobal clears the global paused state. If state.json doesn't exist,
// this is a no-op (returns nil - resume of an already-unpaused state is fine).
func (s *Store) ResumeGlobal() error {
	path := s.GlobalStateFile()
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// PauseProject writes the .appframes/paused marker inside projectRoot.
// Idempotent - re-pausing updates the timestamp.
func (s *Store) PauseProject(projectRoot, reason string, now time.Time) error {
	if projectRoot == "" {
		return errors.New("project root required")
	}
	dir := filepath.Join(projectRoot, stateDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, projectMarker)
	content := fmt.Sprintf("paused-at: %s\nreason: %s\n",
		now.UTC().Format(time.RFC3339), reason)
	return writeFileAtomic(path, []byte(content), 0o644)
}

// ResumeProject removes the .appframes/paused marker in projectRoot.
// If the marker doesn't exist, returns nil (idempotent).
func (s *Store) ResumeProject(projectRoot string) error {
	if projectRoot == "" {
		return errors.New("project root required")
	}
	path := filepath.Join(projectRoot, stateDirName, projectMarker)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

// globalState is the wire format for ~/.appframes/state.json.
type globalState struct {
	Version  int       `json:"version"`
	Paused   bool      `json:"paused"`
	PausedAt time.Time `json:"paused_at,omitempty"`
	Reason   string    `json:"reason,omitempty"`
}

// readGlobal returns the global state, or zero-value if state.json doesn't
// exist (the unpaused-default case). Returns an error if state.json exists
// but can't be parsed - caller decides how to surface it.
func (s *Store) readGlobal() (globalState, error) {
	path := s.GlobalStateFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return globalState{}, nil
		}
		return globalState{}, fmt.Errorf("read %s: %w", path, err)
	}
	var g globalState
	if err := json.Unmarshal(data, &g); err != nil {
		return globalState{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return g, nil
}

// writeGlobal serializes g to state.json atomically (temp + rename).
func (s *Store) writeGlobal(g globalState) error {
	path := s.GlobalStateFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return writeFileAtomic(path, data, 0o644)
}

// projectState is the parsed form of <projectRoot>/.appframes/paused.
type projectState struct {
	PausedAt time.Time
	Reason   string
}

// readProjectMarker returns (state, nil) if the marker exists, (nil, nil)
// if absent, (nil, err) if present-but-unreadable.
func readProjectMarker(projectRoot string) (*projectState, error) {
	path := filepath.Join(projectRoot, stateDirName, projectMarker)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	p := &projectState{}
	for _, line := range splitLines(string(data)) {
		k, v := splitKV(line)
		switch k {
		case "paused-at":
			if v != "" {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					p.PausedAt = t
				}
			}
		case "reason":
			p.Reason = v
		}
	}
	return p, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func splitKV(line string) (key, value string) {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			key = line[:i]
			value = line[i+1:]
			// trim leading spaces from value
			for len(value) > 0 && (value[0] == ' ' || value[0] == '\t') {
				value = value[1:]
			}
			return
		}
	}
	return line, ""
}

// writeFileAtomic writes data to path via a temp file in the same directory,
// then renames. The rename is atomic on POSIX filesystems, so partial writes
// can't leave state.json corrupted.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("create temp in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write temp %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
