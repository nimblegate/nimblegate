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

// metaCharsetRegex matches `<meta charset="utf-8">` or `<meta charset=utf-8>`
// - either quoted or not, any value.
var metaCharsetRegex = regexp.MustCompile(`(?i)<meta\s+charset\s*=\s*["']?[^"'>\s]+`)

// metaViewportRegex matches `<meta name="viewport" content="...">`.
var metaViewportRegex = regexp.MustCompile(`(?i)<meta\s+[^>]*name\s*=\s*["']viewport["'][^>]*>`)

// titleRegex matches a `<title>...</title>` element OR a `<svelte:head>`
// block - the latter strongly suggests dynamic title injection and we
// don't try to follow it statically.
var titleRegex = regexp.MustCompile(`(?is)<title>[^<]*</title>`)
var svelteHeadRegex = regexp.MustCompile(`(?i)<svelte:head\b`)

const htmlRequiredMetaDisableMarker = "appframes:disable web/html-required-meta"
const htmlRequiredMetaDisableLineMarker = "appframes:disable-next-line web/html-required-meta"
const htmlRequiredMetaMaxFileBytes = 1 << 20 // 1 MiB

// HTMLRequiredMeta gates HTML / Svelte / Astro pages on having the three
// indisputably-required meta tags: `<meta charset>`, `<meta viewport>`,
// and `<title>` (or a `<svelte:head>` block that's presumed to add one
// dynamically).
//
// Tier 6 WARN. The check is intentionally lenient on Svelte: presence of
// `<svelte:head>` satisfies the title requirement on the assumption the
// component or a layout is filling it in.
func HTMLRequiredMeta(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "web/html-required-meta",
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

	for _, file := range files {
		if !htmlApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > htmlRequiredMetaMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, htmlRequiredMetaDisableMarker) {
			continue
		}
		body := extractHTMLBody(file, content)

		var missing []string
		if !metaCharsetRegex.MatchString(body) {
			missing = append(missing, "charset")
		}
		if !metaViewportRegex.MatchString(body) {
			missing = append(missing, "viewport")
		}
		if !titleRegex.MatchString(body) && !svelteHeadRegex.MatchString(body) {
			missing = append(missing, "title")
		}
		if len(missing) == 0 {
			continue
		}
		label := "missing required meta: " + strings.Join(missing, ", ")
		hits = append(hits, fmt.Sprintf("%s:0 - %s", file, label))
		hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: label})
		if len(hits) >= hitCap {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "HTML pages missing required meta: " + strings.Join(hits, "; ")
	res.Fix = "every shipping HTML page needs at minimum: `<meta charset=\"utf-8\">`, `<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">`, and a `<title>`. In SvelteKit you can put these in `+layout.svelte` once via `<svelte:head>` and they'll apply to every route."
	return res
}
