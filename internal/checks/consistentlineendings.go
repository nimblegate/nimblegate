// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const lineEndingsDisableMarker = "appframes:disable encoding/consistent-line-endings"
const lineEndingsMaxFileBytes = 1 << 20

// crlfExemptExtensions are extensions where CRLF is conventional or
// required (Windows scripts, PowerShell, INI on Windows).
var crlfExemptExtensions = map[string]bool{
	".bat": true,
	".cmd": true,
	".ps1": true,
}

// binarySkipExtensions are extensions whose content is binary (or de facto
// binary) and where the concept of "line endings" doesn't apply. Scanning
// these files for CRLF/LF bytes produces false positives because the raw
// bytes 0x0D and 0x0A appear naturally in binary content (PNG IDAT chunks,
// JPEG markers, font metric tables, minified JS source maps, etc.).
//
// Added as the Phase G Task G2 fix surfaced by the multi-kit comparison
// validation file (myapp-history-multi-kit-comparison.md Finding 3:
// 59 false-positive BLOCK events on og-image.png across 197 commits).
var binarySkipExtensions = map[string]bool{
	// Images
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".bmp": true, ".tiff": true, ".tif": true, ".heic": true, ".avif": true,
	// Documents / archives
	".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".rar": true, ".dmg": true, ".iso": true,
	// Fonts
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	// Audio / video
	".mp4": true, ".mp3": true, ".wav": true, ".flac": true, ".ogg": true,
	".webm": true, ".m4a": true, ".mov": true, ".avi": true, ".mkv": true,
	// Compiled / object code
	".so": true, ".dll": true, ".dylib": true, ".o": true, ".a": true,
	".class": true, ".jar": true, ".wasm": true, ".pyc": true,
}

// minifiedExtensions are extensions where the file is typically machine-
// generated/minified and line-ending consistency isn't operator-controlled
// (e.g., vendor minified JS like jspdf.umd.min.js). Skipped to avoid
// false positives on bundled third-party code the operator didn't author.
var minifiedExtensions = map[string]bool{
	".min.js":  true,
	".min.css": true,
}

// isLikelyBinaryContent reports whether the first N bytes of data contain
// enough non-printable bytes to be considered binary. Used as a fallback
// when the file's extension doesn't tell us (e.g., a file with no extension,
// or a binary file with a text-like extension).
//
// Heuristic: presence of NULL byte → binary. Otherwise, if >5% of the first
// 8KB is non-printable (excluding common whitespace), call it binary.
func isLikelyBinaryContent(data []byte) bool {
	const sampleSize = 8192
	if len(data) > sampleSize {
		data = data[:sampleSize]
	}
	if len(data) == 0 {
		return false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	nonPrintable := 0
	for _, b := range data {
		switch {
		case b == '\t' || b == '\r' || b == '\n':
			// whitespace OK
		case b >= 0x20 && b <= 0x7E:
			// printable ASCII OK
		case b >= 0x80:
			// high-bit set; could be UTF-8 continuation byte, don't count as non-printable
		default:
			nonPrintable++
		}
	}
	// >5% non-printable in the sample → likely binary
	return nonPrintable*20 > len(data)
}

func lineEndingsApplicableFile(path string) bool {
	if crlfExemptExtensions[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	lower := strings.ToLower(path)
	if binarySkipExtensions[filepath.Ext(lower)] {
		return false
	}
	// Compound minified extensions like .min.js / .min.css need substring
	// match because filepath.Ext only returns the last segment.
	for ext := range minifiedExtensions {
		if strings.HasSuffix(lower, ext) {
			return false
		}
	}
	return true
}

// ConsistentLineEndings flags:
//   - mixed CRLF + LF in the same file
//   - Unix shebang + CRLF anywhere in the file
func ConsistentLineEndings(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/consistent-line-endings",
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
	const hitCap = 20

	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		if !lineEndingsApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > lineEndingsMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		// Content-based binary detection - covers files with text-like
		// extensions but binary content (e.g., a .json that's actually a
		// base64-encoded blob, or a generic binary with no extension).
		if isLikelyBinaryContent(data) {
			continue
		}
		if strings.Contains(string(data), lineEndingsDisableMarker) {
			continue
		}
		crlf := bytes.Count(data, []byte("\r\n"))
		// Bare LF = total LF minus those that are part of CRLF.
		totalLF := bytes.Count(data, []byte("\n"))
		bareLF := totalLF - crlf
		hasShebang := bytes.HasPrefix(data, []byte("#!"))

		// Skip empty / line-ending-free files.
		if crlf == 0 && bareLF == 0 {
			continue
		}

		switch {
		case hasShebang && crlf > 0:
			label := "Unix shebang + CRLF (kernel will reject the interpreter)"
			hits = append(hits, fmt.Sprintf("%s:1 - %s", file, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 1, Label: label})
		case crlf > 0 && bareLF > 0:
			label := fmt.Sprintf("mixed line endings (CRLF=%d, LF=%d)", crlf, bareLF)
			hits = append(hits, fmt.Sprintf("%s:0 - %s", file, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: label})
		}
		if len(hits) >= hitCap {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "inconsistent line endings: " + strings.Join(hits, "; ")
	res.Fix = "normalize to LF: `dos2unix <file>` or `sed -i 's/\\r$//' <file>`. Lock it in .gitattributes: `* text=auto eol=lf`"
	res.Hits = hitsStruct
	return res
}
