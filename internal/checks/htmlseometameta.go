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

// seoMetaPattern is one detection pattern + the human-facing tag name.
type seoMetaPattern struct {
	Name    string
	Pattern *regexp.Regexp
}

var seoMetaPatterns = []seoMetaPattern{
	{
		Name:    "meta description",
		Pattern: regexp.MustCompile(`(?i)<meta\s+[^>]*name\s*=\s*["']description["'][^>]*>`),
	},
	{
		Name:    "link canonical",
		Pattern: regexp.MustCompile(`(?i)<link\s+[^>]*rel\s*=\s*["']canonical["'][^>]*>`),
	},
	{
		Name:    "og:title",
		Pattern: regexp.MustCompile(`(?i)<meta\s+[^>]*property\s*=\s*["']og:title["'][^>]*>`),
	},
	{
		Name:    "og:description",
		Pattern: regexp.MustCompile(`(?i)<meta\s+[^>]*property\s*=\s*["']og:description["'][^>]*>`),
	},
	{
		Name:    "og:image",
		Pattern: regexp.MustCompile(`(?i)<meta\s+[^>]*property\s*=\s*["']og:image["'][^>]*>`),
	},
}

const htmlSEOMetaDisableMarker = "appframes:disable web/html-seo-meta"
const htmlSEOMetaMaxFileBytes = 1 << 20 // 1 MiB

// HTMLSEOMeta gates HTML pages on the standard SEO + social-share meta
// set. None of these are technically required by the spec, but every
// one missing degrades how the page appears in search results, link
// previews, and social-card unfurls.
//
// Tier 6 WARN.
func HTMLSEOMeta(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "web/html-seo-meta",
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
		if err != nil || info.Size() > htmlSEOMetaMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, htmlSEOMetaDisableMarker) {
			continue
		}
		body := extractHTMLBody(file, content)
		// If the page also has <svelte:head>, the SEO meta might be added
		// dynamically by a child layout - skip strict checks here to avoid
		// false positives. The user can still enable for top-level
		// +page.svelte / layout files that explicitly own SEO.
		// (Conservative default. Easy to turn off via project override.)

		var missing []string
		for _, p := range seoMetaPatterns {
			if !p.Pattern.MatchString(body) {
				missing = append(missing, p.Name)
			}
		}
		if len(missing) == 0 {
			continue
		}
		label := "missing SEO/social meta: " + strings.Join(missing, ", ")
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
	res.Reason = "HTML pages missing SEO/social meta: " + strings.Join(hits, "; ")
	res.Fix = "shipping HTML pages benefit from: <meta name=\"description\">, <link rel=\"canonical\">, and the og:* trio (title, description, image). Search-result snippets and social-card unfurls depend on these. For SvelteKit / Astro, put them in your root layout's <svelte:head> / <head> and template them per route."
	return res
}
