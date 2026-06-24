// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package tasks turns the per-run findings of `nimblegate check` into a
// persistent, shrinking task-list. Each finding gets a stable identity that
// survives across runs, so the user (or a PR/agent workflow consuming the
// JSON) sees what is open, what got fixed (drops off), and how long each has
// been open. This is the "Track" stage of the observe-and-correct loop.
package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/engine"
)

const ledgerVersion = 1
const ledgerFilename = "findings-ledger.json"

// Status is a task's lifecycle state.
type Status string

const (
	StatusOpen     Status = "open"
	StatusDeferred Status = "deferred" // acknowledged "known, will fix" - hidden from the active list, but still tracked (not exempt, not fixed)
	StatusResolved Status = "resolved"
)

// FindingKey identifies a tracked finding across runs: frame + project-relative
// file + label. Line is intentionally excluded so a finding that shifts lines
// stays the same task (robust to edits above it).
type FindingKey struct {
	FrameID string
	File    string
	Label   string
}

// ID is a short, stable hash of the key - used as the ledger map key and as a
// reference an external PR/issue workflow can cite (`nimblegate tasks --json`).
func (k FindingKey) ID() string {
	sum := sha256.Sum256([]byte(k.FrameID + "\x00" + k.File + "\x00" + k.Label))
	return hex.EncodeToString(sum[:])[:12]
}

// Finding is one occurrence present in the current run.
type Finding struct {
	Key      FindingKey
	Severity string // BLOCK / WARN / INFO at this sighting
	Line     int    // advisory last-seen line (not part of identity)
}

// Task is the tracked record for a finding across runs.
type Task struct {
	ID         string     `json:"id"`
	FrameID    string     `json:"frame"`
	File       string     `json:"file"`
	Line       int        `json:"line,omitempty"` // advisory last-seen line; not part of identity
	Label      string     `json:"label"`
	Severity   string     `json:"severity"`
	Status     Status     `json:"status"`
	FirstSeen  time.Time  `json:"first_seen"`
	LastSeen   time.Time  `json:"last_seen"`
	RunsSeen   int        `json:"runs_seen"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`

	// Track v2: defer ("known, will fix") + PR/branch linkage.
	DeferReason string     `json:"defer_reason,omitempty"`
	DeferUntil  *time.Time `json:"defer_until,omitempty"`
	DeferredAt  *time.Time `json:"deferred_at,omitempty"`
	PRRef       string     `json:"pr_ref,omitempty"`
}

// Ledger is the persisted task-list, keyed by FindingKey.ID().
type Ledger struct {
	Version int              `json:"version"`
	Tasks   map[string]*Task `json:"tasks"`
}

// NewLedger returns an empty, versioned ledger.
func NewLedger() *Ledger {
	return &Ledger{Version: ledgerVersion, Tasks: map[string]*Task{}}
}

// Reconcile folds this run's findings into the prior ledger and returns the
// updated ledger plus the tasks that resolved this run (were open, now absent).
// Pure: prev is not mutated.
func Reconcile(prev *Ledger, current []Finding, now time.Time) (*Ledger, []*Task) {
	next := NewLedger()
	for id, t := range prev.Tasks {
		cp := *t
		next.Tasks[id] = &cp
	}

	seen := map[string]bool{}
	for _, f := range current {
		id := f.Key.ID()
		seen[id] = true
		if t, ok := next.Tasks[id]; ok {
			switch t.Status {
			case StatusResolved: // regression: it's back
				t.Status = StatusOpen
				t.ResolvedAt = nil
			case StatusDeferred:
				// Stay deferred while it keeps firing - unless the defer
				// window has passed, in which case resurface to open.
				if t.DeferUntil != nil && !t.DeferUntil.After(now) {
					t.Status = StatusOpen
					t.DeferUntil = nil
					t.DeferredAt = nil
					t.DeferReason = ""
				}
			}
			t.LastSeen = now
			t.RunsSeen++
			t.Severity = f.Severity
			t.Line = f.Line // advisory last-seen line
			continue
		}
		next.Tasks[id] = &Task{
			ID:        id,
			FrameID:   f.Key.FrameID,
			File:      f.Key.File,
			Line:      f.Line,
			Label:     f.Key.Label,
			Severity:  f.Severity,
			Status:    StatusOpen,
			FirstSeen: now,
			LastSeen:  now,
			RunsSeen:  1,
		}
	}

	var resolved []*Task
	for id, t := range next.Tasks {
		// A finding that stopped firing is fixed - whether it was open or
		// deferred. PRRef is preserved ("resolved - fixed in #42").
		if (t.Status == StatusOpen || t.Status == StatusDeferred) && !seen[id] {
			t.Status = StatusResolved
			r := now
			t.ResolvedAt = &r
			t.DeferUntil = nil
			t.DeferredAt = nil
			t.DeferReason = ""
			resolved = append(resolved, t)
		}
	}
	return next, resolved
}

