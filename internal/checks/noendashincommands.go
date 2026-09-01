// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const noEnDashDisableMarker = "appframes:disable encoding/no-en-dash-in-commands"
const noEnDashDisableLineMarker = "appframes:disable-next-line encoding/no-en-dash-in-commands"
const noEnDashMaxFileBytes = 1 << 20

var enDashShellExtensions = map[string]bool{
	".sh":   true,
	".bash": true,
	".zsh":  true,
	".mk":   true,
}

var enDashShellBasenamePrefixes = []string{"Dockerfile"}
var enDashShellBasenames = map[string]bool{
	"Makefile":            true,
	"makefile":            true,
	"GNUmakefile":         true,
	"compose.yaml":        true,
	"compose.yml":         true,
	"docker-compose.yaml": true,
	"docker-compose.yml":  true,
}

// enDashMarkdownExtensions are scanned for COMMANDS ONLY: fenced blocks and
// inline code spans. Prose is left alone because a letter-adjacent en dash is
// legitimate there - "the Berlin-Paris route", "a Bose-Einstein condensate" -
// while inside a code span it is the paste bug this frame exists for.
var enDashMarkdownExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
}

func enDashMarkdownFile(path string) bool {
	return enDashMarkdownExtensions[strings.ToLower(filepath.Ext(path))]
}

// enDashCodeRegions returns the runs of line that are code: the whole line when
// inFence, otherwise the spans between backticks. Empty when the line carries
// no command text. Indented (4-space) code blocks are deliberately not
// recognised: they are indistinguishable from wrapped list content without a
// real markdown parser, and guessing there is how false positives start.
func enDashCodeRegions(line string, inFence bool) []string {
	if inFence {
		return []string{line}
	}
	var out []string
	for {
		start := strings.Index(line, "`")
		if start < 0 {
			return out
		}
		rest := line[start+1:]
		end := strings.Index(rest, "`")
		if end < 0 {
			return out
		}
		out = append(out, rest[:end])
		line = rest[end+1:]
	}
}

// enDashFenceToggle reports whether line opens or closes a fenced block.
func enDashFenceToggle(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func enDashApplicableFile(path string) bool {
	if enDashMarkdownFile(path) {
		return true
	}
	base := filepath.Base(path)
	if enDashShellBasenames[base] {
		return true
	}
	for _, p := range enDashShellBasenamePrefixes {
		if strings.HasPrefix(base, p) {
			return true
		}
	}
	if enDashShellExtensions[strings.ToLower(filepath.Ext(path))] {
		return true
	}
	// GitHub Actions workflow YAML lives at .github/workflows/.
	norm := filepath.ToSlash(path)
	if strings.Contains(norm, "/.github/workflows/") || strings.HasPrefix(norm, ".github/workflows/") {
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".yaml" || ext == ".yml"
	}
	return false
}

// NoEnDashInCommands flags U+2013/U+2014 adjacent to alphabetic chars.
// Catches the AI-paste failure where `--verbose` becomes `–verbose`.
// Prose en-dashes (with whitespace on both sides) are ignored.
func NoEnDashInCommands(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-en-dash-in-commands",
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
			if enDashApplicableFile(path) {
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
		if !enDashApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noEnDashMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, noEnDashDisableMarker) {
			continue
		}
		markdown := enDashMarkdownFile(file)
		inFence := false
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if markdown && enDashFenceToggle(line) {
				inFence = !inFence
				continue
			}
			if i > 0 && lineCarriesMarker(lines[i-1], noEnDashDisableLineMarker) {
				continue
			}
			scan := line
			if markdown {
				regions := enDashCodeRegions(line, inFence)
				if len(regions) == 0 {
					continue
				}
				// Join with a space so a dash at a region edge keeps its
				// neighbours: adjacency is what separates a flag from prose.
				scan = strings.Join(regions, " ")
			}
			runes := []rune(scan)
			for j, r := range runes {
				if r != 0x2013 && r != 0x2014 {
					continue
				}
				// Adjacent to a letter on either side counts as a flag-like
				// substitution. Prose ("foo – bar") is ignored.
				var prev, next rune
				if j > 0 {
					prev = runes[j-1]
				}
				if j+1 < len(runes) {
					next = runes[j+1]
				}
				if !unicode.IsLetter(prev) && !unicode.IsLetter(next) {
					continue
				}
				name := "U+2013 en-dash (likely meant `--`)"
				if r == 0x2014 {
					name = "U+2014 em-dash (likely meant `--`)"
				}
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, name))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: name})
				if len(hits) >= hitCap {
					break filesLoop
				}
				break
			}
		}
		// Silence unused-import warning if utf8 stops being referenced; we
		// rely on rune slicing instead.
		_ = utf8.RuneError
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "en/em-dash adjacent to alphabetic char (likely `--` paste corruption): " + strings.Join(hits, "; ")
	res.Fix = "replace `–` and `-` with `--`: `sed -i 's/–/--/g; s/-/--/g' <file>`"
	res.Hits = hitsStruct
	return res
}
