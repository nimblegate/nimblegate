// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// dynamicEnvImportRegex matches a SvelteKit `$env/dynamic/public` import
// or destructure. Both forms appear in real code:
//
//	import { env } from '$env/dynamic/public'
//	import { env } from "$env/dynamic/public"
var dynamicEnvImportRegex = regexp.MustCompile(`from\s+['"]\$env/dynamic/public['"]`)

// dynamicEnvAccessRegex matches a `env.PUBLIC_X` reference. PUBLIC_X is
// the SvelteKit convention for public env vars. We only care about
// vars accessed via the `env` binding from `$env/dynamic/public`.
//
// Captures the var name (the PUBLIC_X part) in group 1.
var dynamicEnvAccessRegex = regexp.MustCompile(`\benv\.(PUBLIC_[A-Z0-9_]+)\b`)

// wranglerVarsSectionRegex finds the `[vars]` table in wrangler.toml and
// captures the body up to the next top-level section.
// (?ms): m → ^/$ match line boundaries; s → . matches newlines.
var wranglerVarsSectionRegex = regexp.MustCompile(`(?ms)^\s*\[vars\]\s*\n(.+?)(?:^\s*\[|\z)`)

// wranglerVarKeyRegex extracts variable names from a [vars] section body.
var wranglerVarKeyRegex = regexp.MustCompile(`(?m)^\s*([A-Z_][A-Z0-9_]*)\s*=`)

// envExampleDashboardCommentRegex matches the documented dashboard-env
// convention in .env.example:
//
//	# DASHBOARD-ONLY: PUBLIC_FOO
const envExampleDashboardComment = "DASHBOARD-ONLY:"

// envExampleKeyRegex matches `KEY=` declarations in .env.example.
var envExampleKeyRegex = regexp.MustCompile(`(?m)^\s*([A-Z_][A-Z0-9_]*)\s*=`)

const dynamicEnvDisableMarker = "appframes:disable app-correctness/dynamic-env-declared"
const dynamicEnvDisableLineMarker = "appframes:disable-next-line app-correctness/dynamic-env-declared"
const dynamicEnvMaxFileBytes = 1 << 20 // 1 MiB

func dynamicEnvApplicableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".svelte", ".ts", ".js":
		return true
	}
	return false
}

// DynamicEnvDeclared scans SvelteKit source for `$env/dynamic/public`
// imports + `env.PUBLIC_X` accesses, then verifies each PUBLIC_X is
// declared either in `wrangler.toml` `[vars]` or in `.env.example`
// (with the `# DASHBOARD-ONLY: PUBLIC_X` convention indicating the
// var is set in the CF Pages dashboard).
//
// The footgun: SvelteKit's `$env/dynamic/public` reads env at runtime.
// Local dev works because `.env` has the var. On CF Pages, if the var
// isn't declared in `wrangler.toml` `[vars]` or the dashboard, the
// dynamic-public module returns undefined; compiled access throws
// `TypeError: Cannot read properties of undefined`. When the
// component is imported at the top of `+page.svelte`, the crash
// hits on every initial page load. App dead in prod until rollback.
//
// Reference incident: AGENTS_LEARNING §23, cf-incidents §2 (2026-05-18).
func DynamicEnvDeclared(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "app-correctness/dynamic-env-declared",
		Category: frames.CategoryAppCorrectness,
	}

	// Build the declared-var set ONCE from wrangler.toml + .env.example.
	declared := loadDeclaredPublicVars(ctx.ProjectRoot)

	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if dynamicEnvApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !dynamicEnvApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > dynamicEnvMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, dynamicEnvDisableMarker) {
			continue
		}
		// Cheap pre-check: skip files that don't import dynamic/public.
		if !dynamicEnvImportRegex.MatchString(content) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], dynamicEnvDisableLineMarker) {
				continue
			}
			for _, m := range dynamicEnvAccessRegex.FindAllStringSubmatch(line, -1) {
				varName := m[1]
				if declared[varName] {
					continue
				}
				label := fmt.Sprintf("env.%s used via $env/dynamic/public but not declared in wrangler.toml [vars] or .env.example (DASHBOARD-ONLY:)", varName)
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "$env/dynamic/public references undeclared vars: " + strings.Join(hits, "; ")
	res.Fix = "declare the var in wrangler.toml [vars] (committed to repo) OR in .env.example with a comment line `# DASHBOARD-ONLY: <NAME>` (acknowledging it's set in the CF Pages dashboard). Local `.env` alone is not enough - that's the 'works on my machine' trap."
	return res
}

// loadDeclaredPublicVars merges declared PUBLIC_* var names from
// wrangler.toml and .env.example.
func loadDeclaredPublicVars(projectRoot string) map[string]bool {
	declared := map[string]bool{}

	if data, err := os.ReadFile(filepath.Join(projectRoot, "wrangler.toml")); err == nil {
		for _, name := range extractWranglerVars(string(data)) {
			declared[name] = true
		}
	}

	if data, err := os.ReadFile(filepath.Join(projectRoot, ".env.example")); err == nil {
		for _, name := range extractEnvExampleVars(string(data)) {
			declared[name] = true
		}
	}

	return declared
}

func extractWranglerVars(content string) []string {
	m := wranglerVarsSectionRegex.FindStringSubmatch(content)
	if m == nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(m[1], "\n") {
		if k := wranglerVarKeyRegex.FindStringSubmatch(line); k != nil {
			names = append(names, k[1])
		}
	}
	return names
}

// extractEnvExampleVars returns every var that's declared in .env.example.
// Both styles count:
//
//	PUBLIC_API=https://api.example.com
//	# DASHBOARD-ONLY: PUBLIC_SECRET_TOKEN
//
// The second form is the documented convention for dashboard-set vars
// (the local fallback isn't useful, but declaring "this var exists" is).
func extractEnvExampleVars(content string) []string {
	var names []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			if idx := strings.Index(trimmed, envExampleDashboardComment); idx >= 0 {
				rest := strings.TrimSpace(trimmed[idx+len(envExampleDashboardComment):])
				// rest is the var name (possibly with whitespace).
				if rest != "" {
					names = append(names, strings.Fields(rest)[0])
				}
			}
			continue
		}
		if k := envExampleKeyRegex.FindStringSubmatch(line); k != nil {
			names = append(names, k[1])
		}
	}
	return names
}
