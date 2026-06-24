// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package whitelist implements the project-level allow-list for frame
// findings, per V0.5 spec §6.
//
// A whitelist entry suppresses one or more raw Hits from a frame's result
// based on (frame ID, path glob, optional pattern). Suppression is
// audit-logged separately so the bypass is never silent.
//
// Fail-closed semantics: any error loading or validating whitelist.toml
// is surfaced as a hard error to the user. A broken whitelist leaves
// gates firing normally - never the other way around.
package whitelist

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Entry is the raw shape of one [[entry]] table in whitelist.toml.
// Frame and Reason are required at load time; Path defaults to "**"
// (match any file) when empty; Pattern is an optional substring filter
// applied to the Hit.Label.
type Entry struct {
	Frame   string `toml:"frame"`
	Path    string `toml:"path"`
	Pattern string `toml:"pattern"`
	Reason  string `toml:"reason"`
	Expires string `toml:"expires"` // YYYY-MM-DD; "" means never expires
}

// resolved is the compiled, validated form of an Entry - what Match works
// against. Created during Load; never constructed externally.
type resolved struct {
	raw       Entry
	pathRegex *regexp.Regexp // nil if Entry.Path == "" (matches any path)
	expiresAt time.Time      // zero if Entry.Expires == ""
	matched   int            // hygiene counter; incremented by Match
	expired   bool           // precomputed at load time
}

// Whitelist is a loaded, validated set of entries plus the source path
// for error messages. Methods are safe for concurrent reads but Match
// mutates the hygiene counter under no lock - callers should serialize
// suppression passes (the pipeline already does this).
type Whitelist struct {
	entries []*resolved
	source  string
}

// HygieneReport summarizes whitelist health for surface in `nimblegate lint`.
type HygieneReport struct {
	Active  int      // entries that are not expired
	Expired int      // entries past expires: date, still active
	Unused  []Unused // entries that never matched a hit
}

// Unused is one whitelist entry that didn't match anything during this run.
type Unused struct {
	Frame  string
	Path   string
	Reason string
}

// rootTable is the TOML decoding target for whitelist.toml.
type rootTable struct {
	Entry []Entry `toml:"entry"`
}

const defaultPath = "**"

// LoadFromProject loads .appframes/_canonical/whitelist.toml relative to
// projectRoot. Returns nil, nil if the file does not exist (no
// exemptions). All other failures (malformed TOML, missing reason, bad
// expires format, unknown frame ID) are hard errors.
//
// knownFrameIDs lets the loader catch typos like "command-safetly/curl-pipe-shell"
// before they grant unintended exemptions. Pass nil to skip this check
// (only useful in tests).
//
// today is injected so tests can verify expiry behavior without
// fast-forwarding the system clock; the caller normally passes
// time.Now().UTC().
func LoadFromProject(projectRoot string, knownFrameIDs map[string]bool, today time.Time) (*Whitelist, error) {
	path := projectRoot + "/.appframes/_canonical/whitelist.toml"
	return Load(path, knownFrameIDs, today)
}

// Load reads and validates a whitelist.toml at the given path. Same
// semantics as LoadFromProject; useful for tests with explicit paths.
func Load(path string, knownFrameIDs map[string]bool, today time.Time) (*Whitelist, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("whitelist: read %s: %w", path, err)
	}
	var raw rootTable
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, fmt.Errorf("whitelist: parse %s: %w", path, err)
	}

	w := &Whitelist{source: path}
	for i, e := range raw.Entry {
		r, err := resolveEntry(e, knownFrameIDs, today, i+1)
		if err != nil {
			return nil, fmt.Errorf("whitelist: %s: entry #%d: %w", path, i+1, err)
		}
		w.entries = append(w.entries, r)
	}
	// Pre-sort by specificity descending so Match attribution lands on
	// the most-specific entry (better hygiene signal for "unused").
	sortBySpecificity(w.entries)
	return w, nil
}

// resolveEntry validates one Entry and compiles its path glob + expiry.
func resolveEntry(e Entry, knownFrameIDs map[string]bool, today time.Time, idx int) (*resolved, error) {
	r := &resolved{raw: e}

	// Required fields.
	if strings.TrimSpace(e.Frame) == "" {
		return nil, errors.New("'frame' is required")
	}
	if strings.TrimSpace(e.Reason) == "" {
		return nil, errors.New("'reason' is required (audit-grade: bypasses must say why)")
	}

	// Frame ID validation. Accept: "*", "category/*", or "category/name".
	if err := validateFrameSpec(e.Frame, knownFrameIDs); err != nil {
		return nil, err
	}

	// Path glob. Empty defaults to "**" (match any file under project).
	pathGlob := e.Path
	if pathGlob == "" {
		pathGlob = defaultPath
		r.raw.Path = defaultPath
	}
	re, err := compileGlob(pathGlob)
	if err != nil {
		return nil, fmt.Errorf("invalid path glob %q: %w", pathGlob, err)
	}
	r.pathRegex = re

	// Expiry.
	if exp := strings.TrimSpace(e.Expires); exp != "" {
		t, err := time.Parse("2006-01-02", exp)
		if err != nil {
			return nil, fmt.Errorf("invalid 'expires' format %q (want YYYY-MM-DD)", exp)
		}
		r.expiresAt = t
		r.expired = !today.IsZero() && today.After(t)
	}
	return r, nil
}

