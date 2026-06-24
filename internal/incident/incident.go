// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package incident captures recurring footguns as durable notes that can be
// promoted into stdlib-style frames. An incident file lives at
// .appframes/_incidents/YYYY-MM-DD-slug.md and follows the cf-incidents
// catalog shape (Incident / Detection signal / Frame proposal / Where the
// check belongs / Generalizes to).
//
// The package is intentionally minimal: parse, write, scaffold, find-by-slug.
// Pattern detection over the audit log lives elsewhere; this package only
// owns the on-disk shape.
package incident

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// IncidentsDirName is the project-relative directory under .appframes/ where
// incident files live. Leading underscore matches whitelist.toml's _canonical
// convention: nimblegate-owned subdir, not user-authored frame markdown.
const IncidentsDirName = "_incidents"

// Status of an incident in its capture → promotion lifecycle.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusPromoted Status = "promoted"
)

// Source describes how the incident was captured. "bypass" is set when the
// post-`--force-yes` prompt scaffolded the file; "manual" is the
// `nimblegate incident new` default.
type Source string

const (
	SourceManual Source = "manual"
	SourceBypass Source = "bypass"
)

// Frontmatter is the YAML block at the top of an incident file. All fields
// except Title and Date are optional with sensible zero-value defaults so a
// half-filled draft still loads.
type Frontmatter struct {
	Title         string   `yaml:"title"`
	Date          string   `yaml:"date"`
	TimeCostHours float64  `yaml:"time-cost-hours,omitempty"`
	Status        Status   `yaml:"status"`
	PromotedTo    string   `yaml:"promoted-to,omitempty"`
	Source        Source   `yaml:"source"`
	SourceFrame   string   `yaml:"source-frame,omitempty"`
	SourceReason  string   `yaml:"source-reason,omitempty"`
	SourceCommand string   `yaml:"source-command,omitempty"`
	Tags          []string `yaml:"tags,omitempty"`
}

// Incident is a parsed incident file: frontmatter + body.
type Incident struct {
	Frontmatter Frontmatter
	Body        string
	SourcePath  string // absolute path to the .md file on disk
}

// Slug returns the kebab-case slug derived from the source path (the filename
// without YYYY-MM-DD- prefix and .md extension). Used by `promote` to find
// an incident by user-supplied identifier.
func (i Incident) Slug() string {
	base := strings.TrimSuffix(filepath.Base(i.SourcePath), ".md")
	// Strip leading "YYYY-MM-DD-" if present (the filename convention).
	if len(base) > 11 && base[4] == '-' && base[7] == '-' && base[10] == '-' {
		return base[11:]
	}
	return base
}

const frontmatterFence = "---"

// Parse reads an incident markdown file (frontmatter + body) and returns
// an Incident. sourcePath is recorded on the result for downstream use.
func Parse(r io.Reader, sourcePath string) (Incident, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	if !sc.Scan() {
		return Incident{}, fmt.Errorf("%s: empty file (expected YAML frontmatter)", sourcePath)
	}
	if strings.TrimRight(sc.Text(), " \t\r") != frontmatterFence {
		return Incident{}, fmt.Errorf("%s: missing opening frontmatter fence", sourcePath)
	}

	var fmLines []string
	closed := false
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimRight(line, " \t\r") == frontmatterFence {
			closed = true
			break
		}
		fmLines = append(fmLines, line)
	}
	if !closed {
		return Incident{}, fmt.Errorf("%s: unclosed frontmatter", sourcePath)
	}

	var body strings.Builder
	for sc.Scan() {
		body.WriteString(sc.Text())
		body.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return Incident{}, fmt.Errorf("%s: read: %w", sourcePath, err)
	}

	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(strings.Join(fmLines, "\n")), &fm); err != nil {
		return Incident{}, fmt.Errorf("%s: invalid frontmatter: %w", sourcePath, err)
	}

	if fm.Status == "" {
		fm.Status = StatusDraft
	}
	if fm.Source == "" {
		fm.Source = SourceManual
	}

	return Incident{
		Frontmatter: fm,
		Body:        strings.TrimRight(body.String(), "\n"),
		SourcePath:  sourcePath,
	}, nil
}

// Write serializes an incident back to disk at i.SourcePath. Used by
// `promote` to update frontmatter (status → promoted, promoted-to → frame ID)
// without rewriting the body.
func (i Incident) Write() error {
	if i.SourcePath == "" {
		return fmt.Errorf("incident: write: no source path set")
	}
	data, err := i.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(i.SourcePath, data, 0o644)
}

// Marshal renders the incident to its on-disk markdown form (frontmatter +
// body) without writing it.
func (i Incident) Marshal() ([]byte, error) {
	fmBytes, err := yaml.Marshal(i.Frontmatter)
	if err != nil {
		return nil, fmt.Errorf("incident: marshal frontmatter: %w", err)
	}
	var buf strings.Builder
	buf.WriteString(frontmatterFence)
	buf.WriteByte('\n')
	buf.Write(fmBytes)
	buf.WriteString(frontmatterFence)
	buf.WriteString("\n\n")
	buf.WriteString(i.Body)
	if !strings.HasSuffix(i.Body, "\n") {
		buf.WriteByte('\n')
	}
	return []byte(buf.String()), nil
}

// LoadFromDir reads every *.md file under incidentsDir and returns parsed
// incidents. Files that fail to parse are returned as a separate error slice
// - one bad draft does not block the rest, matching the loader pattern used
// by frames.LoadFromDir.
func LoadFromDir(incidentsDir string) ([]Incident, []error) {
	var out []Incident
	var errs []error
	entries, err := os.ReadDir(incidentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []error{fmt.Errorf("incident: read dir %s: %w", incidentsDir, err)}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(incidentsDir, name)
		f, err := os.Open(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
			continue
		}
		inc, err := Parse(f, path)
		_ = f.Close()
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, inc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourcePath < out[j].SourcePath })
	return out, errs
}

// FindBySlug returns the incident whose slug matches, or an error if none /
// multiple match. Slugs are unique by construction (filename collision is
// caught at `new` time) so multiple-match indicates a hand-edit conflict.
func FindBySlug(incidentsDir, slug string) (Incident, error) {
	incs, _ := LoadFromDir(incidentsDir)
	var matches []Incident
	for _, inc := range incs {
		if inc.Slug() == slug {
			matches = append(matches, inc)
		}
	}
	switch len(matches) {
	case 0:
		return Incident{}, fmt.Errorf("incident: no incident with slug %q in %s", slug, incidentsDir)
	case 1:
		return matches[0], nil
	default:
		return Incident{}, fmt.Errorf("incident: %d incidents share slug %q (rename one)", len(matches), slug)
	}
}

// slugRegex constrains user-supplied titles into safe filename slugs.
var slugRegex = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify converts an arbitrary title into a lowercase kebab-case slug.
// Trims to 60 chars; collapses runs of non-alphanumeric into a single dash.
func Slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugRegex.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		s = strings.TrimRight(s, "-")
	}
	if s == "" {
		s = "incident"
	}
	return s
}

// Filename returns the YYYY-MM-DD-slug.md filename for an incident captured
// on the given date.
func Filename(date time.Time, slug string) string {
	return fmt.Sprintf("%s-%s.md", date.Format("2006-01-02"), slug)
}
