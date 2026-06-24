// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"strings"
)

// htmlApplicableFile returns true when the file is one of the web-page
// formats the convention/html-* + security/no-mixed-content-urls frames
// scan. The list is intentionally tight - we only look at files that
// produce shipping HTML at render time.
func htmlApplicableFile(path string) bool {
	base := filepath.Base(path)
	if base == "+page.svelte" || base == "+layout.svelte" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".html", ".htm", ".astro":
		return true
	}
	return false
}

// htmlApplicableContentOrMarkdown is a broader filter that also includes
// markdown - used by html-placeholder-content (lorem ipsum / TODO etc.
// can show up in any narrative content).
func htmlApplicableContentOrMarkdown(path string) bool {
	if htmlApplicableFile(path) {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md", ".mdx", ".mdoc":
		return true
	}
	return false
}

// extractHTMLBody returns the HTML portion of a file's content, with
// non-HTML wrappers stripped:
//
//   - Svelte: the entire <script>...</script> block is removed. The
//     remaining body (template markup + <svelte:head>) is what gets
//     parsed / scanned. Style blocks are kept because they can contain
//     URLs the mixed-content check cares about.
//   - Astro: the leading `---` frontmatter (one block at the start of
//     the file) is removed.
//   - Plain HTML: returned as-is.
//
// The result is "what the user would see if they viewed source." It is
// NOT a substitute for a full HTML5 parser; callers that need real
// parsing should pass the result to html.NewTokenizer.
func extractHTMLBody(path, content string) string {
	ext := strings.ToLower(filepath.Ext(path))
	base := filepath.Base(path)
	if ext == ".svelte" || strings.HasSuffix(base, ".svelte") {
		return stripSvelteScript(content)
	}
	if ext == ".astro" {
		return stripAstroFrontmatter(content)
	}
	return content
}

// stripSvelteScript removes <script>...</script> blocks (case-insensitive
// open tag, including `<script lang="ts">` etc.). Multiple script blocks
// are supported. The replacement preserves line count to keep line:col
// reports stable.
func stripSvelteScript(content string) string {
	var b strings.Builder
	i := 0
	for i < len(content) {
		open := indexCaseInsensitive(content[i:], "<script")
		if open < 0 {
			b.WriteString(content[i:])
			break
		}
		b.WriteString(content[i : i+open])
		// Find the closing `</script>`.
		closeRel := indexCaseInsensitive(content[i+open:], "</script>")
		if closeRel < 0 {
			// Unterminated - strip the rest to keep parser sane.
			break
		}
		// Replace the block with same number of newlines so line numbers stay
		// stable for downstream reports.
		block := content[i+open : i+open+closeRel+len("</script>")]
		for _, r := range block {
			if r == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		}
		i = i + open + closeRel + len("</script>")
	}
	return b.String()
}

// stripAstroFrontmatter removes the leading `---\n...\n---\n` block.
// Astro frontmatter is JS/TS code, not HTML, so it should not feed any
// HTML-shaped check. Returns content unchanged if no frontmatter.
func stripAstroFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return content
	}
	// Find the closing `---` on a line of its own.
	rest := content[4:]
	if strings.HasPrefix(content, "---\r\n") {
		rest = content[5:]
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		// Malformed - return as-is, let downstream parsers cope.
		return content
	}
	// Compute how many newlines we're consuming so we can preserve count.
	consumed := content[:len(content)-len(rest)+end+len("\n---")]
	// Trim past the trailing newline after the closing ---.
	tailStart := len(consumed)
	if tailStart < len(content) && content[tailStart] == '\n' {
		tailStart++
	} else if tailStart+1 < len(content) && content[tailStart] == '\r' && content[tailStart+1] == '\n' {
		tailStart += 2
	}
	// Preserve newline count: replace the consumed block with the same
	// number of newlines.
	var b strings.Builder
	for _, r := range content[:tailStart] {
		if r == '\n' {
			b.WriteByte('\n')
		}
	}
	b.WriteString(content[tailStart:])
	return b.String()
}

// indexCaseInsensitive returns the lowest index of needle in s, comparing
// case-insensitively. Returns -1 if not found.
func indexCaseInsensitive(s, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	lower := strings.ToLower(s)
	return strings.Index(lower, strings.ToLower(needle))
}
