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

const noZeroWidthInContentDisableMarker = "appframes:disable encoding/no-zero-width-in-content"
const noZeroWidthInContentDisableLineMarker = "appframes:disable-next-line encoding/no-zero-width-in-content"
const noZeroWidthInContentMaxFileBytes = 1 << 20

var docExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".txt":      true,
	".rst":      true,
}

var docBasenamePrefixes = []string{"README", "LICENSE", "CHANGELOG"}

func noZeroWidthInContentApplicableFile(path string) bool {
	if docExtensions[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	base := filepath.Base(path)
	for _, p := range docBasenamePrefixes {
		if base == p || strings.HasPrefix(base, p+".") {
			return true
		}
	}
	return false
}

// NoZeroWidthInContent warns on zero-width Unicode in docs / prose
// files. WARN, not BLOCK - some i18n needs ZWJ legitimately.
// Position-0 U+FEFF is ignored (encoding/no-bom owns that case).
func NoZeroWidthInContent(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-zero-width-in-content",
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
			if noZeroWidthInContentApplicableFile(path) {
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
		if !noZeroWidthInContentApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noZeroWidthInContentMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noZeroWidthInContentDisableMarker) {
			continue
		}
		offset := 0
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			suppressLine := i > 0 && strings.Contains(lines[i-1], noZeroWidthInContentDisableLineMarker)
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
			offset += len(line) + 1
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeWarn
	res.Reason = "zero-width Unicode in prose (word-count / grep / diff confusion): " + strings.Join(hits, "; ")
	res.Fix = "strip with `LC_ALL=C sed -i 's/[\\xE2\\x80\\x8B-\\xE2\\x80\\x8D]//g' <file>`; if you need ZWJ for i18n string shaping, suppress with `<!-- appframes:disable encoding/no-zero-width-in-content -->` at file top"
	res.Hits = hitsStruct
	return res
}
