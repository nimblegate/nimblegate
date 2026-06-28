// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/canonical"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// linkPattern captures inline markdown links and images: [text](url) and ![alt](url).
// Group 1 is the link target (url / path).
//
// Conservative: only matches when the brackets and parens are on the
// same line; multi-line links are rare and easier to leave to a future
// expansion than to risk false positives.
var linkPattern = regexp.MustCompile(`!?\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// externalSchemeRegex matches any URL scheme that's NOT a project-relative
// path. We skip these - external links aren't validated by this frame.
var externalSchemeRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)

// fenceOpenRegex matches a fenced-code-block delimiter line: 3+ backticks or
// tildes, optionally indented, optionally followed by an info string.
var fenceOpenRegex = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

// isClosingFence reports whether line closes a fence opened with `marker`
// (same character, length >= the opener, nothing else but whitespace).
func isClosingFence(line, marker string) bool {
	t := strings.TrimSpace(line)
	if len(t) < len(marker) {
		return false
	}
	ch := marker[0]
	for i := 0; i < len(t); i++ {
		if t[i] != ch {
			return false
		}
	}
	return true
}

// stripInlineCode removes inline-code spans (text between matching backtick
// runs) from a single line, so markdown links inside code - `[x](y)` - are
// not parsed as real links. Per CommonMark, a backtick run of length n opens
// a span that closes at the next run of exactly length n; an unmatched run is
// literal text. Matched spans are replaced with a space to preserve token
// separation.
func stripInlineCode(line string) string {
	var b strings.Builder
	i, n := 0, len(line)
	for i < n {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		j := i
		for j < n && line[j] == '`' {
			j++
		}
		runLen := j - i
		closeStart := -1
		k := j
		for k < n {
			if line[k] != '`' {
				k++
				continue
			}
			m := k
			for m < n && line[m] == '`' {
				m++
			}
			if m-k == runLen {
				closeStart = k
				k = m
				break
			}
			k = m
		}
		if closeStart < 0 {
			// unmatched run - literal backticks, not a code span
			b.WriteString(line[i:j])
			i = j
			continue
		}
		b.WriteByte(' ')
		i = k
	}
	return b.String()
}

const markdownLinkDisableMarker = "appframes:disable documentation/markdown-link-check-internal"

// MarkdownLinkCheckInternal scans markdown files for inline relative links
// and verifies their targets exist on disk. External (http/https/mailto/...)
// links and pure-anchor (#heading) links are not checked.
//
// An optional canonical table at
// `.appframes/_canonical/markdown-link-ignore.toml` declares path prefixes
// the frame should skip (typically cross-branch references in
// orphan-branch monorepos).
//
// Scope contract follows file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + empty ChangedFiles → PASS (no scan)
//   - non-empty ChangedFiles → scan those files only
//   - noise-dir exclusion applies uniformly
func MarkdownLinkCheckInternal(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "documentation/markdown-link-check-internal",
		Category: frames.CategoryDocumentation,
	}
	ignored := loadIgnoredPrefixes(ctx.ProjectRoot)

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
			if strings.EqualFold(filepath.Ext(path), ".md") {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 20
files:
	for _, file := range files {
		if !strings.EqualFold(filepath.Ext(file), ".md") {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if strings.Contains(content, markdownLinkDisableMarker) {
			continue
		}

		dir := filepath.Dir(file)
		inFence := false
		fenceMarker := ""
		for lineNum, line := range strings.Split(content, "\n") {
			// Fenced code blocks: skip every line inside (and the delimiters).
			if inFence {
				if isClosingFence(line, fenceMarker) {
					inFence = false
				}
				continue
			}
			if m := fenceOpenRegex.FindStringSubmatch(line); m != nil {
				inFence = true
				fenceMarker = m[1]
				continue
			}
			// Inline code spans: links inside backticks aren't real links.
			scanLine := stripInlineCode(line)
			for _, match := range linkPattern.FindAllStringSubmatch(scanLine, -1) {
				link := strings.TrimSpace(match[1])
				if link == "" {
					continue
				}
				// External scheme - skip.
				if externalSchemeRegex.MatchString(link) {
					continue
				}
				// Pure anchor - skip.
				if strings.HasPrefix(link, "#") {
					continue
				}
				// Strip query string + anchor before resolving.
				target := link
				if i := strings.IndexAny(target, "?#"); i >= 0 {
					target = target[:i]
				}
				if target == "" {
					continue
				}
				resolved, ok := resolveAndRelativize(target, dir, ctx.ProjectRoot)
				if !ok {
					continue // outside project; treat as opaque, don't validate
				}
				if hasIgnoredPrefix(resolved, ignored) {
					continue
				}
				abs := filepath.Join(ctx.ProjectRoot, resolved)
				if _, statErr := os.Stat(abs); statErr != nil {
					hits = append(hits, fmt.Sprintf("%s:%d → %s", file, lineNum+1, link))
					hitsStruct = append(hitsStruct, engine.Hit{
						File:  file,
						Line:  lineNum + 1,
						Label: "broken link → " + link,
					})
					if len(hits) >= hitCap {
						break files
					}
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeWarn
	res.Reason = "broken internal markdown links: " + strings.Join(hits, "; ")
	res.Fix = "update the link or add the path's prefix to .appframes/_canonical/markdown-link-ignore.toml"
	res.Hits = hitsStruct
	return res
}

// loadIgnoredPrefixes reads the optional ignore-prefix canonical table.
// Returns an empty slice if the table is missing or malformed (the
// frame still runs, just without suppression hints).
func loadIgnoredPrefixes(projectRoot string) []string {
	path := filepath.Join(projectRoot, ".appframes", "_canonical", "markdown-link-ignore.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	tbl, err := canonical.Load(path)
	if err != nil {
		return nil
	}
	section, ok := tbl.Section("ignored-prefixes")
	if !ok {
		return nil
	}
	out := make([]string, 0, len(section))
	for k := range section {
		out = append(out, k)
	}
	return out
}

// resolveAndRelativize returns the link's resolved path as a
// project-relative slash-form path. Returns (path, true) if the path is
// inside the project; (_, false) if it escapes the project root (we
// don't validate those).
func resolveAndRelativize(link, sourceDir, projectRoot string) (string, bool) {
	// Treat absolute-looking paths (`/foo/bar.md`) as project-root-relative.
	// This is the markdown convention.
	var abs string
	if strings.HasPrefix(link, "/") {
		abs = filepath.Join(projectRoot, link)
	} else {
		abs = filepath.Join(sourceDir, link)
	}
	rel, err := filepath.Rel(projectRoot, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") || rel == ".." {
		return "", false
	}
	return rel, true
}

// hasIgnoredPrefix returns true if path begins with any of the prefixes.
func hasIgnoredPrefix(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}
