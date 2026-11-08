// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func outOfTreeHits(t *testing.T, ctx engine.CheckContext) map[string]string {
	t.Helper()
	res := NoOutOfTreeReferences(ctx)
	got := map[string]string{}
	for _, h := range res.Hits {
		rel, err := filepath.Rel(ctx.ProjectRoot, h.File)
		if err != nil {
			rel = h.File
		}
		got[filepath.ToSlash(rel)] = h.Label
	}
	if len(got) > 0 && res.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN", res.Outcome)
	}
	if len(got) == 0 && res.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s with no hits, want PASS", res.Outcome)
	}
	return got
}

func symlinkTree(t *testing.T) (root, outside string) {
	t.Helper()
	outside = t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	root = t.TempDir()
	writeSource(t, root, "real.txt", "in tree\n")
	link := func(target, name string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	link(filepath.Join(outside, "secret"), "abs-escape")
	link("../../../../../../../../etc/passwd", "docs/rel-escape")
	link("abs-escape", "chained-escape")
	link("real.txt", "inside-link")
	link("does-not-exist.txt", "dangling-inside")
	link("../real.txt", "docs/inside-up")
	link(outside, "node_modules/linked-pkg") // excluded directory
	return root, outside
}

func TestOutOfTree_symlinksByLstat(t *testing.T) {
	root, outside := symlinkTree(t)
	got := outOfTreeHits(t, engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root})
	want := map[string]string{
		"abs-escape":      "symlink -> " + filepath.Join(outside, "secret") + " points outside the repository",
		"docs/rel-escape": "symlink -> ../../../../../../../../etc/passwd points outside the repository",
		"chained-escape":  "symlink -> abs-escape points outside the repository",
	}
	if len(got) != len(want) {
		t.Errorf("hits = %v, want %v", got, want)
	}
	for f, label := range want {
		if got[f] != label {
			t.Errorf("%s: label %q, want %q", f, got[f], label)
		}
	}
}

// The per-file path a pre-commit hook and the false-alarm sweep take: one
// changed file that is itself a symlink.
func TestOutOfTree_changedFileIsSymlink(t *testing.T) {
	root, _ := symlinkTree(t)
	for name, want := range map[string]int{"abs-escape": 1, "docs/rel-escape": 1, "chained-escape": 1, "inside-link": 0, "dangling-inside": 0, "docs/inside-up": 0, "real.txt": 0} {
		ctx := engine.CheckContext{Trigger: engine.TriggerPreCommit, ProjectRoot: root, ChangedFiles: []string{filepath.Join(root, name)}}
		if got := len(NoOutOfTreeReferences(ctx).Hits); got != want {
			t.Errorf("%s alone: %d hits, want %d", name, got, want)
		}
	}
}

// On a staged tree the outward links are already empty files; the list the
// staging step wrote is the only record, and the frame reports from it.
func TestOutOfTree_sidecarList(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "docs/link", "")
	writeSource(t, root, "node_modules/x", "")
	ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, OutsideLinks: []engine.OutsideLink{
		{Path: "docs/link", Target: "/etc/passwd"},
		{Path: "node_modules/x", Target: "/etc/shadow"},
	}}
	got := outOfTreeHits(t, ctx)
	if len(got) != 1 || got["docs/link"] != "symlink -> /etc/passwd points outside the repository (treated as missing)" {
		t.Errorf("hits = %v", got)
	}
	res := NoOutOfTreeReferences(ctx)
	if !strings.Contains(res.Reason, "docs/link - symlink -> /etc/passwd") {
		t.Errorf("reason = %q", res.Reason)
	}

	// An empty, non-nil list means "staged, nothing neutralised": no walk.
	clean := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, OutsideLinks: []engine.OutsideLink{}}
	if got := outOfTreeHits(t, clean); len(got) != 0 {
		t.Errorf("empty list reported %v", got)
	}
}

func TestOutOfTree_labelIsSanitised(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "a", "")
	long := "/" + strings.Repeat("d", 300)
	ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, OutsideLinks: []engine.OutsideLink{
		{Path: "a", Target: "/etc/\x1b[31mpasswd\nINJECTED‮"},
	}}
	label := outOfTreeHits(t, ctx)["a"]
	if strings.ContainsAny(label, "\x1b\n‮") {
		t.Errorf("control characters survived: %q", label)
	}
	if !strings.Contains(label, "/etc/[31mpasswdINJECTED") {
		t.Errorf("label = %q", label)
	}

	ctx.OutsideLinks = []engine.OutsideLink{{Path: "a", Target: long}}
	label = outOfTreeHits(t, ctx)["a"]
	if !strings.Contains(label, long[:120]+"...") || strings.Contains(label, long[:121]) {
		t.Errorf("long target not capped at 120: %q", label)
	}
}

func TestOutOfTree_imports(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/lib/util.ts", "export const u = 1;\n")
	writeSource(t, root, "src/app.ts", strings.Join([]string{
		"import { u } from './lib/util';",         // in tree, exists
		"import { gone } from './lib/removed';",   // in tree, missing
		"import React from 'react';",              // package
		"import { x } from '../../../outside/x';", // escapes
		"// appframes:disable-next-line security/no-out-of-tree-references",
		"import { y } from '../../../outside/y';", // marked
		"",
	}, "\n"))
	writeSource(t, root, "src/notes.md", "import { z } from '../../../z';\n") // not a source file
	writeSource(t, root, "node_modules/p/index.js", "import a from '../../../../a';\n")
	writeSource(t, root, "src/off.js", "// appframes:disable security/no-out-of-tree-references\nimport a from '../../a';\n")
	res := NoOutOfTreeReferences(engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root})
	if len(res.Hits) != 1 {
		t.Fatalf("hits = %+v, want only the escaping import", res.Hits)
	}
	h := res.Hits[0]
	if !strings.HasSuffix(h.File, "src/app.ts") || h.Line != 4 || h.Label != `import "../../../outside/x" points outside the repository` {
		t.Errorf("hit = %+v", h)
	}
}

// The import side must never be read through a symlink: a source file that is
// a link is judged as a link, not opened.
func TestOutOfTree_sourceSymlinkNotRead(t *testing.T) {
	outside := t.TempDir()
	writeSource(t, outside, "evil.js", "import a from '../../../../../../a';\n")
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "evil.js"), filepath.Join(root, "app.js")); err != nil {
		t.Fatal(err)
	}
	got := outOfTreeHits(t, engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root})
	if len(got) != 1 || !strings.HasPrefix(got["app.js"], "symlink -> ") {
		t.Errorf("hits = %v, want the link only", got)
	}
}

func TestClassifyLocalImport(t *testing.T) {
	root := t.TempDir()
	from := writeSource(t, root, "src/a.js", "")
	writeSource(t, root, "src/b.js", "")
	for imp, want := range map[string]importResolution{
		"./b":          importResolved,
		"./c":          importNotFound,
		"../../x":      importEscapesRoot,
		"..":           importNotFound,
		"$lib/missing": importNotFound,
		"react":        importExternal,
	} {
		if _, got := classifyLocalImport(root, from, imp); got != want {
			t.Errorf("%q: %d, want %d", imp, got, want)
		}
	}
}
