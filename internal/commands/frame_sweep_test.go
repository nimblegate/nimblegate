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

	"nimblegate/internal/engine"
)

// TestFrameSweep runs frames file by file over whole directory trees, so every
// hit is reported rather than the first ten per run. Opt-in: it only runs when
// NBG_SWEEP_ROOTS is set. It exists to measure false alarms on real code before
// a frame ships or changes; graded corpora and unit tests cover only the cases
// someone thought of.
//
//	NBG_SWEEP_ROOTS   comma-separated directories to walk
//	NBG_SWEEP_FRAMES  comma-separated frame IDs, or "security" for every security/* frame
//	NBG_SWEEP_OUT     output file (TSV: frame, root, file:line, label); default stdout
//
// Only .git and node_modules are skipped: generated output (public/, dist/,
// build/) is scanned because the gate scans whatever a push carries.
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
		case id == "security":
			for k := range all {
				if strings.HasPrefix(k, "security/") {
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
		t.Fatal("set NBG_SWEEP_FRAMES (frame IDs, or \"security\")")
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
	skip := map[string]bool{".git": true, "node_modules": true}
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
			if !d.Type().IsRegular() {
				return nil
			}
			files++
			ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ChangedFiles: []string{p}, ExcludedDirs: []string{".git"}}
			for _, id := range ids {
				for _, h := range all[id](ctx).Hits {
					fmt.Fprintf(out, "%s\t%s\t%s:%d\t%s\n", id, root, strings.TrimPrefix(h.File, root+"/"), h.Line, h.Label)
				}
			}
			return nil
		})
		fmt.Fprintf(out, "#files\t%s\t%d\n", root, files)
	}
}
