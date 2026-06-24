// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
)

// DefaultExcludes is the path-segment list that file-scanning checks skip
// by default. Any file whose path (relative to projectRoot) contains one of
// these names as an entire segment is excluded from the scan.
//
// Adding `.git` here makes the exclusion idempotent for callers that hand
// us the project root directly; the walker-based callers were already
// skipping `.git` via filepath.WalkDir's SkipDir behaviour.
func DefaultExcludes() []string {
	return []string{".git", "node_modules", "dist", "build", ".appframes"}
}

// ShouldSkipPath is the single decision point file-scanning checks consult
// before opening a file or descending a directory. It composes:
//
//   - ctx.IgnorePath (when non-nil) - the full pipeline: [scan].exclude
//     segments + [scan].exclude-paths globs + .appframes-ignore markers.
//     Set by engine.New for production callers.
//   - The legacy IsExcluded(path, ProjectRoot, ctx.ExcludedDirs) - used
//     as a fallback when IgnorePath is nil (tests / hand-built contexts).
//     Empty ExcludedDirs falls back to DefaultExcludes(), preserving the
//     historical defensive behavior of each check function.
//
// Checks should call this instead of IsExcluded directly when they want
// the new mechanisms to apply.
func ShouldSkipPath(ctx engine.CheckContext, path string) bool {
	if ctx.IgnorePath != nil {
		return ctx.IgnorePath(path)
	}
	excludes := ctx.ExcludedDirs
	if len(excludes) == 0 {
		excludes = DefaultExcludes()
	}
	return IsExcluded(path, ctx.ProjectRoot, excludes)
}

// IsExcluded reports whether path is inside any directory in excludes,
// relative to projectRoot. Matching is done on whole path segments:
// `node_modules` matches `node_modules/foo.js` and `pkg/node_modules/bar.js`
// but NOT `node_modules_old/foo.js`.
//
// path may be absolute or relative; projectRoot is used only to normalize
// absolute paths. If projectRoot is empty, path is treated as-is.
//
// A nil or empty excludes slice means "exclude nothing".
func IsExcluded(path string, projectRoot string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}
	// Normalize to forward slashes for stable segment comparison.
	p := filepath.ToSlash(path)
	if projectRoot != "" {
		root := filepath.ToSlash(projectRoot)
		if strings.HasPrefix(p, root+"/") {
			p = strings.TrimPrefix(p, root+"/")
		} else if p == root {
			return false
		}
	}
	// Strip any leading "./" left over from relative inputs.
	p = strings.TrimPrefix(p, "./")

	excludeSet := make(map[string]struct{}, len(excludes))
	for _, e := range excludes {
		excludeSet[e] = struct{}{}
	}
	for _, segment := range strings.Split(p, "/") {
		if _, hit := excludeSet[segment]; hit {
			return true
		}
	}
	return false
}
