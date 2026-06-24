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

// idPattern matches `data-website-id="<id>"` in HTML/Markdown files.
var idPattern = regexp.MustCompile(`data-website-id="([^"]+)"`)

// CrossBranchIDConsistency scans HTML/MD files for data-website-id values and
// flags any that don't appear in the [ids] section of website-ids.toml.
func CrossBranchIDConsistency(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "documentation/cross-branch-id-consistency",
		Category: frames.CategoryDocumentation,
	}

	tablePath := filepath.Join(ctx.ProjectRoot, ".appframes", "_canonical", "website-ids.toml")
	if _, err := os.Stat(tablePath); errors.Is(err, fs.ErrNotExist) {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "no website-ids.toml; rule not applicable"
		return res
	}

	tbl, err := canonical.Load(tablePath)
	if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = err.Error()
		return res
	}
	ids, ok := tbl.Section("ids")
	if !ok {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "website-ids.toml missing [ids] section"
		return res
	}

	valid := map[string]bool{}
	for _, v := range ids {
		valid[v] = true
	}

	excludes := ctx.ExcludedDirs
	if len(excludes) == 0 {
		excludes = DefaultExcludes()
	}

	var unknownHits []string
	var hitsStruct []engine.Hit
	scanFile := func(path string) {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".html" && ext != ".md" {
			return
		}
		if ShouldSkipPath(ctx, path) {
			return
		}
		data, ok := ReadFileBounded(path, DefaultMaxFileBytes)
		if !ok {
			return
		}
		for _, match := range idPattern.FindAllStringSubmatch(string(data), -1) {
			id := match[1]
			if !valid[id] {
				unknownHits = append(unknownHits, fmt.Sprintf("%s: id %q not in website-ids.toml", path, id))
				hitsStruct = append(hitsStruct, engine.Hit{
					File:  path,
					Line:  0, // regex matches across the whole file; line not tracked
					Label: fmt.Sprintf("unknown data-website-id %q", id),
				})
			}
		}
	}

	// Scan scope:
	//   - non-empty ChangedFiles → scan those files only (staged-files path).
	//   - empty ChangedFiles + cli trigger → project-wide walk.
	//   - empty ChangedFiles + pre-commit/git-wrap → no scan (PASS); matches
	//     what the real hook does when nothing is staged.
	if len(ctx.ChangedFiles) > 0 {
		for _, path := range ctx.ChangedFiles {
			scanFile(path)
		}
	} else if ctx.Trigger == engine.TriggerCLI {
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
			scanFile(path)
			return nil
		})
	}

	if len(unknownHits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeWarn
	// Header + "; "-joined hits so the V0.5 whitelist suppression pass can
	// rebuild the message from surviving Hits when some are filtered.
	res.Reason = "unknown website-ids referenced: " + strings.Join(unknownHits, "; ")
	res.Fix = "ensure data-website-id values match an entry in .appframes/_canonical/website-ids.toml"
	res.Hits = hitsStruct
	return res
}