// validateFrameSpec accepts:
//   - "*" (matches every frame)
//   - "<category>/*" - must use a known category prefix when knownFrameIDs given
//   - "<category>/<name>" - must exist in knownFrameIDs when provided
//
// When knownFrameIDs is nil, only syntactic validation runs.
func validateFrameSpec(spec string, knownFrameIDs map[string]bool) error {
	if spec == "*" {
		return nil
	}
	slash := strings.IndexByte(spec, '/')
	if slash <= 0 || slash == len(spec)-1 {
		return fmt.Errorf("invalid 'frame' %q (want \"*\", \"category/*\", or \"category/name\")", spec)
	}
	if knownFrameIDs == nil {
		return nil
	}
	if strings.HasSuffix(spec, "/*") {
		category := spec[:slash]
		// Accept the wildcard if ANY registered frame lives under this category.
		prefix := category + "/"
		for id := range knownFrameIDs {
			if strings.HasPrefix(id, prefix) {
				return nil
			}
		}
		return fmt.Errorf("unknown category in 'frame' %q (no registered frames match prefix %q)", spec, prefix)
	}
	if !knownFrameIDs[spec] {
		return fmt.Errorf("unknown frame ID %q (typo? not registered? check `nimblegate list`)", spec)
	}
	return nil
}

// Match reports whether the (frameID, file, label) triple is covered by
// any non-expired whitelist entry. The first matching entry (highest
// specificity due to sort) gets credit in the hygiene counter.
//
// Expired entries do NOT match - they're inactive in their suppression
// role but still surface as "expired" in the hygiene report. This is the
// dated-todo discipline: expiry is the killer feature.
//
// file is the project-relative path (the caller is responsible for
// converting absolute paths). label is the Hit.Label (already redacted).
func (w *Whitelist) Match(frameID, file, label string) bool {
	if w == nil {
		return false
	}
	for _, r := range w.entries {
		if r.expired {
			continue
		}
		if !matchFrameSpec(r.raw.Frame, frameID) {
			continue
		}
		if !r.pathRegex.MatchString(file) {
			continue
		}
		if r.raw.Pattern != "" && !strings.Contains(label, r.raw.Pattern) {
			continue
		}
		r.matched++
		return true
	}
	return false
}

// matchFrameSpec applies the wildcard semantics of validateFrameSpec at
// match time. "*" matches everything; "category/*" matches by prefix;
// exact matches everything.
func matchFrameSpec(spec, frameID string) bool {
	if spec == "*" {
		return true
	}
	if strings.HasSuffix(spec, "/*") {
		return strings.HasPrefix(frameID, spec[:len(spec)-1]) // keep the trailing slash
	}
	return spec == frameID
}

// Hygiene returns a snapshot of whitelist health. Call AFTER all
// suppression passes for the current invocation so matched-counts are
// final.
func (w *Whitelist) Hygiene() HygieneReport {
	if w == nil {
		return HygieneReport{}
	}
	rep := HygieneReport{}
	for _, r := range w.entries {
		if r.expired {
			rep.Expired++
			continue
		}
		rep.Active++
		if r.matched == 0 {
			rep.Unused = append(rep.Unused, Unused{
				Frame:  r.raw.Frame,
				Path:   r.raw.Path,
				Reason: r.raw.Reason,
			})
		}
	}
	return rep
}

// Source returns the path the whitelist was loaded from (for error
// messages and `nimblegate whitelist list`).
func (w *Whitelist) Source() string {
	if w == nil {
		return ""
	}
	return w.source
}

// Count returns the number of resolved entries.
func (w *Whitelist) Count() int {
	if w == nil {
		return 0
	}
	return len(w.entries)
}

// EntryView is one entry plus its current runtime status, for display by
// `nimblegate whitelist list` and any future UI. Status reflects the
// current run: "active", "expired", or "active-unused" (active but
// hasn't matched anything yet this invocation).
type EntryView struct {
	Frame        string
	Path         string
	Pattern      string
	Reason       string
	Expires      string // YYYY-MM-DD or ""
	Expired      bool
	MatchedCount int
}

