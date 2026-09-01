// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// disableMarkerRe captures the frame ID from either marker form. The ID shape
// mirrors what frames declare: <category>/<name>, lowercase with hyphens.
var disableMarkerRe = regexp.MustCompile(`appframes:disable(?:-next-line)?\s+([a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*)`)

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
func UnknownDisableMarkers(projectRoot string, known map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	_ = filepath.WalkDir(projectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipMarkerDir(d.Name()) && path != projectRoot {
				return filepath.SkipDir
			}
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

// skipMarkerDir keeps the walk off directories that never hold hand-written
// suppressions, matching what the frames themselves skip.
func skipMarkerDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "dist", "build", "bin", ".next", ".svelte-kit", "target", "__pycache__":
		return true
	}
	return false
}
