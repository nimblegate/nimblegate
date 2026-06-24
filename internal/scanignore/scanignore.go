// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package scanignore decides whether a path should be skipped by file-
// scanning checks. It composes three signals:
//
//  1. Segment-name excludes from `[scan] exclude` (the historical
//     behavior: skip any path containing one of these names as a whole
//     segment - `node_modules` matches `pkg/node_modules/foo`).
//  2. Path-glob excludes from `[scan] exclude-paths` (new in V0.5+):
//     doublestar globs evaluated against the path relative to the
//     project root. Lets a project skip `public/downloads/**` without
//     skipping every directory named "downloads."
//  3. Marker-file ignores: `.appframes-ignore` files anywhere in the
//     tree contribute patterns that scope to their containing dir.
//     Discoverable + local - the directory's owner declares the policy
//     in place, gitignore-style.
//
// All three are pre-scan: the matcher is consulted before the file is
// opened, so an ignored path generates no audit-log activity and no
// per-file cost.
package scanignore

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/glob"
)

// MarkerFilename is the gitignore-style file nimblegate looks for when
// walking the tree. Patterns inside it are scoped to the file's directory.
const MarkerFilename = ".appframes-ignore"

// scopedPattern is one ignore rule attached to a directory (the dir where
// the marker file was found, or the project root for globally-configured
// rules). The match runs against the path relative to dir.
type scopedPattern struct {
	dir     string         // absolute path of the dir the pattern is scoped to
	pattern *regexp.Regexp // anchored regex from glob.Compile
	raw     string         // human-readable glob for error messages
}

// Matcher answers "should this path be skipped?" given the project's
// config + discovered marker files. Constructed once per scan via New.
type Matcher struct {
	projectRoot  string
	excludeNames map[string]struct{} // segment names from [scan] exclude
	excludePaths []scopedPattern     // globs from [scan] exclude-paths
	markerScoped []scopedPattern     // patterns from .appframes-ignore files
	loadWarnings []string            // non-fatal pattern compile failures
}

// New builds a Matcher from the project's config + a one-time tree walk to
// discover marker files. excludeNames are the segment-name list (the
// historical [scan].exclude). excludePathGlobs are doublestar path globs.
//
// Returns a matcher even on partial failure; pattern-compile errors are
// surfaced via LoadWarnings so the caller can decide whether to surface
// them to the user. Catastrophic errors (e.g. project root doesn't exist)
// are returned.
func New(projectRoot string, excludeNames []string, excludePathGlobs []string) (*Matcher, error) {
	m := &Matcher{
		projectRoot:  projectRoot,
		excludeNames: map[string]struct{}{},
	}
	for _, n := range excludeNames {
		m.excludeNames[n] = struct{}{}
	}
	for _, g := range excludePathGlobs {
		re, err := glob.Compile(g)
		if err != nil {
			m.loadWarnings = append(m.loadWarnings, fmt.Sprintf("[scan] exclude-paths: invalid glob %q: %v", g, err))
			continue
		}
		m.excludePaths = append(m.excludePaths, scopedPattern{dir: projectRoot, pattern: re, raw: g})
	}
	if err := m.discoverMarkers(); err != nil {
		return m, err
	}
	return m, nil
}

