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

const noHomoglyphDisableMarker = "appframes:disable security/no-homoglyph-identifiers"
const noHomoglyphDisableLineMarker = "appframes:disable-next-line security/no-homoglyph-identifiers"
const noHomoglyphMaxFileBytes = 1 << 20

// latinConfusables maps Cyrillic / Greek runes that visually collide
// with Latin letters to their confusable Latin counterpart. Set is
// intentionally tight: the most common attack-shape substitutions,
// not the full UTR #39 catalog.
var latinConfusables = map[rune]string{
	// Cyrillic lowercase
	0x0430: "Cyrillic small letter a (looks like Latin a)",
	0x0435: "Cyrillic small letter ye (looks like Latin e)",
	0x043E: "Cyrillic small letter o (looks like Latin o)",
	0x0440: "Cyrillic small letter er (looks like Latin p)",
	0x0441: "Cyrillic small letter es (looks like Latin c)",
	0x0443: "Cyrillic small letter u (looks like Latin y)",
	0x0445: "Cyrillic small letter ha (looks like Latin x)",
	// Cyrillic uppercase
	0x0410: "Cyrillic capital letter A (looks like Latin A)",
	0x0415: "Cyrillic capital letter Ye (looks like Latin E)",
	0x041E: "Cyrillic capital letter O (looks like Latin O)",
	0x0420: "Cyrillic capital letter Er (looks like Latin P)",
	0x0421: "Cyrillic capital letter Es (looks like Latin C)",
	// Greek lowercase
	0x03B1: "Greek small letter alpha (looks like Latin a)",
	0x03BF: "Greek small letter omicron (looks like Latin o)",
	0x03C1: "Greek small letter rho (looks like Latin p)",
	0x03C5: "Greek small letter upsilon (looks like Latin u)",
	// Greek uppercase
	0x0391: "Greek capital letter Alpha (looks like Latin A)",
	0x039F: "Greek capital letter Omicron (looks like Latin O)",
	0x03A1: "Greek capital letter Rho (looks like Latin P)",
}

func noHomoglyphApplicableFile(path string) bool {
	return sourceCodeExtensions[strings.ToLower(filepath.Ext(path))]
}

// NoHomoglyphIdentifiers warns on Cyrillic / Greek runes in source files
// that look identical to Latin letters in monospace fonts. WARN, not
// BLOCK - legitimate i18n needs are too common to fail the gate by default.
func NoHomoglyphIdentifiers(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-homoglyph-identifiers",
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
			if noHomoglyphApplicableFile(path) {
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
		if !noHomoglyphApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noHomoglyphMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noHomoglyphDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noHomoglyphDisableLineMarker) {
				continue
			}
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				if desc, ok := latinConfusables[r]; ok {
					label := fmt.Sprintf("U+%04X %s", r, desc)
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
	res.Outcome = engine.OutcomeWarn
	res.Reason = "Latin-confusable character in source (homoglyph identifier risk): " + strings.Join(hits, "; ")
	res.Fix = "replace with the Latin equivalent if unintentional. If the file legitimately contains Cyrillic / Greek (i18n strings, translation tables), suppress with `# appframes:disable security/no-homoglyph-identifiers`"
	res.Hits = hitsStruct
	return res
}
