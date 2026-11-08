// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
)

// htmlPageInScope reports whether an HTML-ish file can be a page the
// project ships. Test fixtures, test suites, site-generator template
// pieces and hidden tool folders (.superpowers/, .private/) never are.
func htmlPageInScope(ctx engine.CheckContext, path string) bool {
	rel := projectRel(ctx, path)
	if inFixtureDir(rel) || inTestDir(rel) || inHiddenDir(rel) {
		return false
	}
	return !htmlTemplateFragment(ctx, path)
}

// inHiddenDir reports a path under a dot-folder. Static hosts and site
// generators skip these; .well-known is the one served exception.
func inHiddenDir(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		if len(seg) > 1 && seg[0] == '.' && seg != ".." && seg != ".well-known" {
			return true
		}
	}
	return false
}

var (
	htmlDocTagRe     = regexp.MustCompile(`(?i)<(?:html|head)[\s>]`)
	svelteHeadOpenRe = regexp.MustCompile(`(?i)<svelte:head[\s>]`)
	htmlNoIndexRe    = regexp.MustCompile(`(?i)<meta\s[^>]*name\s*=\s*["']?robots["']?[^>]*content\s*=\s*["'][^"']*noindex`)
)

// htmlIsDocument reports whether body is a whole page rather than a
// fragment loaded into one (an HTMX partial, a report snippet, an
// include). Page-level meta checks only apply to documents. A Svelte
// route counts when it owns a <svelte:head>; without one its head comes
// from the layout and app.html.
func htmlIsDocument(path, body string) bool {
	if strings.HasSuffix(strings.ToLower(path), ".svelte") {
		return svelteHeadOpenRe.MatchString(body)
	}
	return htmlDocTagRe.MatchString(body)
}

// htmlNoIndex reports a page that opts out of search indexing.
func htmlNoIndex(body string) bool {
	return htmlNoIndexRe.MatchString(body)
}

// htmlErrorPage reports error pages (404.html, +error.svelte): search
// engines never index them, so canonical/og meta is meaningless there.
func htmlErrorPage(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "404.html", "500.html", "50x.html", "+error.svelte":
		return true
	}
	return false
}

// svelteKitShell reports SvelteKit's app.html, whose head is filled per
// route through %sveltekit.head%.
func svelteKitShell(body string) bool {
	return strings.Contains(body, "%sveltekit.head%")
}

// blankHTMLElements replaces the content of every <tag ...>...</tag> for
// the given tag names with spaces, keeping newlines so line numbers stay
// valid.
func blankHTMLElements(content string, tags ...string) string {
	out := []byte(content)
	lb := []byte(content)
	for i, c := range lb {
		if c >= 'A' && c <= 'Z' {
			lb[i] = c + 'a' - 'A'
		}
	}
	lower := string(lb)
	for _, tag := range tags {
		open, closeTag := "<"+tag, "</"+tag+">"
		for i := 0; ; {
			j := strings.Index(lower[i:], open)
			if j < 0 {
				break
			}
			start := i + j
			after := start + len(open)
			if after < len(lower) && !strings.ContainsRune(" \t\r\n>/", rune(lower[after])) {
				i = after
				continue
			}
			end := strings.Index(lower[after:], closeTag)
			stop := len(content)
			if end >= 0 {
				stop = after + end + len(closeTag)
			}
			for k := start; k < stop; k++ {
				if out[k] != '\n' {
					out[k] = ' '
				}
			}
			i = stop
		}
	}
	return string(out)
}

var htmlPlaceholderAttrRe = regexp.MustCompile(`(?i)\splaceholder\s*=\s*(?:"[^"]*"|'[^']*')`)

// htmlVisibleText blanks the parts of an HTML page a visitor does not read
// as page copy: scripts, styles, code samples and form-field hint text
// (`placeholder="https://www.example.com"` is the attribute's job).
func htmlVisibleText(content string) string {
	s := blankHTMLElements(content, "script", "style", "pre", "code", "kbd", "samp")
	return htmlPlaceholderAttrRe.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat(" ", len(m))
	})
}

var siteMarkdownDirs = map[string]bool{"routes": true, "pages": true, "content": true}

// markdownShips reports whether a Markdown file is site content that gets
// rendered into pages, rather than a repository document (plans, specs,
// notes, READMEs) that only developers read: Hugo content/ and Jekyll
// _posts/ beside their config, and src/content|pages|routes in Astro,
// SvelteKit (mdsvex) and similar frameworks.
func markdownShips(ctx engine.CheckContext, path string) bool {
	if siteContentFile(ctx, path) {
		return true
	}
	segs := strings.Split(filepath.ToSlash(filepath.Dir(projectRel(ctx, path))), "/")
	for i := 0; i+1 < len(segs); i++ {
		if segs[i] == "src" && siteMarkdownDirs[segs[i+1]] {
			return true
		}
	}
	return false
}
