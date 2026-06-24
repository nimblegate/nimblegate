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

const noBidiOverrideDisableMarker = "appframes:disable security/no-bidi-override"
const noBidiOverrideDisableLineMarker = "appframes:disable-next-line security/no-bidi-override"
const noBidiOverrideMaxFileBytes = 1 << 20 // 1 MiB

// bidiOverrideRunes is the set of Unicode bidirectional override / isolate
// characters that reverse visual rendering of code (Trojan Source - CVE-2021-42574).
var bidiOverrideRunes = map[rune]string{
	0x202A: "LRE (left-to-right embedding)",
	0x202B: "RLE (right-to-left embedding)",
	0x202C: "PDF (pop directional formatting)",
	0x202D: "LRO (left-to-right override)",
	0x202E: "RLO (right-to-left override)",
	0x2066: "LRI (left-to-right isolate)",
	0x2067: "RLI (right-to-left isolate)",
	0x2068: "FSI (first strong isolate)",
	0x2069: "PDI (pop directional isolate)",
}

// NoBidiOverride scans files for Unicode bidirectional override characters
// (Trojan Source attack class). Presence of any such character anywhere
// in a text file triggers BLOCK.
//
// Scope contract follows the standard standard file-scan scope convention.
func NoBidiOverride(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-bidi-override",
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
		if err != nil || info.Size() > noBidiOverrideMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noBidiOverrideDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noBidiOverrideDisableLineMarker) {
				continue
			}
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				if name, ok := bidiOverrideRunes[r]; ok {
					label := fmt.Sprintf("U+%04X %s", r, name)
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
					if len(hits) >= hitCap {
						break filesLoop
					}
					break // one finding per line
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
	res.Reason = "bidirectional override character detected (Trojan Source - CVE-2021-42574): " + strings.Join(hits, "; ")
	res.Fix = "delete the offending character; if the file legitimately needs bidi controls (RTL-language code), add `# appframes:disable security/no-bidi-override` at the top of the file"
	res.Hits = hitsStruct
	return res
}
