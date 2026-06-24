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

const noMixedIndentDisableMarker = "appframes:disable encoding/no-mixed-indent"
const noMixedIndentDisableLineMarker = "appframes:disable-next-line encoding/no-mixed-indent"
const noMixedIndentMaxFileBytes = 1 << 20

var mixedIndentExtensions = map[string]bool{
	".py":  true,
	".js":  true,
	".ts":  true,
	".jsx": true,
	".tsx": true,
	".mjs": true,
	".cjs": true,
	".go":  true,
	".rs":  true,
	".c":   true,
	".cpp": true,
	".cc":  true,
	".cxx": true,
	".h":   true,
	".hpp": true,
	".mk":  true,
}

var mixedIndentBasenames = map[string]bool{
	"Makefile":    true,
	"makefile":    true,
	"GNUmakefile": true,
}

func mixedIndentApplicableFile(path string) bool {
	if mixedIndentBasenames[filepath.Base(path)] {
		return true
	}
	return mixedIndentExtensions[strings.ToLower(filepath.Ext(path))]
}

// NoMixedIndent flags lines whose leading whitespace contains BOTH
// tabs and spaces. Renders identically in editors; binds differently
// to interpreters (Python TabError, make recipe rules, gofmt churn).
func NoMixedIndent(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-mixed-indent",
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
			if mixedIndentApplicableFile(path) {
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
		if !mixedIndentApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noMixedIndentMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noMixedIndentDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noMixedIndentDisableLineMarker) {
				continue
			}
			hasTab, hasSpace := false, false
			for _, ch := range line {
				switch ch {
				case '\t':
					hasTab = true
				case ' ':
					hasSpace = true
				default:
					goto done
				}
			}
		done:
			if !hasTab || !hasSpace {
				continue
			}
			label := "mixed tab + space in leading whitespace"
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
	res.Reason = "mixed tab + space indentation (Python TabError / make recipe / gofmt churn): " + strings.Join(hits, "; ")
	res.Fix = "pick one indent style per project. Run gofmt / black / prettier / expand to normalize. Makefiles: recipe lines must START with a literal tab"
	res.Hits = hitsStruct
	return res
}
