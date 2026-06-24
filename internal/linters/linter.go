// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package linters drives the project's language-native linters and normalizes
// their findings into engine.CheckResult so they flow through the same audit +
// whitelist + format pipeline as native frames. One config, one suppression
// mechanism, one gate.
//
// It's modular: built-in adapters cover go-vet / eslint / shellcheck; any other
// [linters.<name>] in appframes.toml is a user-defined linter driven entirely
// by config (Command/Args/Patterns/Regex) - bring any CLI linter without code.
package linters

import (
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// Linter drives one external linter and normalizes its findings. Run is
// self-contained: it resolves its tool and returns a SKIP CheckResult if the
// tool isn't runnable (a missing tool never fails the gate).
type Linter interface {
	// ID is the synthetic frame ID findings carry (e.g. "app-correctness/eslint").
	ID() string
	// Run executes the linter and returns one merged CheckResult.
	Run(projectRoot string, cfg config.LinterConfig, excludedDirs []string) engine.CheckResult
}

// builtins maps a linter name to its built-in adapter. Names NOT here are
// treated as user-defined custom linters (config-driven command + regex).
var builtins = map[string]Linter{
	"go-vet":     GoVet{},
	"eslint":     ESLint{},
	"shellcheck": ShellCheck{},
}

// resolveOutcome maps a configured severity to the CheckOutcome a linter's
// findings carry. Default (and unknown) is BLOCK - language linters are
// high-signal; the point is to stop the bug at the gate.
func resolveOutcome(severity string) engine.CheckOutcome {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "warn":
		return engine.OutcomeWarn
	case "info":
		return engine.OutcomeInfo
	default:
		return engine.OutcomeBlock
	}
}

// RunEnabled runs every enabled [linters.<name>] and returns their results plus
// the frame IDs that ran (check.go unions these into the known-frame-ID set so
// whitelist entries for linter findings validate). Built-in name → built-in
// adapter; any other name → a config-driven custom linter. Deterministic order.
func RunEnabled(lc map[string]config.LinterConfig, projectRoot string, excludedDirs []string) (results []engine.CheckResult, ranIDs []string) {
	names := make([]string, 0, len(lc))
	for name := range lc {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cfg := lc[name]
		if !cfg.Enabled {
			continue
		}
		lint, ok := builtins[name]
		if !ok {
			if cfg.Kind == "regex" {
				lint = regexLinter{name: name}
			} else {
				lint = customLinter{name: name}
			}
		}
		results = append(results, lint.Run(projectRoot, cfg, excludedDirs))
		ranIDs = append(ranIDs, lint.ID())
	}
	return results, ranIDs
}

// frameID builds the synthetic frame ID for a linter (e.g. app-correctness/eslint).
func frameID(name string) string { return "app-correctness/" + name }

// EnabledIDs returns the synthetic frame IDs of the enabled linters. The
// whitelist loader fails closed on unknown frame IDs, so every path that loads
// the whitelist must union these in - otherwise a whitelist entry suppressing
// a linter finding (e.g. app-correctness/shellcheck) breaks the load.
func EnabledIDs(lc map[string]config.LinterConfig) []string {
	var ids []string
	for name, cfg := range lc {
		if cfg.Enabled {
			ids = append(ids, frameID(name))
		}
	}
	sort.Strings(ids)
	return ids
}

// LinterInfo describes one enabled linter for read-only inspection surfaces
// (e.g. the dashboard /frames page). It exposes the effective config without
// running anything - linters are external tools, not frames, but their findings
// carry a synthetic frame ID, so they need a home in the catalogue too.
type LinterInfo struct {
	Name     string   // [linters.<name>]
	ID       string   // synthetic frame ID, app-correctness/<name>
	Builtin  bool     // true for go-vet/eslint/shellcheck; false = config-driven custom
	Severity string   // effective gate severity: BLOCK (default) / WARN / INFO
	Dir      string   // project-relative subdir it runs from, if scoped
	Patterns []string // paths/globs it lints (custom + eslint scoping)
	Command  string   // custom linters only: the CLI invoked
	Args     []string // custom linters only
	Disable  []string // rule codes ignored
}

