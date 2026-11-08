// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const (
	outOfTreeID                = "security/no-out-of-tree-references"
	outOfTreeDisableMarker     = "appframes:disable security/no-out-of-tree-references"
	outOfTreeDisableLineMarker = "appframes:disable-next-line security/no-out-of-tree-references"
	outOfTreeTargetMax         = 120
)

// outOfTreeImportExts are the files whose `import ... from '...'` lines are
// read, with the same import syntax top-of-page-import-safety follows.
var outOfTreeImportExts = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true, ".jsx": true,
	".ts": true, ".mts": true, ".cts": true, ".tsx": true,
	".svelte": true, ".vue": true,
}

// NoOutOfTreeReferences warns about references that leave the repository: a
// symlink whose target resolves outside the project root, and a relative
// import that climbs above it. Either is broken code on any other checkout,
// or an attempt to reach files the repository does not hold.
//
// Symlinks come from ctx.OutsideLinks when it is set (a staged tree whose
// outward links were already replaced); otherwise the tree is walked and each
// link is resolved without ever opening its target.
func NoOutOfTreeReferences(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  outOfTreeID,
		Category: frames.CategorySecurity,
	}
	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10
	add := func(file string, line int, label string) bool {
		loc := relPath(ctx, file)
		if line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, line)
		}
		hits = append(hits, loc+" - "+label)
		hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: line, Label: label})
		return len(hits) >= hitCap
	}

	links, sources := outOfTreeCandidates(ctx)

	if ctx.OutsideLinks != nil {
		for _, l := range ctx.OutsideLinks {
			abs := filepath.Join(ctx.ProjectRoot, filepath.FromSlash(l.Path))
			if ShouldSkipPath(ctx, abs) {
				continue
			}
			if add(abs, 0, "symlink -> "+sanitizeReference(l.Target)+" points outside the repository (treated as missing)") {
				return outOfTreeResult(res, hits, hitsStruct)
			}
		}
	} else {
		roots := outOfTreeRoots(ctx.ProjectRoot)
		for _, p := range links {
			target, err := os.Readlink(p)
			if err != nil {
				continue
			}
			if withinAny(roots, resolveSymlinkTarget(p, target)) {
				continue
			}
			if add(p, 0, "symlink -> "+sanitizeReference(target)+" points outside the repository") {
				return outOfTreeResult(res, hits, hitsStruct)
			}
		}
	}

	for _, file := range sources {
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if !strings.Contains(content, "import") || fileDisabledByMarker(content, outOfTreeDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			m := topOfPageImportRegex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if i > 0 && lineCarriesMarker(lines[i-1], outOfTreeDisableLineMarker) {
				continue
			}
			if _, r := classifyLocalImport(ctx.ProjectRoot, file, m[1]); r != importEscapesRoot {
				continue
			}
			if add(file, i+1, fmt.Sprintf("import %q points outside the repository", sanitizeReference(m[1]))) {
				return outOfTreeResult(res, hits, hitsStruct)
			}
		}
	}
	return outOfTreeResult(res, hits, hitsStruct)
}

func outOfTreeResult(res engine.CheckResult, hits []string, hitsStruct []engine.Hit) engine.CheckResult {
	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "references outside the repository: " + strings.Join(hits, "; ")
	res.Fix = "point the symlink or import at a path inside the repository, or vendor the file in; an absolute or climbing path does not exist on another checkout"
	return res
}

// outOfTreeCandidates splits the files in scope into symlinks and import
// sources. Lstat throughout: a symlink is judged by where it points and never
// read through, and a source reached only through a link is not opened.
func outOfTreeCandidates(ctx engine.CheckContext) (links, sources []string) {
	consider := func(p string, mode fs.FileMode) {
		switch {
		case mode&fs.ModeSymlink != 0:
			links = append(links, p)
		case mode.IsRegular() && outOfTreeImportExts[strings.ToLower(filepath.Ext(p))]:
			sources = append(sources, p)
		}
	}
	if len(ctx.ChangedFiles) > 0 {
		for _, p := range ctx.ChangedFiles {
			if ShouldSkipPath(ctx, p) {
				continue
			}
			if fi, err := os.Lstat(p); err == nil {
				consider(p, fi.Mode())
			}
		}
		return links, sources
	}
	if ctx.Trigger != engine.TriggerCLI {
		return nil, nil
	}
	_ = filepath.WalkDir(ctx.ProjectRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != ctx.ProjectRoot && ShouldSkipPath(ctx, p) {
				return filepath.SkipDir
			}
			return nil
		}
		if ShouldSkipPath(ctx, p) {
			return nil
		}
		consider(p, d.Type())
		return nil
	})
	return links, sources
}

// outOfTreeRoots is the project root as given and as resolved, so a root
// reached through a symlinked parent (a /tmp link, a mounted home) still
// contains its own links.
func outOfTreeRoots(root string) []string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	roots := []string{abs}
	if real, err := filepath.EvalSymlinks(abs); err == nil && real != abs {
		roots = append(roots, real)
	}
	return roots
}

// resolveSymlinkTarget mirrors the gateway's staging rule: follow the whole
// chain when it resolves, otherwise resolve the first hop lexically, so a
// dangling link inside the tree stays inside while one aimed outward is
// still caught.
func resolveSymlinkTarget(link, target string) string {
	abs, err := filepath.Abs(link)
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(abs), target))
}

func withinAny(roots []string, p string) bool {
	if p == "" {
		return false
	}
	for _, r := range roots {
		if insideRoot(r, p) {
			return true
		}
	}
	return false
}

// sanitizeReference makes a link target or import path from scanned content
// safe to print: control and format characters (newlines, ANSI escapes, bidi
// overrides) are dropped, and the result is capped.
func sanitizeReference(s string) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		if n == outOfTreeTargetMax {
			b.WriteString("...")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}
