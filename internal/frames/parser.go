// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// nameRegex matches the same pattern the JSON schema enforces: leading
// alphanumeric, followed by alphanumerics, underscores, or hyphens.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

const frontmatterFence = "---"

// Parse reads a frame markdown file (YAML frontmatter + body) and returns a Frame.
// sourcePath is the original location (file path, embed path, or test URI) and is
// stored on the returned Frame for error messages and dedup.
func Parse(r io.Reader, sourcePath string) (Frame, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	if !scanner.Scan() {
		return Frame{}, fmt.Errorf("%s: empty file (expected YAML frontmatter)", sourcePath)
	}
	if strings.TrimRight(scanner.Text(), " \t\r") != frontmatterFence {
		return Frame{}, fmt.Errorf("%s: missing opening frontmatter fence (expected %q on first line)", sourcePath, frontmatterFence)
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
		return Frame{}, fmt.Errorf("%s: unclosed frontmatter (no closing %q found)", sourcePath, frontmatterFence)
	}

	var body strings.Builder
	for scanner.Scan() {
		body.WriteString(scanner.Text())
		body.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return Frame{}, fmt.Errorf("%s: read error: %w", sourcePath, err)
	}

	fmText := strings.Join(fmLines, "\n")

	// Defense in depth: reject control bytes in frontmatter
	// before handing it to the YAML decoder. A name field carrying raw ESC
	// or other C0 control bytes confuses the YAML scanner and surfaces a
	// generic "unclosed frontmatter" error - misleading the user about the
	// actual cause. The output sanitizer already prevents terminal injection
	// downstream; this surfaces the right error at the right layer.
	if idx := indexControlByte(fmText); idx >= 0 {
		return Frame{}, fmt.Errorf("%s: frontmatter contains forbidden control byte (offset %d, byte 0x%02x) - frame field values must be printable",
			sourcePath, idx, fmText[idx])
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
		return Frame{}, fmt.Errorf("%s: invalid frontmatter YAML: %w", sourcePath, err)
	}

	if err := validateFrontmatter(fm); err != nil {
		return Frame{}, fmt.Errorf("%s: %w", sourcePath, err)
	}

	return Frame{
		Frontmatter: fm,
		Body:        strings.TrimRight(body.String(), "\n"),
		SourcePath:  sourcePath,
	}, nil
}

func validateFrontmatter(fm Frontmatter) error {
	if fm.Name == "" {
		return fmt.Errorf("frontmatter: 'name' is required")
	}
	if !nameRegex.MatchString(fm.Name) {
		return fmt.Errorf("frontmatter: 'name' must match [a-zA-Z0-9][a-zA-Z0-9_-]* (got %q)",
			SanitizeForOutput(fm.Name))
	}
	if fm.Category == "" {
		return fmt.Errorf("frontmatter: 'category' is required")
	}
	canonical := map[string]struct{}{
		"security": {}, "network": {}, "filesystem": {}, "git": {}, "commands": {},
		"app-correctness": {}, "database": {}, "web": {}, "documentation": {}, "platform": {}, "framework": {},
		"encoding": {},
	}
	if _, ok := canonical[string(fm.Category)]; !ok {
		return fmt.Errorf("frontmatter: category %q is not one of the 12 canonical values "+
			"(security, network, filesystem, git, commands, app-correctness, "+
			"database, web, documentation, platform, framework, encoding)", fm.Category)
	}
	if strings.TrimSpace(fm.Subcategory) == "" {
		return fmt.Errorf("frontmatter: subcategory is required and must be non-empty")
	}
	if fm.Severity == "" {
		return fmt.Errorf("frontmatter: 'severity' is required")
	}
	if fm.Severity != SeverityBlock && fm.Severity != SeverityWarn && fm.Severity != SeverityInfo {
		return fmt.Errorf("frontmatter: unknown severity %q (must be BLOCK, WARN, or INFO)",
			SanitizeForOutput(string(fm.Severity)))
	}
	if len(fm.Triggers) == 0 {
		return fmt.Errorf("frontmatter: 'triggers' must contain at least one entry")
	}
	if fm.Tier != 0 && (fm.Tier < 1 || fm.Tier > 6) {
		return fmt.Errorf("frontmatter: 'tier' must be 1-6 (got %d)", fm.Tier)
	}
	if fm.DedupKey != "" && fm.DedupKey != "file" && fm.DedupKey != "file:line" {
		return fmt.Errorf("frontmatter: 'dedup-key' must be \"file\" or \"file:line\" (got %q)",
			SanitizeForOutput(fm.DedupKey))
	}
	if fm.TimeCostHoursPrevented < 0 {
		return fmt.Errorf("frontmatter: 'time-cost-hours-prevented' must be >= 0 (got %v)", fm.TimeCostHoursPrevented)
	}
	if fm.Pattern != "" && !nameRegex.MatchString(fm.Pattern) {
		return fmt.Errorf("frontmatter: 'pattern' must match [a-zA-Z0-9][a-zA-Z0-9_-]* (got %q)",
			SanitizeForOutput(fm.Pattern))
	}
	if fm.Lifecycle != "" && !isKnownLifecycle(fm.Lifecycle) {
		return fmt.Errorf("frontmatter: unknown lifecycle %q (must be one of proposed, candidate, active, deprecated, archived)",
			SanitizeForOutput(string(fm.Lifecycle)))
	}
	if fm.SelectionGrade != "" && !isKnownSelectionGrade(fm.SelectionGrade) {
		return fmt.Errorf("frontmatter: unknown selection-grade %q (must be one of passing, failing, pending, pre-architecture)",
			SanitizeForOutput(fm.SelectionGrade))
	}
	return nil
}

func isKnownSelectionGrade(g string) bool {
	switch g {
	case "passing", "failing", "pending", "pre-architecture":
		return true
	}
	return false
}

func isKnownLifecycle(l Lifecycle) bool {
	switch l {
	case LifecycleProposed, LifecycleCandidate, LifecycleActive, LifecycleDeprecated, LifecycleArchived:
		return true
	}
	return false
}

// indexControlByte returns the offset of the first forbidden control byte
// in s, or -1 if none. Tab (0x09) and newline (0x0a) are allowed because
// YAML uses them; carriage return (0x0d) is also allowed for CRLF files.
// Everything else below 0x20 (and 0x7f DEL) is rejected.
func indexControlByte(s string) int {
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch b {
		case '\t', '\n', '\r':
			continue
		}
		if b < 0x20 || b == 0x7f {
			return i
		}
	}
	return -1
}
