// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"nimblegate/internal/checks"
	"nimblegate/internal/engine"
)

// TestFrameSweep runs frames file by file over whole directory trees, so every
// hit is reported rather than the first ten per run. Opt-in: it only runs when
// NBG_SWEEP_ROOTS is set. It exists to measure false alarms on real code before
// a frame ships or changes; graded corpora and unit tests cover only the cases
// someone thought of.
//
//	NBG_SWEEP_ROOTS   comma-separated directories to walk
//	NBG_SWEEP_FRAMES  comma-separated frame IDs, "security" for every security/* frame,
//	                  or "all" for every frame except git/* (those judge push metadata)
//	NBG_SWEEP_OUT     output file (TSV: frame, root, file:line, label); default stdout
//
// Directories are skipped exactly as the gate skips them by default, so
// generated output the gate does scan (public/, a docs site) is included.
func TestFrameSweep(t *testing.T) {
	roots := os.Getenv("NBG_SWEEP_ROOTS")
	if roots == "" {
		t.Skip("set NBG_SWEEP_ROOTS to run the sweep")
	}
	all := BuiltinCheckFuncs()
	var ids []string
	for _, id := range strings.Split(os.Getenv("NBG_SWEEP_FRAMES"), ",") {
		id = strings.TrimSpace(id)
		switch {
		case id == "security" || id == "all":
			for k := range all {
				// git/* frames judge push metadata (refs, flags), not file
				// content, so a per-file sweep says nothing about them.
				if (id == "all" && !strings.HasPrefix(k, "git/")) || strings.HasPrefix(k, "security/") {
					ids = append(ids, k)
				}
			}
		case id != "":
			if _, ok := all[id]; !ok {
				t.Fatalf("unknown frame %q", id)
			}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		t.Fatal("set NBG_SWEEP_FRAMES (frame IDs, \"security\" or \"all\")")
	}
	sort.Strings(ids)

	out := os.Stdout
	if p := os.Getenv("NBG_SWEEP_OUT"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		out = f
	}
	// Default: the directories the gate itself skips (checks.DefaultExcludes),
	// so the sweep measures what a push would see. NBG_SWEEP_RAW=1 walks
	// everything except .git.
	excl := checks.DefaultExcludes()
	if os.Getenv("NBG_SWEEP_RAW") == "1" {
		excl = []string{".git"}
	}
	skip := map[string]bool{}
	for _, d := range excl {
		skip[d] = true
	}
	for _, root := range strings.Split(roots, ",") {
		files := 0
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skip[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			link := d.Type()&fs.ModeSymlink != 0
			if !d.Type().IsRegular() && !link {
				return nil
			}
			files++
			ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ChangedFiles: []string{p}, ExcludedDirs: excl}
			for _, id := range ids {
				// Symlinks go only to the frame that judges links. Content
				// frames would read through them, and a link to a FIFO or
				// device would hang the sweep.
				if link && id != "security/no-out-of-tree-references" {
					continue
				}
				for _, h := range all[id](ctx).Hits {
					fmt.Fprintf(out, "%s\t%s\t%s:%d\t%s\n", id, root, strings.TrimPrefix(h.File, root+"/"), h.Line, h.Label)
				}
			}
			return nil
		})
		fmt.Fprintf(out, "#files\t%s\t%d\n", root, files)
	}
}
