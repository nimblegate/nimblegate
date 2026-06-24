// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package config loads appframes.toml (per-project) and ~/.appframes/config.toml
// (per-user) into typed Go structs.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/BurntSushi/toml"
)

// Project mirrors the [project] section.
type Project struct {
	Name    string `toml:"name"`
	Version string `toml:"version"`
}

// Frames mirrors the [frames] section.
type Frames struct {
	Enabled []string `toml:"enabled"`
}

// Scan mirrors the [scan] section - knobs for file-scanning checks.
type Scan struct {
	// Exclude is a list of directory-name segments that file-scanning checks
	// (no-innerHTML-user-input, cross-branch-id-consistency, etc.) skip.
	// Empty = use built-in defaults (see internal/checks.DefaultExcludes).
	Exclude []string `toml:"exclude"`

	// ExcludePaths is a list of doublestar globs (e.g. "public/downloads/**")
	// evaluated against paths relative to the project root. Where Exclude
	// matches any directory of that name anywhere in the tree, ExcludePaths
	// matches a specific path - useful for "I serve this directory as user
	// content, never scan it" without losing scanning on identically-named
	// directories elsewhere.
	ExcludePaths []string `toml:"exclude-paths"`
}

// TimeEstimates mirrors the [time-estimates] section. It lets a project
// override the built-in per-tier time-prevented defaults used by
// `nimblegate audit analyze` and the `nimblegate status` time-prevented
// teaser. The user's previous-project data is often a better estimate
// than the conservative defaults nimblegate ships with.
//
// Values are hours-per-hit and must be >= 0. Tier-N where the project
// hasn't set a value falls back to the engine's default
// (frames.DefaultTimeCostHoursPreventedByTier[N]).
type TimeEstimates struct {
	Tier1 *float64 `toml:"tier-1"`
	Tier2 *float64 `toml:"tier-2"`
	Tier3 *float64 `toml:"tier-3"`
	Tier4 *float64 `toml:"tier-4"`
	Tier5 *float64 `toml:"tier-5"`
	Tier6 *float64 `toml:"tier-6"`
}

// Lookup returns the project's override for the given tier (1-6) and
// reports whether one was set. Tiers outside the 1-6 range return
// (0, false).
func (te TimeEstimates) Lookup(tier int) (float64, bool) {
	var v *float64
	switch tier {
	case 1:
		v = te.Tier1
	case 2:
		v = te.Tier2
	case 3:
		v = te.Tier3
	case 4:
		v = te.Tier4
	case 5:
		v = te.Tier5
	case 6:
		v = te.Tier6
	default:
		return 0, false
	}
	if v == nil {
		return 0, false
	}
	return *v, true
}

// LinterConfig is one linter's settings ([linters.<name>]). Built-in adapters
// (go-vet, eslint, shellcheck) use Enabled + Severity; any other [linters.<name>]
// is a user-defined linter driven by Command/Args/Patterns/Regex - nimblegate
// runs the command and parses each output line with the named-group Regex
// (groups: file, line, msg). Severity is "block" or "warn" (default block).
type LinterConfig struct {
	Enabled  bool   `toml:"enabled"`
	Severity string `toml:"severity"`

	// Dir runs the linter from this project-relative subdirectory (e.g.
	// "studio" for a frontend whose eslint + node_modules live there).
	// Empty = the project root. Applies to built-in and custom linters.
	Dir string `toml:"dir"`

	// Disable lists rule codes to ignore for this linter (Phase 3): shellcheck
	// "SC1091", eslint "no-unused-vars", go vet "composites", etc. A finding is
	// dropped when its rule (the leading token of its label) matches an entry.
	Disable []string `toml:"disable"`

	// Kind selects the executor: "regex" runs a deterministic content scan
	// (ScanRegexContent - no subprocess); "" or "command" runs the external
	// command (customLinter). Built-in linters ignore Kind.
	Kind string `toml:"kind"`

	// User-defined (custom) linter fields - ignored by built-in adapters.
	Command  string   `toml:"command"`
	Args     []string `toml:"args"`
	Patterns []string `toml:"patterns"`
	Regex    string   `toml:"regex"`
}

// FrameOverride is one row of [frames.<id>] - the severity and/or enabled override.
type FrameOverride struct {
	Severity string `toml:"severity"`
	Enabled  *bool  `toml:"enabled"`
}

// ProjectConfig is the parsed appframes.toml for a project.
type ProjectConfig struct {
	Project        Project                  `toml:"project"`
	Frames         Frames                   `toml:"frames"`
	Scan           Scan                     `toml:"scan"`
	Linters        map[string]LinterConfig  `toml:"linters"`
	TimeEstimates  TimeEstimates            `toml:"time-estimates"`
	Triggers       map[string]bool          `toml:"triggers"`
	Canonical      map[string]string        `toml:"canonical"`
	FrameOverrides map[string]FrameOverride `toml:"-"`
}

// Validate returns the first validation error in the project config
// (currently: time-estimates fields must be >= 0). Called by `nimblegate lint`.
func (c ProjectConfig) Validate() error {
	for i, p := range []*float64{c.TimeEstimates.Tier1, c.TimeEstimates.Tier2, c.TimeEstimates.Tier3, c.TimeEstimates.Tier4, c.TimeEstimates.Tier5, c.TimeEstimates.Tier6} {
		if p != nil && *p < 0 {
			return fmt.Errorf("config: [time-estimates] tier-%d must be >= 0 (got %v)", i+1, *p)
		}
	}
	return nil
}

// LoadProject loads appframes.toml from path. If the file does not exist, returns
// a zero-value config with non-nil maps; this is not an error.
func LoadProject(path string) (ProjectConfig, error) {
	cfg := ProjectConfig{
		Triggers:       map[string]bool{},
		Canonical:      map[string]string{},
		FrameOverrides: map[string]FrameOverride{},
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: read %s: %w", path, err)
	}

	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, fmt.Errorf("config: parse %s: %w", path, err)
	}

	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return cfg, fmt.Errorf("config: re-parse %s for overrides: %w", path, err)
	}
	collectOverrides("", raw["frames"], cfg.FrameOverrides)

	if cfg.Triggers == nil {
		cfg.Triggers = map[string]bool{}
	}
	if cfg.Canonical == nil {
		cfg.Canonical = map[string]string{}
	}
	if cfg.FrameOverrides == nil {
		cfg.FrameOverrides = map[string]FrameOverride{}
	}
	return cfg, nil
}

func collectOverrides(prefix string, node any, out map[string]FrameOverride) {
	m, ok := node.(map[string]any)
	if !ok {
		return
	}
	if hasOverrideKeys(m) {
		ov := FrameOverride{}
		if sv, ok := m["severity"].(string); ok {
			ov.Severity = sv
		}
		if en, ok := m["enabled"].(bool); ok {
			ov.Enabled = &en
		}
		out[prefix] = ov
		return
	}
	for k, v := range m {
		var next string
		if prefix == "" {
			next = k
		} else {
			next = prefix + "/" + k
		}
		if prefix == "" && k == "enabled" {
			continue
		}
		collectOverrides(next, v, out)
	}
}

func hasOverrideKeys(m map[string]any) bool {
	if v, ok := m["severity"]; ok {
		if _, isStr := v.(string); isStr {
			return true
		}
	}
	if v, ok := m["enabled"]; ok {
		if _, isBool := v.(bool); isBool {
			return true
		}
	}
	return false
}
