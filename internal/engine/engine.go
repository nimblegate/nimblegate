// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"path/filepath"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/scanignore"
)

// Options bundles inputs to the Engine constructor.
type Options struct {
	ProjectRoot   string
	StdlibFrames  []frames.Frame
	CheckFuncs    map[string]CheckFunc
	ProjectFrames []frames.Frame
}

// Engine owns the registry + audit + config and is the entry point for trigger surfaces.
type Engine struct {
	Registry      *Registry
	Audit         *Audit
	ProjectRoot   string
	ProjectConfig config.ProjectConfig

	// EnabledExpanded is the flat frame ID / wildcard list from
	// cfg.Frames.Enabled. Stored so downstream (nimblegate lint, future UI)
	// can answer "what's actually active" without reloading config.
	EnabledExpanded []string

	// IgnoreMatcher is the unified scan-ignore predicate built from the
	// project's [scan] config + discovered .appframes-ignore markers.
	// Callers populate CheckContext.IgnorePath from this.
	IgnoreMatcher *scanignore.Matcher
}

// New builds an Engine: loads config, filters frames by axis selection
// (v2) OR by the explicit enabled list (v1), binds check funcs, opens
// the audit log.
//
// Schema version is detected via config.ReadAny. v1 configs continue using
// the existing path (cfg.Frames.Enabled drives filtering); v2 configs use
// engine.BuildV2FrameMap + buckets.Selection.IsFrameActive to compute the
// effective enabled list at runtime. Both paths produce the same downstream
// shape - Registry receives the filtered frames either way.
func New(opts Options) (*Engine, error) {
	configPath := paths.ConfigPath(opts.ProjectRoot)

	cfg, err := config.LoadProject(configPath)
	if err != nil {
		return nil, fmt.Errorf("engine.New: load project config: %w", err)
	}

	enabled := cfg.Frames.Enabled

	// Schema-version dispatch: if v2, override enabled list via bucket
	// selection. v1 path is preserved unchanged for backwards-compat.
	readResult, rerr := config.ReadAny(configPath)
	if rerr == nil && readResult.SchemaVersion == 2 && readResult.V2 != nil {
		v2Map, ferr := BuildV2FrameMap()
		if ferr != nil {
			return nil, fmt.Errorf("engine.New: build v2 frame map: %w", ferr)
		}
		enabled = v2Map.EnabledFrameIDs(readResult.V2, opts.StdlibFrames)
	}

	reg := NewRegistry()

	for _, f := range opts.StdlibFrames {
		if !isFrameEnabled(f.ID(), enabled) {
			continue
		}
		check := opts.CheckFuncs[f.ID()]
		if err := reg.Add(applyOverride(f, cfg), check); err != nil {
			return nil, fmt.Errorf("engine.New: register stdlib %s: %w", f.ID(), err)
		}
	}

	for _, f := range opts.ProjectFrames {
		if !isFrameEnabled(f.ID(), enabled) {
			continue
		}
		check := opts.CheckFuncs[f.ID()]
		if err := reg.AddProjectOverride(applyOverride(f, cfg), check); err != nil {
			return nil, fmt.Errorf("engine.New: register project %s: %w", f.ID(), err)
		}
	}

	auditPath := filepath.Join(opts.ProjectRoot, ".appframes", "audit.log")
	a, err := OpenAudit(auditPath)
	if err != nil {
		return nil, fmt.Errorf("engine.New: open audit: %w", err)
	}

	// Build the unified ignore matcher. Failures during marker discovery
	// are non-fatal - surface them via IgnoreMatcher.LoadWarnings().
	excludeNames := cfg.Scan.Exclude
	if len(excludeNames) == 0 {
		excludeNames = defaultExcludedDirs()
	}
	matcher, _ := scanignore.New(opts.ProjectRoot, excludeNames, cfg.Scan.ExcludePaths)

	return &Engine{
		Registry:        reg,
		Audit:           a,
		ProjectRoot:     opts.ProjectRoot,
		ProjectConfig:   cfg,
		EnabledExpanded: enabled,
		IgnoreMatcher:   matcher,
	}, nil
}

// IgnorePathFunc returns the matcher's Match method bound to a function
// type, ready to drop into CheckContext.IgnorePath. Returns nil when the
// engine or matcher is nil, so checks can null-check safely.
func (e *Engine) IgnorePathFunc() func(string) bool {
	if e == nil || e.IgnoreMatcher == nil {
		return nil
	}
	return e.IgnoreMatcher.Match
}

// Close releases resources (audit log).
func (e *Engine) Close() error {
	if e.Audit != nil {
		return e.Audit.Close()
	}
	return nil
}

// KnownFrameIDs returns the set of frame IDs currently registered. Used
// by the whitelist loader to validate `frame:` references - catching
// typos at load time before they grant unintended exemptions.
func (e *Engine) KnownFrameIDs() map[string]bool {
	if e == nil || e.Registry == nil {
		return nil
	}
	out := map[string]bool{}
	for _, rf := range e.Registry.All() {
		out[rf.Frame.ID()] = true
	}
	return out
}

// ExcludedDirs returns the directory-name segments that file-scanning checks
// should skip. Drawn from the project config's [scan].exclude or, if that's
// empty, a built-in default (.git, node_modules, dist, build, .nimblegate).
//
// Callers populate CheckContext.ExcludedDirs with this value when building
// a context to hand to the runner.
func (e *Engine) ExcludedDirs() []string {
	if e == nil {
		return defaultExcludedDirs()
	}
	if len(e.ProjectConfig.Scan.Exclude) > 0 {
		return e.ProjectConfig.Scan.Exclude
	}
	return defaultExcludedDirs()
}

// defaultExcludedDirs mirrors internal/checks.DefaultExcludes(). Duplicated
// here so the engine package doesn't import internal/checks (cycle).
func defaultExcludedDirs() []string {
	return []string{".git", "node_modules", "dist", "build", ".appframes"}
}

// isFrameEnabled returns true if id matches any pattern in enabled. Patterns
// support trailing "/*" to match a whole category.
// Empty enabled list = all stdlib frames enabled.
func isFrameEnabled(id string, enabled []string) bool {
	if len(enabled) == 0 {
		return true
	}
	for _, pat := range enabled {
		if pat == id {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "*")
			if strings.HasPrefix(id, prefix) {
				return true
			}
		}
	}
	return false
}

// applyOverride mutates frame's frontmatter (severity, enabled) per the project's
// [frames.<id>] override section, if any. When a severity override is present it
// also clears SeveritySource so that applySeverity uses the overridden value even
// for frames that declare severity-source: frame. The admin override is explicitly
// stronger than the frame-author's self-managed severity.
func applyOverride(f frames.Frame, cfg config.ProjectConfig) frames.Frame {
	ov, ok := cfg.FrameOverrides[f.ID()]
	if !ok {
		return f
	}
	if ov.Severity != "" {
		f.Frontmatter.Severity = frames.Severity(ov.Severity)
		f.Frontmatter.SeveritySource = "" // admin override wins over severity-source: frame
	}
	return f
}
