// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

// ScanRegexContent walks files under root whose project-relative path or
// basename matches any glob in patterns (filepath.Match semantics; empty
// patterns = all files), skipping any directory segment named in excludedDirs
// (and always .git), and returns a Hit per line matching re. Deterministic,
// no subprocess. Hits are sorted by file then line.
func ScanRegexContent(root string, patterns []string, re *regexp.Regexp, excludedDirs []string) ([]engine.Hit, error) {
	skip := map[string]bool{".git": true}
	for _, d := range excludedDirs {
		skip[d] = true
	}
	var hits []engine.Hit
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil
		}
		if !matchesAnyGlob(rel, patterns) {
			return nil
		}
		f, oerr := os.Open(path)
		if oerr != nil {
			return nil
		}
		defer f.Close() // runs when this closure invocation returns (per file), not at ScanRegexContent's end
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		line := 0
		for sc.Scan() {
			line++
			if loc := re.FindStringIndex(sc.Text()); loc != nil {
				hits = append(hits, engine.Hit{File: rel, Line: line, Label: strings.TrimSpace(sc.Text())})
			}
		}
		if err := sc.Err(); err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		return hits[i].Line < hits[j].Line
	})
	return hits, nil
}

// matchesAnyGlob reports whether rel (or its basename) matches any pattern.
// Empty patterns matches everything.
func matchesAnyGlob(rel string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	base := filepath.Base(rel)
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, rel); ok {
			return true
		}
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// regexLinter is the Linter adapter for kind="regex" entries.
type regexLinter struct{ name string }

func (r regexLinter) ID() string { return frameID(r.name) }

func (r regexLinter) Run(projectRoot string, cfg config.LinterConfig, excludedDirs []string) engine.CheckResult {
	id := r.ID()
	if strings.TrimSpace(cfg.Regex) == "" {
		return skipResult(id, r.name+": skipped (no `regex` configured)")
	}
	re, err := regexp.Compile(cfg.Regex)
	if err != nil {
		return skipResult(id, r.name+": skipped (invalid regex: "+err.Error()+")")
	}
	hits, err := ScanRegexContent(projectRoot, cfg.Patterns, re, excludedDirs)
	if err != nil {
		return skipResult(id, r.name+": skipped (scan error: "+err.Error()+")")
	}
	return buildResult(id, r.name, hits, resolveOutcome(cfg.Severity), cfg.Disable)
}
