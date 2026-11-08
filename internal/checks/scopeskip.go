// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
)

// projectRel returns path relative to the project root, slash-separated.
func projectRel(ctx engine.CheckContext, path string) string {
	if ctx.ProjectRoot != "" {
		if r, err := filepath.Rel(ctx.ProjectRoot, path); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return filepath.ToSlash(path)
}

// inFixtureDir reports files under a test-fixture directory. Fixtures hold
// deliberately bad input for a test suite; they never ship.
func inFixtureDir(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		switch seg {
		case "testdata", "fixtures", "__fixtures__", "__snapshots__":
			return true
		}
	}
	return false
}

// inVendoredDir reports third-party code checked into a project. Default
// excludes already drop node_modules and virtualenvs; these are the
// remaining conventional homes of code the project does not author.
func inVendoredDir(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		switch seg {
		case "vendor", "third_party", "third-party", "bower_components":
			return true
		}
	}
	return false
}

// isMarkdownPath reports Markdown sources.
func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdx":
		return true
	}
	return false
}

// markdownProse returns the lines of a Markdown document with fenced code
// blocks blanked and inline code spans removed, keeping line numbers
// aligned. Code in a document is an example, not the document's own text.
func markdownProse(content string) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, len(lines))
	inFence, marker := false, ""
	for i, line := range lines {
		if inFence {
			if isClosingFence(line, marker) {
				inFence = false
			}
			continue
		}
		if m := fenceOpenRegex.FindStringSubmatch(line); m != nil {
			inFence, marker = true, m[1]
			continue
		}
		out[i] = stripInlineCode(line)
	}
	return out
}
