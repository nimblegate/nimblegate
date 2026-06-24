// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const htmlImgAltDisableMarker = "appframes:disable web/html-img-alt"
const htmlImgAltDisableLineMarker = "appframes:disable-next-line web/html-img-alt"
const htmlImgAltMaxFileBytes = 1 << 20 // 1 MiB

// HTMLImgAlt gates every <img> tag on the presence of an `alt` attribute.
// Empty alt ("") is acceptable (decorative image convention). Missing
// alt = WARN.
//
// Tier 6. Catches the a11y + SEO miss in one pass.
func HTMLImgAlt(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "web/html-img-alt",
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
			if htmlApplicableFile(path) {
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
		if !htmlApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > htmlImgAltMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, htmlImgAltDisableMarker) {
			continue
		}
		body := extractHTMLBody(file, content)
		lines := strings.Split(content, "\n")

		// Walk tokens; for each `<img>` start (or self-closing) tag, check
		// for an `alt` attribute.
		tok := html.NewTokenizer(strings.NewReader(body))
		for {
			tt := tok.Next()
			if tt == html.ErrorToken {
				break
			}
			if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
				continue
			}
			tagName, hasAttr := tok.TagName()
			if string(tagName) != "img" {
				continue
			}
			hasAlt := false
			for hasAttr {
				key, _, more := tok.TagAttr()
				if string(key) == "alt" {
					hasAlt = true
				}
				hasAttr = more
			}
			if hasAlt {
				continue
			}
			// Token offsets aren't directly available from the tokenizer;
			// estimate the line by computing bytes-consumed-so-far. The
			// tokenizer exposes Raw() which is the original markup for the
			// token; we find it in body and translate to a line number.
			raw := string(tok.Raw())
			line := approximateLineOf(body, raw, content)
			if line > 1 && strings.Contains(lines[line-2], htmlImgAltDisableLineMarker) {
				continue
			}
			label := "<img> missing alt attribute (a11y + SEO)"
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, line, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: line, Label: label})
			if len(hits) >= hitCap {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "<img> tags missing alt: " + strings.Join(hits, "; ")
	res.Fix = "every <img> needs an alt attribute. Use alt=\"description of the image\" for informative images and alt=\"\" for decorative ones (screen readers will skip those). The empty-string form is intentional in HTML5 and satisfies this check."
	return res
}

// approximateLineOf locates raw in body and translates the byte offset
// to a 1-based line number in the original content. Falls back to line 1
// when the lookup fails (rare - typically due to body != content for
// .svelte or .astro files; the index returned is still an upper bound
// useful for users to find the tag).
func approximateLineOf(body, raw, content string) int {
	idx := strings.Index(body, raw)
	if idx < 0 {
		return 1
	}
	// body and content share a 1:1 character mapping for the parts we
	// kept (the stripper preserves newline positions for Svelte+Astro).
	// So an offset within body corresponds to the same offset within
	// content for non-stripped regions.
	return 1 + strings.Count(content[:min(idx, len(content))], "\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
