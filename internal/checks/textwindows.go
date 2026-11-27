// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// secretScanMaxBytes caps the files the two key-pattern frames
// (no-hardcoded-credentials, no-private-keys-in-repo) read. Everything up to
// it is streamed in windows, so memory stays near textWindowBytes per file
// whatever the size and the RAM argument behind DefaultMaxFileBytes still
// holds; the cap only bounds scan time. Bundles a build commits (dist/,
// public/) are routinely 0.5-2 MiB, and a key in one is still a leak.
const secretScanMaxBytes = 64 << 20

const (
	textWindowBytes  = 1 << 20
	textOverlapBytes = 64 << 10
	textOverlapLines = 64
)

// longLineBytes marks a line as machine output (a minified bundle): findings
// on it name the column, since "line 1" points nowhere.
const longLineBytes = 1000

// textWindow is a run of consecutive lines, or one piece of a line longer
// than a window. Windows overlap, so a finding that needs the lines after it
// (a PEM body) or straddles a cut is whole in at least one window; callers
// dedupe by position.
type textWindow struct {
	Lines     []string
	FirstLine int    // 1-based line number of Lines[0]
	FirstCol  int    // 1-based byte column where Lines[0] starts
	Cut       bool   // Lines[0] is a piece of a line longer than a window
	Prev      string // the line before Lines[0], for disable-next-line markers
}

// col returns the 1-based column of byte offset at in w.Lines[i].
func (w textWindow) col(i, at int) int {
	if i == 0 {
		return w.FirstCol + at
	}
	return at + 1
}

// long reports whether w.Lines[i] is part of a minified-length line.
func (w textWindow) long(i int) bool {
	return (i == 0 && w.Cut) || len(w.Lines[i]) > longLineBytes
}

// prev returns the line before w.Lines[i].
func (w textWindow) prev(i int) string {
	if i == 0 {
		return w.Prev
	}
	return w.Lines[i-1]
}

// scanTextWindows reads r and calls fn per window until fn returns false.
func scanTextWindows(r io.Reader, fn func(textWindow) bool) error {
	br := bufio.NewReaderSize(r, 64<<10)
	// readLine returns up to about textWindowBytes of the current line and
	// whether the line ended there.
	readLine := func() (string, bool, error) {
		var b []byte
		for len(b) < textWindowBytes {
			frag, err := br.ReadSlice('\n')
			b = append(b, frag...)
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if err != nil {
				if len(b) == 0 {
					return "", false, err
				}
				return string(b), true, nil
			}
			return string(b[:len(b)-1]), true, nil
		}
		return string(b), false, nil
	}

	var lines []string
	size, fresh := 0, 0
	first, lineNo := 1, 1
	prev := ""
	flush := func() bool {
		if fresh == 0 {
			return true
		}
		if !fn(textWindow{Lines: lines, FirstLine: first, FirstCol: 1, Prev: prev}) {
			return false
		}
		keep, kb := 0, 0
		for keep < len(lines)-1 && keep < textOverlapLines {
			l := lines[len(lines)-1-keep]
			if kb+len(l)+1 > textOverlapBytes {
				break
			}
			kb += len(l) + 1
			keep++
		}
		prev = lines[len(lines)-1-keep]
		lines = append([]string(nil), lines[len(lines)-keep:]...)
		first = lineNo - keep
		size, fresh = kb, 0
		return true
	}

	for {
		text, complete, err := readLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				flush()
				return nil
			}
			return err
		}
		if complete {
			lines = append(lines, text)
			size += len(text) + 1
			fresh++
			lineNo++
			if size >= textWindowBytes && !flush() {
				return nil
			}
			continue
		}

		// A line longer than a window: send the lines before it, then the
		// line in overlapping pieces. Carried lines would break the line
		// numbering of the next window, so none are kept.
		if !flush() {
			return nil
		}
		if len(lines) > 0 {
			prev = lines[len(lines)-1]
		}
		lines, size = nil, 0
		col := 1
		piece := text
		for {
			if !fn(textWindow{Lines: []string{piece}, FirstLine: lineNo, FirstCol: col, Cut: true, Prev: prev}) {
				return nil
			}
			if complete {
				break
			}
			more, c, err := readLine()
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
			ov := piece
			if len(ov) > textOverlapBytes {
				ov = ov[len(ov)-textOverlapBytes:]
			}
			col += len(piece) - len(ov)
			piece, complete = ov+more, c
		}
		prev = ""
		lineNo++
		first = lineNo
	}
}

// secretFileStatus says what scanSecretFile did with a file.
type secretFileStatus int

const (
	secretFileScanned  secretFileStatus = iota
	secretFileSkipped                   // unreadable, binary, or missing
	secretFileDisabled                  // carries the frame's file-level disable marker
	secretFileTooLarge                  // text file over secretScanMaxBytes
)

// scanSecretFile feeds one file to fn window by window, for the frames that
// look for keys by their fixed shape. Files up to DefaultMaxFileBytes are read
// whole, as before; larger ones are streamed up to secretScanMaxBytes,
// skipping binary extensions and files with a NUL byte, after a first pass
// that looks for the file-level disable marker the way fileDisabledByMarker
// does on a small file. The line heuristics (possible-secret,
// no-plaintext-passwords) keep the 1 MiB cap: on big generated code they ran
// at about 3.5 MB/s and raised false alarms on parser variables.
func scanSecretFile(path, disableMarker string, fn func(textWindow) bool) secretFileStatus {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return secretFileSkipped
	}
	if info.Size() <= DefaultMaxFileBytes {
		data, ok := ReadFileBounded(path, DefaultMaxFileBytes)
		if !ok {
			return secretFileSkipped
		}
		content := string(data)
		if fileDisabledByMarker(content, disableMarker) {
			return secretFileDisabled
		}
		_ = scanTextWindows(strings.NewReader(content), fn)
		return secretFileScanned
	}
	if binarySkipExtensions[strings.ToLower(filepath.Ext(path))] {
		return secretFileSkipped
	}
	if info.Size() > secretScanMaxBytes {
		return secretFileTooLarge
	}

	binary, disabled := false, false
	if err := streamFile(path, func(w textWindow) bool {
		for i, l := range w.Lines {
			if strings.IndexByte(l, 0) >= 0 {
				binary = true
				return false
			}
			if !w.long(i) && markerOwnsLine(l, disableMarker) {
				disabled = true
			}
		}
		return true
	}); err != nil || binary {
		return secretFileSkipped
	}
	if disabled {
		return secretFileDisabled
	}
	if streamFile(path, fn) != nil {
		return secretFileSkipped
	}
	return secretFileScanned
}

func streamFile(path string, fn func(textWindow) bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return scanTextWindows(f, fn)
}
