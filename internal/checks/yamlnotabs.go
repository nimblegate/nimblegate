// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const yamlNoTabsDisableMarker = "appframes:disable encoding/yaml-no-tabs"
const yamlNoTabsDisableLineMarker = "appframes:disable-next-line encoding/yaml-no-tabs"
const yamlNoTabsMaxFileBytes = 1 << 20

func yamlNoTabsApplicableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

// YAMLNoTabs flags any tab in leading whitespace of a YAML file.
// YAML disallows tab indentation; most parsers fail cryptically.
func YAMLNoTabs(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/yaml-no-tabs",
		Category: frames.CategoryEncoding,
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
			if yamlNoTabsApplicableFile(path) {
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
		if ShouldSkipPath(ctx, file) {
			continue
		}
		if !yamlNoTabsApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > yamlNoTabsMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, yamlNoTabsDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], yamlNoTabsDisableLineMarker) {
				continue
			}
			leadingTab := false
			for _, ch := range line {
				if ch == '\t' {
					leadingTab = true
					break
				}
				if ch != ' ' {
					break
				}
			}
			if !leadingTab {
				continue
			}
			label := "tab in YAML indentation"
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
			if len(hits) >= hitCap {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "tab character in YAML indentation (parser rejects): " + strings.Join(hits, "; ")
	res.Fix = "replace leading tabs with spaces: `expand -t 2 -i <file>`"
	res.Hits = hitsStruct
	return res
}