// KeysFromResults extracts the trackable findings from a run's CheckResults.
// Only fired results (BLOCK/WARN/INFO) contribute; PASS/SKIP/ERROR are ignored.
// Hit-bearing results yield one Finding per hit (paths made project-relative);
// a fired result with no hits yields one frame-level Finding.
func KeysFromResults(results []engine.CheckResult, projectRoot string) []Finding {
	var out []Finding
	for _, r := range results {
		sev := severityString(r.Outcome)
		if sev == "" {
			continue
		}
		if len(r.Hits) == 0 {
			out = append(out, Finding{Key: FindingKey{FrameID: r.FrameID, File: "", Label: r.Reason}, Severity: sev})
			continue
		}
		for _, h := range r.Hits {
			out = append(out, Finding{
				Key:      FindingKey{FrameID: r.FrameID, File: relPath(h.File, projectRoot), Label: h.Label},
				Severity: sev,
				Line:     h.Line,
			})
		}
	}
	return out
}

func severityString(o engine.CheckOutcome) string {
	switch o {
	case engine.OutcomeBlock:
		return "BLOCK"
	case engine.OutcomeWarn:
		return "WARN"
	case engine.OutcomeInfo:
		return "INFO"
	}
	return ""
}

func relPath(file, projectRoot string) string {
	if projectRoot == "" || !filepath.IsAbs(file) {
		return file
	}
	if rel, err := filepath.Rel(projectRoot, file); err == nil {
		return rel
	}
	return file
}

// OpenTasks returns the open tasks sorted by severity (BLOCK→WARN→INFO) then
// oldest-first, so the most-dangerous, longest-standing tasks lead the list.
func (l *Ledger) OpenTasks() []*Task {
	return l.filterSorted(StatusOpen)
}

// ResolvedTasks returns resolved tasks, most-recently-resolved first.
func (l *Ledger) ResolvedTasks() []*Task {
	out := l.filterSorted(StatusResolved)
	sort.SliceStable(out, func(i, j int) bool {
		return resolvedTime(out[i]).After(resolvedTime(out[j]))
	})
	return out
}

// DeferredTasks returns deferred tasks, severity → age sorted.
func (l *Ledger) DeferredTasks() []*Task {
	return l.filterSorted(StatusDeferred)
}

// resolveID maps a full ID or unambiguous prefix to a task ID.
func (l *Ledger) resolveID(idOrPrefix string) (string, error) {
	if _, ok := l.Tasks[idOrPrefix]; ok {
		return idOrPrefix, nil
	}
	var matches []string
	for id := range l.Tasks {
		if strings.HasPrefix(id, idOrPrefix) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no task matching %q (see `nimblegate tasks --json` for IDs)", idOrPrefix)
	default:
		return "", fmt.Errorf("ambiguous task id %q (%d matches - use more characters)", idOrPrefix, len(matches))
	}
}

