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

// mixedIndentTabsThenSpacesOK reports whether tabs followed by spaces are a
// legitimate shape in this file: a make recipe line must start with a tab
// and the spaces after it belong to the shell command, and C-family code
// conventionally indents with tabs and aligns with spaces. Only a space
// before a tab is wrong there - make rejects the recipe, and the width
// depends on the reader's tab stop.
func mixedIndentTabsThenSpacesOK(path string) bool {
	if mixedIndentBasenames[filepath.Base(path)] {
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mk", ".c", ".h", ".cpp", ".cc", ".cxx", ".hpp":
		return true
	}
	return false
}

// goLinesStartingInLiteral marks, per line, whether the line begins inside a
// raw string literal or a block comment. Its leading whitespace is then
// content (embedded SQL, JSON, an indented example), not indentation:
// gofmt normalises every code line to tabs and leaves literals alone.
func goLinesStartingInLiteral(lines []string) []bool {
	out := make([]bool, len(lines))
	const (
		code = iota
		raw
		block
	)
	state := code
	for i, line := range lines {
		out[i] = state != code
		for j := 0; j < len(line); j++ {
			c := line[j]
			switch state {
			case raw:
				if c == '`' {
					state = code
				}
			case block:
				if c == '*' && j+1 < len(line) && line[j+1] == '/' {
					state = code
					j++
				}
			default:
				switch {
				case c == '`':
					state = raw
				case c == '/' && j+1 < len(line) && line[j+1] == '*':
					state = block
					j++
				case c == '/' && j+1 < len(line) && line[j+1] == '/':
					j = len(line)
				case c == '"' || c == '\'':
					// Interpreted strings and runes end on this line; skip
					// them so a backtick inside one opens nothing.
					for j++; j < len(line) && line[j] != c; j++ {
						if line[j] == '\\' {
							j++
						}
					}
				}
			}
		}
	}
	return out
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
		if fileDisabledByMarker(content, noMixedIndentDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		alignWithSpaces := mixedIndentTabsThenSpacesOK(file)
		var inLiteral []bool
		if strings.EqualFold(filepath.Ext(file), ".go") {
			inLiteral = goLinesStartingInLiteral(lines)
		}
		for i, line := range lines {
			if i > 0 && lineCarriesMarker(lines[i-1], noMixedIndentDisableLineMarker) {
				continue
			}
			if inLiteral != nil && inLiteral[i] {
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
			if alignWithSpaces {
				lead := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				if !strings.Contains(lead, " \t") {
					continue
				}
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