// Describe builds the inspection view of one linter from its config.
func Describe(name string, cfg config.LinterConfig) LinterInfo {
	_, builtin := builtins[name]
	return LinterInfo{
		Name:     name,
		ID:       frameID(name),
		Builtin:  builtin,
		Severity: severityName(cfg.Severity),
		Dir:      cfg.Dir,
		Patterns: cfg.Patterns,
		Command:  cfg.Command,
		Args:     cfg.Args,
		Disable:  cfg.Disable,
	}
}

// DescribeEnabled returns the inspection view of every enabled linter, sorted.
func DescribeEnabled(lc map[string]config.LinterConfig) []LinterInfo {
	names := make([]string, 0, len(lc))
	for name, cfg := range lc {
		if cfg.Enabled {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	out := make([]LinterInfo, 0, len(names))
	for _, name := range names {
		out = append(out, Describe(name, lc[name]))
	}
	return out
}

// ByID resolves a synthetic linter frame ID (app-correctness/<name>) to its
// inspection view, if that linter is enabled. Returns false otherwise - so a
// real app-correctness/* frame is never mistaken for a linter.
func ByID(id string, lc map[string]config.LinterConfig) (LinterInfo, bool) {
	for name, cfg := range lc {
		if cfg.Enabled && frameID(name) == id {
			return Describe(name, cfg), true
		}
	}
	return LinterInfo{}, false
}

// severityName is the display form of a configured severity, matching
// resolveOutcome's mapping (default + unknown → BLOCK).
func severityName(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "warn":
		return "WARN"
	case "info":
		return "INFO"
	default:
		return "BLOCK"
	}
}

// linterRoot is the directory a linter runs from: the project root, or
// projectRoot/cfg.Dir when the linter is scoped to a subproject (e.g. a
// frontend whose tool + config live in studio/). Finding paths are still
// displayed relative to projectRoot.
func linterRoot(projectRoot string, cfg config.LinterConfig) string {
	if cfg.Dir == "" {
		return projectRoot
	}
	return filepath.Join(projectRoot, cfg.Dir)
}

// skipResult is the SKIP a linter returns when its tool isn't runnable - a
// missing tool must never fail the gate.
func skipResult(id, reason string) engine.CheckResult {
	return engine.CheckResult{
		FrameID:   id,
		Category:  frames.CategoryAppCorrectness,
		Outcome:   engine.OutcomeSkip,
		Reason:    reason,
		Timestamp: time.Now().UTC(),
	}
}

// ruleDisabled reports whether a finding's label leads with a disabled rule
// code. A word boundary (end / space / colon / paren) is required so "SC10"
// does not match "SC1091". Case-insensitive.
func ruleDisabled(label string, disable []string) bool {
	for _, d := range disable {
		d = strings.TrimSpace(d)
		if d == "" || len(label) < len(d) {
			continue
		}
		if !strings.EqualFold(label[:len(d)], d) {
			continue
		}
		if len(label) == len(d) {
			return true
		}
		switch label[len(d)] {
		case ' ', ':', '(':
			return true
		}
	}
	return false
}

// dropDisabled removes hits whose rule code is in disable.
func dropDisabled(hits []engine.Hit, disable []string) []engine.Hit {
	if len(disable) == 0 {
		return hits
	}
	kept := make([]engine.Hit, 0, len(hits))
	for _, h := range hits {
		if !ruleDisabled(h.Label, disable) {
			kept = append(kept, h)
		}
	}
	return kept
}

// buildResult merges a linter's hits into one CheckResult, dropping any whose
// rule is in disable (Phase 3). Empty (or all-disabled) → PASS. The Reason
// follows the "<tool>: <hit>; <hit>" convention so the whitelist suppression
// pass can rebuild it from surviving hits. Hits are sorted for determinism.
func buildResult(id, toolLabel string, hits []engine.Hit, findingOutcome engine.CheckOutcome, disable []string) engine.CheckResult {
	hits = dropDisabled(hits, disable)
	res := engine.CheckResult{
		FrameID:   id,
		Category:  frames.CategoryAppCorrectness,
		Timestamp: time.Now().UTC(),
	}
	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		res.Reason = toolLabel + ": no findings"
		return res
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		if hits[i].Line != hits[j].Line {
			return hits[i].Line < hits[j].Line
		}
		return hits[i].Label < hits[j].Label
	})
	parts := make([]string, len(hits))
	for i, h := range hits {
		parts[i] = h.Format()
	}
	res.Outcome = findingOutcome
	res.Hits = hits
	res.Reason = toolLabel + ": " + strings.Join(parts, "; ")
	return res
}
