// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// binaryExtensions are formats that are never text, so text frames skip
// them without reading the content.
var binaryExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true,
	".ico": true, ".bmp": true, ".tif": true, ".tiff": true, ".heic": true, ".psd": true,
	".ora": true, ".xcf": true,
	".pdf": true, ".odt": true, ".ods": true, ".odp": true, ".docx": true, ".xlsx": true, ".pptx": true,
	".doc": true, ".xls": true, ".ppt": true, ".epub": true,
	".zip": true, ".tar": true, ".tgz": true, ".gz": true, ".bz2": true, ".xz": true, ".7z": true,
	".rar": true, ".zst": true, ".br": true, ".lz4": true, ".jar": true, ".whl": true, ".deb": true, ".rpm": true,
	".woff": true, ".woff2": true, ".ttf": true, ".otf": true, ".eot": true,
	".mp3": true, ".mp4": true, ".m4a": true, ".wav": true, ".ogg": true, ".flac": true,
	".webm": true, ".mov": true, ".avi": true, ".mkv": true,
	".so": true, ".dll": true, ".dylib": true, ".exe": true, ".bin": true, ".class": true,
	".o": true, ".a": true, ".pyc": true, ".wasm": true, ".rlib": true, ".rmeta": true,
	".db": true, ".sqlite": true, ".sqlite3": true, ".sqlite-wal": true, ".sqlite-shm": true,
	".npy": true, ".npz": true, ".pkl": true, ".pickle": true, ".parquet": true, ".h5": true,
	".onnx": true, ".pt": true, ".safetensors": true,
}

// textExtensions are formats that are always text: a NUL byte in one of
// them is corruption worth reporting, not a sign of a binary file.
var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".rst": true, ".adoc": true, ".log": true,
	".csv": true, ".tsv": true, ".json": true, ".jsonl": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".cfg": true, ".conf": true, ".env": true, ".xml": true,
	".html": true, ".htm": true, ".css": true, ".scss": true, ".svg": true,
	".go": true, ".py": true, ".js": true, ".mjs": true, ".cjs": true, ".ts": true, ".tsx": true,
	".jsx": true, ".svelte": true, ".vue": true, ".astro": true, ".rs": true, ".c": true, ".h": true,
	".cpp": true, ".hpp": true, ".java": true, ".kt": true, ".rb": true, ".php": true, ".lua": true,
	".sh": true, ".bash": true, ".zsh": true, ".ps1": true, ".sql": true, ".lean": true, ".tex": true,
}

// binarySniffBytes matches the window git's own binary heuristic reads.
const binarySniffBytes = 8000

// isBinaryContent reports whether a file is binary. Known binary
// extensions always are; known text extensions never are. Anything else
// is sniffed: a NUL byte in the first 8000 bytes (the rule git uses), or
// a window where more than 30% of the bytes are control bytes or
// invalid UTF-8 (hash tiles, packed data). Text frames must never scan
// binary files.
func isBinaryContent(data []byte, path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if binaryExtensions[ext] {
		return true
	}
	if textExtensions[ext] {
		return false
	}
	head := data
	if len(head) > binarySniffBytes {
		head = head[:binarySniffBytes]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	if len(head) == 0 {
		return false
	}
	odd := 0
	for i := 0; i < len(head); {
		r, size := utf8.DecodeRune(head[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			if len(head)-i < utf8.UTFMax && len(data) > len(head) {
				i = len(head)
				continue
			}
			odd++
		case r < 0x20 && r != '\t' && r != '\n' && r != '\r' && r != '\f':
			odd++
		}
		i += size
	}
	return odd*10 > len(head)*3
}

// isMinifiedPath reports minified bundles (`*.min.js`, `*.min.css`):
// machine output, usually vendored, whose embedded string tables are not
// the author's text.
func isMinifiedPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".min.mjs")
}