// Entries returns a snapshot of every loaded entry with status info.
// The order is the same load order (which is most-specific-first after
// internal sort). Callers should treat the slice as read-only.
func (w *Whitelist) Entries() []EntryView {
	if w == nil {
		return nil
	}
	out := make([]EntryView, 0, len(w.entries))
	for _, r := range w.entries {
		out = append(out, EntryView{
			Frame:        r.raw.Frame,
			Path:         r.raw.Path,
			Pattern:      r.raw.Pattern,
			Reason:       r.raw.Reason,
			Expires:      r.raw.Expires,
			Expired:      r.expired,
			MatchedCount: r.matched,
		})
	}
	return out
}

// AddEntry appends e to the whitelist.toml at path (creating the file + parent
// dirs if absent), preserving existing content/comments. Returns added=false
// (no error) when an entry with the same Frame+Path already exists. Requires
// e.Frame and e.Reason non-empty (the gate's honesty contract). No known-frame
// validation here (leaf package) - callers validate the frame ID.
func AddEntry(path string, e Entry) (added bool, err error) {
	if strings.TrimSpace(e.Frame) == "" {
		return false, errors.New("whitelist: frame is required")
	}
	if strings.TrimSpace(e.Reason) == "" {
		return false, errors.New("whitelist: reason is required")
	}
	existing := ""
	if data, rerr := os.ReadFile(path); rerr == nil {
		existing = string(data)
	} else if !errors.Is(rerr, fs.ErrNotExist) {
		return false, rerr
	}
	var raw rootTable
	if existing != "" {
		if _, derr := toml.Decode(existing, &raw); derr != nil {
			return false, fmt.Errorf("whitelist: existing file unparseable: %w", derr)
		}
	}
	for _, ex := range raw.Entry {
		if ex.Frame == e.Frame && ex.Path == e.Path {
			return false, nil // dedup
		}
	}
	var b strings.Builder
	b.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("\n[[entry]]\n")
	fmt.Fprintf(&b, "frame  = %q\n", e.Frame)
	fmt.Fprintf(&b, "path   = %q\n", e.Path)
	fmt.Fprintf(&b, "reason = %q\n", e.Reason)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return false, err
	}
	return true, nil
}

// RemoveEntry strips the [[entry]] with the given Frame+Path from path's
// whitelist.toml, preserving every other entry's full data (Pattern, Reason,
// Expires) and the file's TOML shape. Returns (true, nil) when an entry was
// removed, (false, nil) when no matching entry exists or the file is absent.
// Frame+Path mirrors AddEntry's dedup key - same identity, both directions.
func RemoveEntry(path string, frame, pathSpec string) (removed bool, err error) {
	if strings.TrimSpace(frame) == "" {
		return false, errors.New("whitelist: frame is required")
	}
	data, rerr := os.ReadFile(path)
	if rerr != nil {
		if errors.Is(rerr, fs.ErrNotExist) {
			return false, nil
		}
		return false, rerr
	}
	var raw rootTable
	if _, derr := toml.Decode(string(data), &raw); derr != nil {
		return false, fmt.Errorf("whitelist: existing file unparseable: %w", derr)
	}
	kept := make([]Entry, 0, len(raw.Entry))
	for _, ex := range raw.Entry {
		if ex.Frame == frame && ex.Path == pathSpec {
			removed = true
			continue
		}
		kept = append(kept, ex)
	}
	if !removed {
		return false, nil
	}
	var b strings.Builder
	for i, ent := range kept {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("[[entry]]\n")
		fmt.Fprintf(&b, "frame  = %q\n", ent.Frame)
		fmt.Fprintf(&b, "path   = %q\n", ent.Path)
		if ent.Pattern != "" {
			fmt.Fprintf(&b, "pattern = %q\n", ent.Pattern)
		}
		fmt.Fprintf(&b, "reason = %q\n", ent.Reason)
		if ent.Expires != "" {
			fmt.Fprintf(&b, "expires = %q\n", ent.Expires)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

// sortBySpecificity orders entries from most-specific to least-specific.
// Specificity scoring:
//
//	frame exact ID:        +4    frame "category/*":  +2    frame "*":  0
//	pattern provided:      +1
//
// Path is always provided (defaults to "**"), so it contributes equally;
// path glob length is a tiebreaker (longer = more specific by intuition).
func sortBySpecificity(entries []*resolved) {
	score := func(r *resolved) int {
		s := 0
		switch {
		case r.raw.Frame == "*":
			// 0
		case strings.HasSuffix(r.raw.Frame, "/*"):
			s += 2
		default:
			s += 4
		}
		if r.raw.Pattern != "" {
			s++
		}
		return s
	}
	// Stable sort by descending score, then descending path-length tiebreaker.
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0; j-- {
			a, b := entries[j-1], entries[j]
			sa, sb := score(a), score(b)
			if sa > sb {
				break
			}
			if sa == sb && len(a.raw.Path) >= len(b.raw.Path) {
				break
			}
			entries[j-1], entries[j] = b, a
		}
	}
}
