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

// htmlVoidElements are HTML5 void elements: tags that never have a
// closing form. The tokenizer reports them as start tags; we treat them
// as auto-closed and don't push onto the open-tag stack.
var htmlVoidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// htmlForeignContentTags are tags whose body content is foreign (SVG,
// MathML, or framework-specific). The tokenizer can mishandle nested
// SVG enough that we skip balance-checking once we enter one, until we
// see the matching close tag.
var htmlForeignContentTags = map[string]bool{
	"svg": true, "math": true, "script": true, "style": true, "template": true,
}

const htmlMarkupValidDisableMarker = "appframes:disable web/html-markup-valid"
const htmlMarkupValidMaxFileBytes = 1 << 20 // 1 MiB

// HTMLMarkupValid walks the file with the HTML5 tokenizer and reports:
//
//   - Unclosed tags (start tag never matched by a close)
//   - Mismatched closing tags (`</div>` when the current open is `<span>`)
//   - Duplicate `id=` values within the same file
//
// This covers the "Vite / HTML parser complaint" class. Inside foreign
// content (svg, math, script, style, template) we skip balance checking
// because those subtrees have different parsing rules.
//
// Tier 3 WARN. Not a substitute for a proper HTML validator (e.g.
// vnu.jar) but catches the common shapes at zero infrastructure cost.
func HTMLMarkupValid(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "web/html-markup-valid",
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
		if err != nil || info.Size() > htmlMarkupValidMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, htmlMarkupValidDisableMarker) {
			continue
		}
		body := extractHTMLBody(file, content)

		fileHits := walkHTMLTokens(file, body, content)
		for _, h := range fileHits {
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, h.Line, h.Label))
			hitsStruct = append(hitsStruct, h)
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
	res.Reason = "HTML markup validation: " + strings.Join(hits, "; ")
	res.Fix = "fix the unclosed / mismatched tag and duplicate IDs. Browsers tolerate most of these via the HTML5 error-recovery algorithm, but downstream parsers (RSS readers, screen readers, social-card scrapers, AMP validators) are stricter."
	return res
}

// walkHTMLTokens scans `body` with the HTML5 tokenizer and returns Hit
// rows for tag balance + duplicate-id issues. `body` is the stripped
// HTML view; `content` is the original file content for line-mapping.
func walkHTMLTokens(file, body, content string) []engine.Hit {
	var hits []engine.Hit

	tok := html.NewTokenizer(strings.NewReader(body))
	type openTag struct {
		name string
		line int
	}
	var stack []openTag
	idLines := map[string]int{}
	// foreignDepth tracks nested foreign-content tags. While > 0 we skip
	// balance checking but still process close tags to pop the depth.
	foreignDepth := 0

	for {
		tt := tok.Next()
		if tt == html.ErrorToken {
			break
		}

		raw := string(tok.Raw())
		line := approximateLineOf(body, raw, content)

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			tagName, hasAttr := tok.TagName()
			name := strings.ToLower(string(tagName))
			// Collect id values for duplicate detection.
			for hasAttr {
				key, val, more := tok.TagAttr()
				if string(key) == "id" {
					id := string(val)
					if id != "" {
						if prev, exists := idLines[id]; exists {
							hits = append(hits, engine.Hit{
								File: file, Line: line,
								Label: fmt.Sprintf("duplicate id=%q (first seen on line %d)", id, prev),
							})
						} else {
							idLines[id] = line
						}
					}
				}
				hasAttr = more
			}
			if tt == html.SelfClosingTagToken {
				continue
			}
			if htmlVoidElements[name] {
				// Void elements never appear on the stack.
				continue
			}
			if foreignDepth > 0 {
				if htmlForeignContentTags[name] {
					foreignDepth++
				}
				continue
			}
			if htmlForeignContentTags[name] {
				foreignDepth++
				continue
			}
			stack = append(stack, openTag{name: name, line: line})

		case html.EndTagToken:
			tagName, _ := tok.TagName()
			name := strings.ToLower(string(tagName))
			if foreignDepth > 0 {
				if htmlForeignContentTags[name] {
					foreignDepth--
				}
				continue
			}
			if htmlVoidElements[name] {
				// `</br>` etc. - meaningless, ignore.
				continue
			}
			if len(stack) == 0 {
				hits = append(hits, engine.Hit{
					File: file, Line: line,
					Label: fmt.Sprintf("</%s> closes nothing (no matching open tag)", name),
				})
				continue
			}
			top := stack[len(stack)-1]
			if top.name == name {
				stack = stack[:len(stack)-1]
				continue
			}
			// Mismatch - look further down for the matching open.
			matchIdx := -1
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].name == name {
					matchIdx = i
					break
				}
			}
			if matchIdx < 0 {
				hits = append(hits, engine.Hit{
					File: file, Line: line,
					Label: fmt.Sprintf("</%s> closes nothing (current open: <%s> from line %d)", name, top.name, top.line),
				})
				continue
			}
			// Implicit-close of intermediate tags. Report ONE entry naming
			// the outermost orphan to avoid noise on a single typo that
			// cascades into many false reports.
			orphan := stack[matchIdx+1]
			hits = append(hits, engine.Hit{
				File: file, Line: orphan.line,
				Label: fmt.Sprintf("<%s> on line %d not closed before </%s> on line %d", orphan.name, orphan.line, name, line),
			})
			stack = stack[:matchIdx]
		}
	}
	// Whatever remains on the stack is unclosed.
	for _, t := range stack {
		hits = append(hits, engine.Hit{
			File: file, Line: t.line,
			Label: fmt.Sprintf("<%s> on line %d never closed", t.name, t.line),
		})
	}
	return hits
}
