// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

// PatternFrontmatter is the metadata at the top of a pattern markdown
// file. Patterns are the canonical/parent frame; concrete frames are
// INSTANCES of a pattern. The pattern describes the structural shape of
// a mistake; instances bind it to specific platforms or check logic.
//
// Added 2026-05-20 with the Phase 1 architecture (see
// docs/frame-patterns.md). Patterns do not fire as gates - they're the
// abstraction layer. Only instance frames have check logic.
type PatternFrontmatter struct {
	ID                  string   `yaml:"id"`
	Description         string   `yaml:"description"`
	AnticipatedSiblings []string `yaml:"anticipated-siblings"`
}

// Pattern is a parsed pattern: frontmatter + markdown body. Distinct
// from Frame because patterns don't fire as gates - they document the
// structural shape of a class of mistakes that instance frames detect.
type Pattern struct {
	Frontmatter PatternFrontmatter
	Body        string
	SourcePath  string
}

// ID returns the pattern's identifier (its frontmatter id field).
func (p Pattern) ID() string {
	return p.Frontmatter.ID
}

// ParsePattern reads a pattern markdown file (YAML frontmatter + body)
// and returns a Pattern. Format mirrors the frame parser: fenced YAML
// frontmatter on top, markdown body below. Validation is intentionally
// lighter than Parse - patterns are documentation artifacts, not gates,
// so only id and description are required.
func ParsePattern(r io.Reader, sourcePath string) (Pattern, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return Pattern{}, fmt.Errorf("%s: empty file (expected YAML frontmatter)", sourcePath)
	}
	if strings.TrimRight(scanner.Text(), " \t\r") != frontmatterFence {
		return Pattern{}, fmt.Errorf("%s: missing opening frontmatter fence (expected %q on first line)", sourcePath, frontmatterFence)
	}

	var fmLines []string
	closed := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimRight(line, " \t\r") == frontmatterFence {
			closed = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !closed {
		return Pattern{}, fmt.Errorf("%s: unclosed frontmatter (no closing %q found)", sourcePath, frontmatterFence)
	}

	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return Pattern{}, fmt.Errorf("%s: read error: %w", sourcePath, err)
	}

	fmText := strings.Join(fmLines, "\n")
	if idx := indexControlByte(fmText); idx >= 0 {
		return Pattern{}, fmt.Errorf("%s: frontmatter contains forbidden control byte (offset %d, byte 0x%02x)",
			sourcePath, idx, fmText[idx])
	}

	var pfm PatternFrontmatter
	if err := yaml.Unmarshal([]byte(fmText), &pfm); err != nil {
		return Pattern{}, fmt.Errorf("%s: invalid pattern frontmatter YAML: %w", sourcePath, err)
	}

	if pfm.ID == "" {
		return Pattern{}, fmt.Errorf("%s: pattern frontmatter: 'id' is required", sourcePath)
	}
	if !nameRegex.MatchString(pfm.ID) {
		return Pattern{}, fmt.Errorf("%s: pattern frontmatter: 'id' must match [a-zA-Z0-9][a-zA-Z0-9_-]* (got %q)",
			sourcePath, SanitizeForOutput(pfm.ID))
	}
	if pfm.Description == "" {
		return Pattern{}, fmt.Errorf("%s: pattern frontmatter: 'description' is required", sourcePath)
	}

	return Pattern{
		Frontmatter: pfm,
		Body:        strings.TrimRight(body.String(), "\n"),
		SourcePath:  sourcePath,
	}, nil
}
