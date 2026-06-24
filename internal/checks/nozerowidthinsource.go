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

const noZeroWidthInSourceDisableMarker = "appframes:disable security/no-zero-width-in-source"
const noZeroWidthInSourceDisableLineMarker = "appframes:disable-next-line security/no-zero-width-in-source"
const noZeroWidthInSourceMaxFileBytes = 1 << 20

// sourceCodeExtensions is the set of file extensions considered "source"
// for invisible-identifier scanning. Prose / docs / configs are NOT
// included - encoding/no-zero-width-in-content handles documentation.
var sourceCodeExtensions = map[string]bool{
	".py":    true,
	".js":    true,
	".ts":    true,
	".jsx":   true,
	".tsx":   true,
	".mjs":   true,
	".cjs":   true,
	".go":    true,
	".rs":    true,
	".c":     true,
	".cpp":   true,
	".cc":    true,
	".cxx":   true,
	".h":     true,
	".hpp":   true,
	".java":  true,
	".kt":    true,
	".swift": true,
	".rb":    true,
	".php":   true,
	".sh":    true,
	".bash":  true,
	".zsh":   true,
}

func noZeroWidthApplicableFile(path string) bool {
	return sourceCodeExtensions[strings.ToLower(filepath.Ext(path))]
}

var zeroWidthRunes = map[rune]string{
	0x200B: "ZWSP (zero-width space)",
	0x200C: "ZWNJ (zero-width non-joiner)",
	0x200D: "ZWJ (zero-width joiner)",
	0xFEFF: "ZWNBSP (zero-width no-break space)",
}

// NoZeroWidthInSource scans source-code files for zero-width Unicode
// characters that can be smuggled into identifiers to forge symbols.
//
// Position-0 U+FEFF (UTF-8 BOM) is intentionally ignored - `encoding/no-bom`
// owns that case.
func NoZeroWidthInSource(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-zero-width-in-source",
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
			if noZeroWidthApplicableFile(path) {
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
		if !noZeroWidthApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noZeroWidthInSourceMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noZeroWidthInSourceDisableMarker) {
			continue
		}
		// Track absolute byte offset across the whole file so we can
		// ignore a position-0 BOM (handled by encoding/no-bom).
		offset := 0
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			suppressLine := i > 0 && strings.Contains(lines[i-1], noZeroWidthInSourceDisableLineMarker)
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				abs := offset + j
				if name, ok := zeroWidthRunes[r]; ok && !(r == 0xFEFF && abs == 0) {
					if !suppressLine {
						label := fmt.Sprintf("U+%04X %s", r, name)
						hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
						hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
						if len(hits) >= hitCap {
							break filesLoop
						}
						break
					}
				}
				j += size
			}
			offset += len(line) + 1 // +1 for the trailing newline (or end of file)
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "zero-width Unicode in source (identifier-forgery class): " + strings.Join(hits, "; ")
	res.Fix = "delete the offending character; identifiers with embedded zero-width runes are a known attack class. If you genuinely need ZWJ/ZWNJ for Indic / Arabic identifiers, suppress at the file level with `# appframes:disable security/no-zero-width-in-source`"
	res.Hits = hitsStruct
	return res
}
