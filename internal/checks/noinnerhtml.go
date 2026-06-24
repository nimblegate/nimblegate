// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// innerHTMLPattern matches `.innerHTML = <something not a string literal>`.
var innerHTMLPattern = regexp.MustCompile(`\.innerHTML\s*=\s*[^"'` + "`" + `;\s]`)

// disableMarker is the per-file or per-line suppression comment.
const disableMarker = "appframes:disable security/no-innerHTML-user-input"

// applicableExtensions is the set of file suffixes that this check examines.
var applicableExtensions = map[string]bool{
	".js":   true,
	".mjs":  true,
	".ts":   true,
	".tsx":  true,
	".jsx":  true,
	".html": true,
}

// NoInnerHTMLUserInput scans either ctx.ChangedFiles or the whole project for
// innerHTML assignments to non-string-literal expressions.
//
// Scan scope:
//   - cli trigger with empty ChangedFiles → project-wide walk (preview everything).
//   - pre-commit / git-wrap with empty ChangedFiles → no scan (PASS, matches
//     what the real hook does when nothing is staged).
//   - any trigger with non-empty ChangedFiles → scan those files only.
//
// Exclusion (node_modules / dist / build / .git / .nimblegate by default,
// configurable via [scan].exclude) applies uniformly to BOTH the project-wide
// walk AND files arriving via ChangedFiles.
func NoInnerHTMLUserInput(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-innerHTML-user-input",
		Category: frames.CategorySecurity,
	}
	excludes := ctx.ExcludedDirs
	if len(excludes) == 0 {
		excludes = DefaultExcludes()
	}

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
			if applicableExtensions[strings.ToLower(filepath.Ext(path))] {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	for _, file := range files {
		if !applicableExtensions[strings.ToLower(filepath.Ext(file))] {
			continue
		}
		// Apply the same exclusion list to files that arrived via ChangedFiles.
		// Without this, staging a vendored file under node_modules/ would
		// produce a false positive.
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if strings.Contains(content, disableMarker) {
			continue
		}
		for i, line := range strings.Split(content, "\n") {
			if innerHTMLPattern.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d", file, i+1))
				hitsStruct = append(hitsStruct, engine.Hit{
					File:  file,
					Line:  i + 1,
					Label: "innerHTML non-literal assignment",
				})
				break
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "innerHTML assignment of non-literal value found: " + strings.Join(hits, ", ")
	res.Fix = "use `textContent` for text; for HTML, sanitize first (DOMPurify, etc.); or add `// appframes:disable security/no-innerHTML-user-input` above the line if intentional"
	res.Hits = hitsStruct
	return res
}
