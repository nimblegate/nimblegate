// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const noInvisibleTagCharsDisableMarker = "appframes:disable security/no-invisible-tag-chars"
const noInvisibleTagCharsDisableLineMarker = "appframes:disable-next-line security/no-invisible-tag-chars"
const noInvisibleTagCharsMaxFileBytes = 1 << 20

// isUnicodeTagRune reports whether r is in the Unicode "Tags" block
// (U+E0000-U+E007F) - invisible payload widely used to smuggle prompt
// injection through otherwise innocuous-looking text.
func isUnicodeTagRune(r rune) bool {
	return r >= 0xE0000 && r <= 0xE007F
}

// NoInvisibleTagChars scans text files for Unicode tag-block characters
// (U+E0000-U+E007F). These render as zero pixels yet carry payload that
// agentic LLM readers will interpret as instructions.
func NoInvisibleTagChars(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-invisible-tag-chars",
		Category: frames.CategorySecurity,
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
			files = append(files, path)
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
		info, err := os.Stat(file)
		if err != nil || info.Size() > noInvisibleTagCharsMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noInvisibleTagCharsDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noInvisibleTagCharsDisableLineMarker) {
				continue
			}
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				if isUnicodeTagRune(r) {
					label := fmt.Sprintf("U+%05X (Unicode tag char)", r)
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
					if len(hits) >= hitCap {
						break filesLoop
					}
					break
				}
				j += size
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "invisible Unicode tag character detected (prompt-injection channel): " + strings.Join(hits, "; ")
	res.Fix = "delete the offending character; tag-block runes have no legitimate use in source/docs. Use appframes:disable per-file only if you genuinely need them and document why"
	res.Hits = hitsStruct
	return res
}
