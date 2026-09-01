// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nimblegate/internal/engine"
)

// disableMarkerRe captures the frame ID from either marker form: <category>/<name>.
// Categories are lowercase; names are NOT necessarily - security/no-innerHTML-user-input
// carries capitals, and a lowercase-only name class truncated it to
// "security/no-inner" and reported the frame's own documentation as broken.
var disableMarkerRe = regexp.MustCompile(`appframes:disable(?:-next-line)?\s+([a-z0-9][a-z0-9-]*/[A-Za-z0-9][A-Za-z0-9-]*)`)

// markerScanMaxBytes caps per-file reads. Markers live in source and config;
// anything larger is generated or binary and not worth the read.
const markerScanMaxBytes = 1 << 20

// UnknownDisableMarkers walks projectRoot and returns one message per
// suppression marker naming a frame ID that is not in known.
//
// A marker that names no real frame suppresses nothing, and the only symptom
// is a finding the user expected to have silenced - so this reports rather
// than fires. known must hold every frame ID that exists, not just the ones
// enabled here: suppressing a frame this repo has switched off is legitimate
// and must stay quiet.
func UnknownDisableMarkers(ctx engine.CheckContext, known map[string]bool) []string {
	projectRoot := ctx.ProjectRoot
	seen := map[string]bool{}
	var out []string
	_ = filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if ShouldSkipPath(ctx, path) && path != projectRoot {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipPath(ctx, path) || isGeneratedForMarkers(path) {
			return nil
		}
		body, ok := ReadFileBounded(path, markerScanMaxBytes)
		if !ok {
			return nil
		}
		content := string(body)
		if !strings.Contains(content, "appframes:disable") {
			return nil
		}
		rel, relErr := filepath.Rel(projectRoot, path)
		if relErr != nil {
			rel = path
		}
		for i, line := range strings.Split(content, "\n") {
			for _, m := range disableMarkerRe.FindAllStringSubmatch(line, -1) {
				id := m[1]
				if known[id] {
					continue
				}
				key := rel + ":" + id
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, fmt.Sprintf("%s:%d - `%s` names no frame; this marker suppresses nothing", rel, i+1, id))
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// isGeneratedForMarkers skips file types that carry frame IDs as DATA rather
// than as suppressions: audit logs and event streams quote a frame ID inside a
// reason string, and a snapshot of the dashboard embeds whole frame documents.
// Running the marker regex over those produces an ID mangled by whatever text
// abuts it - 1249 of them on this repo before this filter existed.
func isGeneratedForMarkers(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".log", ".jsonl", ".html", ".htm", ".map", ".svg", ".lock", ".sum":
		return true
	}
	return false
}
