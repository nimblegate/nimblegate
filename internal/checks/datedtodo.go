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

// datedTodoMarker matches the leading marker word: TODO, FIXME, HACK, XXX
// on a word boundary.
var datedTodoMarker = regexp.MustCompile(`\b(TODO|FIXME|HACK|XXX)\b`)

// datedTodoAccept matches one of the three acceptable annotations on the
// same line:
//   - ISO date: YYYY-MM-DD (optionally followed by ": <condition>")
//   - owner: @handle or (handle) bare-word
//   - issue: #1234 or PROJ-1234 (2..6 uppercase letters, dash, digits)
//
// One of these within the same line of the marker is enough to accept it.
var datedTodoAccept = regexp.MustCompile(
	`\b\d{4}-\d{2}-\d{2}\b` + // ISO date
		`|@[a-zA-Z0-9_-]+` + // @owner
		`|\(\s*[a-zA-Z][a-zA-Z0-9_-]*\s*[):]` + // (owner) or (owner:
		`|#\d+` + // #issue
		`|[A-Z]{2,6}-\d+`, // PROJ-1234
)

// datedTodoExtensions are the file types the check examines. Source code +
// markdown docs. Keep narrow to avoid false positives in shell prompts /
// config files where "TODO" appears as text.
var datedTodoExtensions = map[string]bool{
	".go":   true,
	".js":   true,
	".mjs":  true,
	".ts":   true,
	".tsx":  true,
	".jsx":  true,
	".py":   true,
	".rs":   true,
	".rb":   true,
	".java": true,
	".sh":   true,
	".md":   true,
}

const datedTodoDisableMarker = "appframes:disable documentation/dated-todo"
const datedTodoDisableLineMarker = "appframes:disable-next-line documentation/dated-todo"

// DatedTodo scans source + markdown files for TODO/FIXME/HACK/XXX markers
// that lack a date, owner, or issue tag. Tagged markers are accepted on
// the trust that "tagged" implies "retire-able".
//
// Scan scope follows the same convention as the other file-scanning checks:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those files only
//   - pre-commit + empty → no scan (PASS), matching the real hook
//
// Exclusion (node_modules / dist / build / etc.) applies uniformly via
// IsExcluded.
func DatedTodo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "documentation/dated-todo",
		Category: frames.CategoryDocumentation,
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
			if datedTodoExtensions[strings.ToLower(filepath.Ext(path))] {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	for _, file := range files {
		if !datedTodoExtensions[strings.ToLower(filepath.Ext(file))] {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if strings.Contains(content, datedTodoDisableMarker) {
			continue // file-level opt-out
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			match := datedTodoMarker.FindString(line)
			if match == "" {
				continue
			}
			if datedTodoAccept.MatchString(line) {
				continue // tagged with date/owner/issue
			}
			// Per-line opt-out: the line above contains the disable marker.
			if i > 0 && strings.Contains(lines[i-1], datedTodoDisableLineMarker) {
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d", file, i+1))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: "untagged " + match})
			if len(hits) >= 20 {
				// Cap to avoid runaway output on legacy codebases.
				break
			}
		}
		if len(hits) >= 20 {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeWarn
	res.Reason = "untagged TODO/FIXME/HACK/XXX markers: " + strings.Join(hits, ", ")
	res.Fix = "add a date `TODO(2026-06-15)`, owner `TODO(@user)`, or issue `TODO(#42)`; or add `// appframes:disable-next-line documentation/dated-todo` above the line if intentional"
	res.Hits = hitsStruct
	return res
}