// discoverMarkers walks the project tree looking for .appframes-ignore
// files. Each is parsed and its patterns scoped to its containing dir.
//
// The walk honors the segment-name excludes already set up - i.e., we
// don't descend into node_modules/ looking for marker files. This means
// you can't put an .appframes-ignore inside an already-excluded dir to
// re-include something; that's intentional, segment excludes are the
// outer ring.
func (m *Matcher) discoverMarkers() error {
	if m.projectRoot == "" {
		return nil
	}
	info, err := os.Stat(m.projectRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("scanignore: stat project root: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("scanignore: project root %q is not a directory", m.projectRoot)
	}
	walkErr := filepath.WalkDir(m.projectRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			// Don't descend into segment-excluded dirs. Also don't descend
			// into hidden dirs by default - saves time on .git/ etc., and
			// matches how the existing IsExcluded behaves for `.git`.
			name := d.Name()
			if _, hit := m.excludeNames[name]; hit {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != MarkerFilename {
			return nil
		}
		dir := filepath.Dir(path)
		patterns, warnings := parseMarkerFile(path)
		for _, raw := range patterns {
			// Marker patterns without `/` match recursively under the marker
			// dir (gitignore convention: `*.zip` skips zips anywhere below).
			// Patterns with `/` are anchored to the marker dir.
			pat := raw
			if !strings.Contains(pat, "/") {
				pat = "**/" + pat
			}
			re, err := glob.Compile(pat)
			if err != nil {
				m.loadWarnings = append(m.loadWarnings, fmt.Sprintf("%s: invalid pattern %q: %v", path, raw, err))
				continue
			}
			m.markerScoped = append(m.markerScoped, scopedPattern{dir: dir, pattern: re, raw: raw})
		}
		m.loadWarnings = append(m.loadWarnings, warnings...)
		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("scanignore: walk: %w", walkErr)
	}
	return nil
}

// Match reports whether absPath should be skipped by file-scanning checks.
// absPath may be absolute or relative; if relative, it is joined with the
// matcher's projectRoot to resolve.
func (m *Matcher) Match(absPath string) bool {
	if m == nil {
		return false
	}
	p := filepath.ToSlash(absPath)
	if !filepath.IsAbs(absPath) && m.projectRoot != "" {
		p = filepath.ToSlash(filepath.Join(m.projectRoot, absPath))
	}

	rootSlash := filepath.ToSlash(m.projectRoot)
	// Segment-name excludes operate against the project-root-relative
	// path so a dir literally named projectRoot doesn't accidentally
	// match.
	var relSegments []string
	if rootSlash != "" && (p == rootSlash || strings.HasPrefix(p, rootSlash+"/")) {
		rel := strings.TrimPrefix(p, rootSlash)
		rel = strings.TrimPrefix(rel, "/")
		if rel != "" {
			relSegments = strings.Split(rel, "/")
		}
	} else {
		relSegments = strings.Split(strings.TrimPrefix(p, "/"), "/")
	}
	for _, seg := range relSegments {
		if _, hit := m.excludeNames[seg]; hit {
			return true
		}
	}

	// Path-glob excludes - projectRoot-relative.
	relForGlobs := strings.Join(relSegments, "/")
	for _, sp := range m.excludePaths {
		if sp.pattern.MatchString(relForGlobs) {
			return true
		}
	}

	// Marker-file patterns - scoped to the marker's dir.
	for _, sp := range m.markerScoped {
		dirSlash := filepath.ToSlash(sp.dir)
		if p != dirSlash && !strings.HasPrefix(p, dirSlash+"/") {
			continue
		}
		rel := strings.TrimPrefix(p, dirSlash)
		rel = strings.TrimPrefix(rel, "/")
		if rel == "" {
			continue
		}
		if sp.pattern.MatchString(rel) {
			return true
		}
	}
	return false
}

// MatchDir is the same predicate as Match, exposed separately so callers
// can call filepath.SkipDir on positive matches and avoid descending an
// entire subtree.
func (m *Matcher) MatchDir(absPath string) bool {
	return m.Match(absPath)
}

// LoadWarnings returns non-fatal issues encountered during discovery
// (e.g. malformed glob patterns). Callers should surface these so users
// see typos in their ignore files.
func (m *Matcher) LoadWarnings() []string {
	if m == nil {
		return nil
	}
	return m.loadWarnings
}

// parseMarkerFile reads a .appframes-ignore file. Lines starting with `#`
// are comments; blank lines are skipped. Each remaining line is one glob
// pattern. Returns the patterns and any non-fatal warnings.
func parseMarkerFile(path string) ([]string, []string) {
	var patterns []string
	var warnings []string
	f, err := os.Open(path)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: cannot read: %v", path, err))
		return nil, warnings
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	if err := sc.Err(); err != nil {
		warnings = append(warnings, fmt.Sprintf("%s: read error: %v", path, err))
	}
	return patterns, warnings
}
