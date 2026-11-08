// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// placeholderShape is one detection pattern + a human label.
type placeholderShape struct {
	Label   string
	Pattern *regexp.Regexp
}

var placeholderMarkerRe = regexp.MustCompile(`\b(?:FIXME|XXX|HACK)\b`)

var placeholderNameRe = regexp.MustCompile(`(?i)\b(?:lorem\s+ipsum|localhost|127\.0\.0\.1|example\.(?:com|org|net)|insert\s+(?:text|content)\s+here)\b|\b(?:TODO|FIXME|XXX|HACK)\b`)

// placeholderNameList reports a line naming two or more different kinds
// of placeholder ("lorem ipsum, localhost URLs, FIXME markers", "TODO,
// FIXME, HACK, and XXX"): prose about placeholders, not a leftover one.
func placeholderNameList(line string) bool {
	seen := map[string]bool{}
	for _, m := range placeholderNameRe.FindAllString(line, -1) {
		seen[strings.Join(strings.Fields(strings.ToLower(m)), " ")] = true
	}
	return len(seen) >= 2
}

// placeholderInLine returns the label of the placeholder on line, or "".
func placeholderInLine(line string) string {
	for _, shape := range placeholderShapes {
		if shape.Pattern.MatchString(line) {
			if placeholderNameList(line) {
				return ""
			}
			return shape.Label
		}
	}
	return ""
}

var placeholderShapes = []placeholderShape{
	{
		Label: "Lorem Ipsum placeholder text",
		// The filler itself ("Lorem ipsum dolor sit amet", or the two
		// words alone as a heading/paragraph), not prose that mentions
		// "dropping lorem ipsum in for layout".
		Pattern: regexp.MustCompile(`(?i)\blorem\s+ipsum(?:\s+dolor\b|\s*(?:$|[<.!"']))`),
	},
	{
		Label:   "INSERT TEXT HERE placeholder",
		Pattern: regexp.MustCompile(`(?i)\bINSERT\s+(?:TEXT|CONTENT)\s+HERE\b`),
	},
	{
		Label:   "<<placeholder>> marker",
		Pattern: regexp.MustCompile(`<<[A-Z][A-Z0-9_\s-]*>>`),
	},
	{
		Label:   "{{template-placeholder}}",
		Pattern: regexp.MustCompile(`\{\{[A-Z][A-Z0-9_\s-]*\}\}`),
	},
	{
		Label:   "localhost URL in shipping content",
		Pattern: regexp.MustCompile(`\bhttps?://localhost(:\d+)?\b`),
	},
	{
		Label:   "127.0.0.1 URL in shipping content",
		Pattern: regexp.MustCompile(`\bhttps?://127\.0\.0\.1(:\d+)?\b`),
	},
	{
		Label:   "FIXME / XXX / HACK marker in shipping content",
		Pattern: placeholderMarkerRe,
	},
	{
		Label:   "TODO: ship marker",
		Pattern: regexp.MustCompile(`(?i)TODO:?\s*(?:ship|prod|production|before launch)`),
	},
	{
		Label:   "example.com placeholder URL",
		Pattern: regexp.MustCompile(`(?i)\bhttps?://(?:www\.)?example\.(?:com|org|net)\b`),
	},
}

const htmlPlaceholderDisableMarker = "appframes:disable web/html-placeholder-content"
const htmlPlaceholderDisableLineMarker = "appframes:disable-next-line web/html-placeholder-content"
const htmlPlaceholderMaxFileBytes = 1 << 20 // 1 MiB

// HTMLPlaceholderContent surfaces a WARN for shipping content that
// contains common placeholder patterns: lorem ipsum, INSERT TEXT HERE,
// `<<PLACEHOLDER>>`, localhost URLs, FIXME/XXX/HACK markers,
// example.com.
//
// Scope: HTML / Svelte / Astro pages PLUS markdown / mdx files. The
// frame is about user-visible content, so source code (.js / .ts) is
// out of scope - different gate covers that.
//
// Tier 3 WARN. Easy to suppress per-line / per-file when intentional
// (documentation that genuinely needs lorem ipsum or a localhost URL).
func HTMLPlaceholderContent(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "web/html-placeholder-content",
		Category: frames.CategoryWeb,
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
			if htmlApplicableContentOrMarkdown(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 20

filesLoop:
	for _, file := range files {
		if !htmlApplicableContentOrMarkdown(file) {
			continue
		}
		if repoDocFile(file) {
			continue
		}
		rel := projectRel(ctx, file)
		if inFixtureDir(rel) || inTestDir(rel) || inHiddenDir(rel) {
			continue
		}
		// A template's {{SLOTS}} are filled by whatever renders it.
		if strings.Contains(strings.ToLower(filepath.Base(file)), "template") {
			continue
		}
		markdown := isMarkdownPath(file) || strings.EqualFold(filepath.Ext(file), ".mdoc")
		if markdown && !markdownShips(ctx, file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > htmlPlaceholderMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, htmlPlaceholderDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		// Code samples and scripts are not page copy: a documented
		// `curl https://example.com/install` or a JS fallback URL is fine.
		var scan []string
		if markdown {
			scan = markdownProse(content)
		} else {
			scan = strings.Split(htmlVisibleText(extractHTMLBody(file, content)), "\n")
		}
		for i := range lines {
			if i >= len(scan) {
				break
			}
			line := scan[i]
			if i > 0 && lineCarriesMarker(lines[i-1], htmlPlaceholderDisableLineMarker) {
				continue
			}
			if label := placeholderInLine(line); label != "" {
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "placeholder content in shipping files: " + strings.Join(hits, "; ")
	res.Fix = "replace placeholders with real content before shipping, or move the file out of the shipping path (drafts in `_drafts/`, examples in `examples/`). For documentation that legitimately uses lorem ipsum / localhost URLs / example.com, suppress per-line: `<!-- appframes:disable-next-line web/html-placeholder-content -->`."
	return res
}
