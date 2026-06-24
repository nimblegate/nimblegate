// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/canonical"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const docTouchesDisableMarker = "appframes:disable documentation/doc-touches-with-code"

// DocTouchesWithCode enforces that a commit which changes a source file
// declared in code-doc-map.toml also changes the mapped documentation file.
//
// The mapping is project-author-controlled. Glob matching uses
// filepath.Match (no `**`; one dir level per glob). Doc paths are
// relative to project root.
//
// Scope contract follows the other file-scanning checks:
//   - empty ChangedFiles + cli → SKIP (rule needs a staged set to evaluate)
//   - empty ChangedFiles + pre-commit → PASS (no staged files = nothing to enforce)
//   - populated ChangedFiles → run the check
//   - no canonical table → SKIP (rule not applicable)
//
// Override: add `appframes:disable documentation/doc-touches-with-code`
// anywhere in the source file's content to opt that file out of the
// coupling. Per-commit suppression via commit message is documented as
// V0.6+ work because pre-commit hooks fire before commit-msg.
func DocTouchesWithCode(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "documentation/doc-touches-with-code",
		Category: frames.CategoryDocumentation,
	}

	tablePath := filepath.Join(ctx.ProjectRoot, ".appframes", "_canonical", "code-doc-map.toml")
	if _, err := os.Stat(tablePath); errors.Is(err, fs.ErrNotExist) {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "no code-doc-map.toml; rule not applicable"
		return res
	}

	tbl, err := canonical.Load(tablePath)
	if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = err.Error()
		return res
	}
	mapping, ok := tbl.Section("code-to-docs")
	if !ok {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "code-doc-map.toml missing [code-to-docs] section"
		return res
	}
	if len(mapping) == 0 {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "code-doc-map.toml [code-to-docs] section is empty"
		return res
	}

	if len(ctx.ChangedFiles) == 0 {
		// pre-commit + empty stage → PASS (file-scan scope contract); cli with no
		// staged set → SKIP because the rule needs change-set context to
		// evaluate "did the doc move with the code?".
		if ctx.Trigger == engine.TriggerCLI {
			res.Outcome = engine.OutcomeSkip
			res.Reason = "no changed files; rule evaluates against staged/working-tree changes"
			return res
		}
		res.Outcome = engine.OutcomePass
		return res
	}

	// Build relative staged set for glob matching + presence checks.
	stagedRel := map[string]bool{}
	for _, abs := range ctx.ChangedFiles {
		rel := relativePath(abs, ctx.ProjectRoot)
		if rel != "" {
			stagedRel[rel] = true
		}
	}

	var hits []string
	checked := 0
	for glob, doc := range mapping {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}
		for src := range stagedRel {
			matched, _ := filepath.Match(glob, src)
			if !matched {
				continue
			}
			checked++
			// Per-file override: read the source file and skip if it
			// contains the disable marker.
			absSrc := filepath.Join(ctx.ProjectRoot, src)
			if data, ok := ReadFileBounded(absSrc, DefaultMaxFileBytes); ok {
				if strings.Contains(string(data), docTouchesDisableMarker) {
					continue
				}
			}
			if !stagedRel[doc] {
				hits = append(hits, fmt.Sprintf("%s (expects doc %s)", src, doc))
			}
			if len(hits) >= 10 {
				break
			}
		}
		if len(hits) >= 10 {
			break
		}
	}

	if checked == 0 {
		// No staged file matched any mapping glob - nothing to enforce.
		res.Outcome = engine.OutcomePass
		return res
	}
	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}

	res.Outcome = engine.OutcomeWarn
	res.Reason = "source file(s) changed without their mapped doc: " + strings.Join(hits, "; ")
	res.Fix = "stage the mapped doc too, or add `// appframes:disable documentation/doc-touches-with-code` to the source if the change is doc-irrelevant"
	return res
}

// relativePath normalizes abs into a project-relative slash-form path.
// Returns "" if abs isn't under root (we can't meaningfully glob against
// outside paths).
func relativePath(abs, root string) string {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "../") {
		return ""
	}
	return rel
}
