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

const noNonPrintableDisableMarker = "appframes:disable encoding/no-non-printable"
const noNonPrintableDisableLineMarker = "appframes:disable-next-line encoding/no-non-printable"
const noNonPrintableMaxFileBytes = 1 << 20

// noNonPrintableSkipExtensions are extensions where control bytes are
// expected (binary / generated content).
var noNonPrintableSkipExtensions = map[string]bool{
	".png":   true,
	".jpg":   true,
	".jpeg":  true,
	".gif":   true,
	".webp":  true,
	".ico":   true,
	".pdf":   true,
	".zip":   true,
	".tar":   true,
	".gz":    true,
	".bz2":   true,
	".xz":    true,
	".7z":    true,
	".woff":  true,
	".woff2": true,
	".ttf":   true,
	".otf":   true,
	".eot":   true,
	".mp3":   true,
	".mp4":   true,
	".wav":   true,
	".webm":  true,
	".so":    true,
	".dll":   true,
	".exe":   true,
	".bin":   true,
	".class": true,
	".o":     true,
	".a":     true,
	".pyc":   true,
}

// isNonPrintableControl reports whether r is a C0 (excl. \t \n \r) or
// C1 control character that has no legitimate use in text files.
func isNonPrintableControl(r rune) bool {
	switch r {
	case '\t', '\n', '\r':
		return false
	}
	if r < 0x20 || r == 0x7F {
		return true
	}
	if r >= 0x80 && r <= 0x9F {
		return true
	}
	return false
}

// NoNonPrintable warns on C0/C1 control characters in text files.
// Typically leaked from terminal paste (ESC bytes from ANSI color),
// screen capture, or copy-from-PDF.
func NoNonPrintable(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-non-printable",
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
		if noNonPrintableSkipExtensions[strings.ToLower(filepath.Ext(file))] {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noNonPrintableMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noNonPrintableDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noNonPrintableDisableLineMarker) {
				continue
			}
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				if isNonPrintableControl(r) {
					label := fmt.Sprintf("U+%04X (non-printable control)", r)
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
	res.Reason = "non-printable control character in text file: " + strings.Join(hits, "; ")
	res.Fix = "strip with `LC_ALL=C sed -i 's/[\\x00-\\x08\\x0b\\x0c\\x0e-\\x1f\\x7f-\\x9f]//g' <file>`; for ANSI-leaked ESC bytes specifically: `sed -i 's/\\x1b\\[[0-9;]*[A-Za-z]//g' <file>`"
	res.Hits = hitsStruct
	return res
}