// Find resolves a full ID or unambiguous prefix to its task.
func (l *Ledger) Find(idOrPrefix string) (*Task, error) {
	id, err := l.resolveID(idOrPrefix)
	if err != nil {
		return nil, err
	}
	return l.Tasks[id], nil
}

// Defer marks a task "known, will fix" - hidden from the active list but still
// tracked. Optional until resurfaces it after that time. Resolved tasks cannot
// be deferred (they're already fixed).
func (l *Ledger) Defer(idOrPrefix, reason string, until *time.Time, now time.Time) (*Task, error) {
	id, err := l.resolveID(idOrPrefix)
	if err != nil {
		return nil, err
	}
	t := l.Tasks[id]
	if t.Status == StatusResolved {
		return nil, fmt.Errorf("task %s is already resolved; nothing to defer", id)
	}
	t.Status = StatusDeferred
	t.DeferReason = reason
	t.DeferUntil = until
	d := now
	t.DeferredAt = &d
	return t, nil
}

// Undefer returns a deferred task to the active list.
func (l *Ledger) Undefer(idOrPrefix string) (*Task, error) {
	id, err := l.resolveID(idOrPrefix)
	if err != nil {
		return nil, err
	}
	t := l.Tasks[id]
	if t.Status != StatusDeferred {
		return nil, fmt.Errorf("task %s is not deferred", id)
	}
	t.Status = StatusOpen
	t.DeferReason = ""
	t.DeferUntil = nil
	t.DeferredAt = nil
	return t, nil
}

// Link records the PR / branch / URL where a task is being fixed. Free-text.
func (l *Ledger) Link(idOrPrefix, ref string) (*Task, error) {
	id, err := l.resolveID(idOrPrefix)
	if err != nil {
		return nil, err
	}
	l.Tasks[id].PRRef = ref
	return l.Tasks[id], nil
}

// Unlink clears a task's PR reference.
func (l *Ledger) Unlink(idOrPrefix string) (*Task, error) {
	id, err := l.resolveID(idOrPrefix)
	if err != nil {
		return nil, err
	}
	l.Tasks[id].PRRef = ""
	return l.Tasks[id], nil
}

func (l *Ledger) filterSorted(status Status) []*Task {
	var out []*Task
	for _, t := range l.Tasks {
		if t.Status == status {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := severityRank(out[i].Severity), severityRank(out[j].Severity)
		if si != sj {
			return si > sj
		}
		if !out[i].FirstSeen.Equal(out[j].FirstSeen) {
			return out[i].FirstSeen.Before(out[j].FirstSeen)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func severityRank(s string) int {
	switch s {
	case "BLOCK":
		return 3
	case "WARN":
		return 2
	case "INFO":
		return 1
	}
	return 0
}

func resolvedTime(t *Task) time.Time {
	if t.ResolvedAt != nil {
		return *t.ResolvedAt
	}
	return t.LastSeen
}

func ledgerPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".appframes", "_canonical", ledgerFilename)
}

// Load reads the ledger from the project's _canonical/ directory. A missing
// file returns an empty ledger (first-run). Parse errors / version mismatch
// also return an empty ledger - the task-list is advisory and a corrupt file
// must never break `check`.
func Load(projectRoot string) (*Ledger, error) {
	path := ledgerPath(projectRoot)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewLedger(), nil
	}
	if err != nil {
		return NewLedger(), fmt.Errorf("findings-ledger: read %s: %w", path, err)
	}
	var l Ledger
	if err := json.Unmarshal(data, &l); err != nil {
		return NewLedger(), fmt.Errorf("findings-ledger: parse %s: %w", path, err)
	}
	if l.Version != ledgerVersion || l.Tasks == nil {
		return NewLedger(), nil
	}
	return &l, nil
}

// Save atomically writes the ledger (temp file + rename).
func (l *Ledger) Save(projectRoot string) error {
	l.Version = ledgerVersion
	path := ledgerPath(projectRoot)
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("findings-ledger: marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("findings-ledger: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("findings-ledger: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("findings-ledger: rename: %w", err)
	}
	return nil
}
