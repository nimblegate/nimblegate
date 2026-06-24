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

// rootPageFilename matches the SvelteKit route-root pages whose top-level
// imports run at module-load time. A crash here is an entire-route
// failure with no upstream error boundary.
func topOfPageApplicableRootFile(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "+page.svelte", "+layout.svelte":
		return true
	}
	return false
}

// topOfPageImportRegex extracts the import path from a Svelte/JS/TS
// `import ... from '<path>';` statement. Captures the path in group 1.
var topOfPageImportRegex = regexp.MustCompile(`(?m)^\s*import\s+[^'"]*from\s+['"]([^'"]+)['"]`)

const topOfPageDisableMarker = "appframes:disable app-correctness/top-of-page-import-safety"
const topOfPageDisableLineMarker = "appframes:disable-next-line app-correctness/top-of-page-import-safety"
const topOfPageMaxFileBytes = 1 << 20 // 1 MiB

// TopOfPageImportSafety surfaces an INFO when a SvelteKit `+page.svelte`
// or `+layout.svelte` imports a component that pulls in
// `$env/dynamic/public`. Components imported at the top of a route-root
// page execute at module-load time - if their module body touches env
// before runtime initialization completes, the entire route crashes
// with no upstream error boundary.
//
// The check follows ONE level deep: imports of `+page.svelte` are read,
// each resolved relative path is opened, and if that file has an
// `import { env } from '$env/dynamic/public'` line, the import is
// surfaced. Deeper transitive cases (component A imports B which imports
// C which uses dynamic-public) are out of scope for V1 - false-positive
// risk grows fast and the user can still get the signal from the direct
// imports.
//
// Reference: cf-incidents §2 frame proposal #6.
//
// This is pattern advice, not bug detection. dynamic-env-declared
// already BLOCKs the actual crash; this frame surfaces the pattern as
// an opportunity to refactor (lazy-load, guard at top-of-script, switch
// to $env/static/public).
func TopOfPageImportSafety(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "app-correctness/top-of-page-import-safety",
		Category: frames.CategoryAppCorrectness,
	}

	rootPages := ctx.ChangedFiles
	if len(rootPages) == 0 && ctx.Trigger == engine.TriggerCLI {
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
			if topOfPageApplicableRootFile(path) {
				rootPages = append(rootPages, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, page := range rootPages {
		if !topOfPageApplicableRootFile(page) {
			continue
		}
		if ShouldSkipPath(ctx, page) {
			continue
		}
		info, err := os.Stat(page)
		if err != nil || info.Size() > topOfPageMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(page)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, topOfPageDisableMarker) {
			continue
		}

		// Find each import and resolve relative paths to absolute files.
		// External imports (no leading dot or $) are skipped - we can't
		// open them.
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			m := topOfPageImportRegex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if i > 0 && strings.Contains(lines[i-1], topOfPageDisableLineMarker) {
				continue
			}
			path := m[1]
			resolved := resolveLocalImport(page, path)
			if resolved == "" {
				continue
			}
			if ShouldSkipPath(ctx, resolved) {
				continue
			}
			if !componentUsesDynamicPublic(resolved) {
				continue
			}
			label := fmt.Sprintf("imports %q (uses $env/dynamic/public at module level); module crashes propagate to the whole route", path)
			hits = append(hits, fmt.Sprintf("%s:%d - %s", page, i+1, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: page, Line: i + 1, Label: label})
			if len(hits) >= hitCap {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeInfo
	res.Reason = "+page.svelte / +layout.svelte imports components that use $env/dynamic/public at the module level: " + strings.Join(hits, "; ")
	res.Fix = "either (a) move the env access inside onMount / a function so it runs after runtime init, (b) guard with `typeof env === 'undefined'` at the top of the script, or (c) switch the imported component to `$env/static/public` (see app-correctness/prefer-static-public)."
	return res
}

// resolveLocalImport returns the absolute path of an imported file, or
// "" if the import is external or unresolvable. Supports relative paths
// (`./foo`, `../bar`) and SvelteKit's `$lib/` alias by checking common
// project shapes.
func resolveLocalImport(fromFile, importPath string) string {
	if !strings.HasPrefix(importPath, ".") && !strings.HasPrefix(importPath, "$lib") {
		return ""
	}
	// Try the literal path first, then common SvelteKit extensions.
	base := filepath.Dir(fromFile)
	tryPath := func(rel string) string {
		full := filepath.Clean(filepath.Join(base, rel))
		// $lib aliases to src/lib by SvelteKit convention. Walk up from the
		// page file to find src/lib.
		if strings.HasPrefix(rel, "$lib") {
			// Strip $lib and append to the project's src/lib.
			tail := strings.TrimPrefix(rel, "$lib")
			tail = strings.TrimPrefix(tail, "/")
			// Walk up from base to find a sibling src/ directory.
			dir := base
			for i := 0; i < 8; i++ {
				cand := filepath.Join(dir, "src", "lib", tail)
				if pathExistsAsFile(cand) {
					return cand
				}
				parent := filepath.Dir(dir)
				if parent == dir {
					break
				}
				dir = parent
			}
			return ""
		}
		return full
	}
	candidates := []string{
		tryPath(importPath),
		tryPath(importPath + ".svelte"),
		tryPath(importPath + ".ts"),
		tryPath(importPath + ".js"),
		tryPath(importPath + "/index.svelte"),
		tryPath(importPath + "/index.ts"),
		tryPath(importPath + "/index.js"),
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if pathExistsAsFile(c) {
			return c
		}
	}
	return ""
}

func pathExistsAsFile(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// componentUsesDynamicPublic reads the file at path and reports whether
// it contains an `import ... from '$env/dynamic/public'` statement.
func componentUsesDynamicPublic(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() > topOfPageMaxFileBytes {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return dynamicEnvImportRegex.MatchString(string(data))
}
