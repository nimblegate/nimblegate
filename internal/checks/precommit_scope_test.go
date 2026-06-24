// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

// TestNoInnerHTML_PreCommitEmptyChangedFilesPasses
// Scope fix: with the pre-commit trigger and no staged files, the check
// must PASS without scanning the entire project. This matches what the
// real pre-commit hook does (it scans only what git staged).
func TestNoInnerHTML_PreCommitEmptyChangedFilesPasses(t *testing.T) {
	root := t.TempDir()
	// Plant a real violation in the project tree - it should NOT be
	// detected because the pre-commit fallback no longer scans
	// project-wide.
	writeJS(t, root, "src/violation.js", "el.innerHTML = userInput;\n")

	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit with no staged files must not fall back to project-wide; got reason: %s", got.Outcome, got.Reason)
	}
}

// TestNoInnerHTML_CLIEmptyChangedFilesScansProject - the cli trigger
// must STILL fall back to project-wide. This is the deliberate preview
// path that didn't change.
func TestNoInnerHTML_CLIEmptyChangedFilesScansProject(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "src/violation.js", "el.innerHTML = userInput;\n")

	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK - cli trigger must still walk project when ChangedFiles empty", got.Outcome)
	}
}

// TestNoInnerHTML_PreCommitStagedScansThoseFiles - pre-commit with
// staged files: scans those, not the whole project. (Existing behaviour;
// regression guard.)
func TestNoInnerHTML_PreCommitStagedScansThoseFiles(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "src/staged.js", "el.innerHTML = userInput;\n")
	writeJS(t, root, "src/not-staged.js", "el.innerHTML = elseInput;\n")

	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "src/staged.js")},
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	// Reason must reference only the staged file, not the un-staged sibling.
	if !contains(got.Reason, "src/staged.js") {
		t.Errorf("reason missing src/staged.js: %s", got.Reason)
	}
	if contains(got.Reason, "not-staged.js") {
		t.Errorf("not-staged.js leaked into reason - staged-files isolation broken: %s", got.Reason)
	}
}

// TestCrossBranchID_PreCommitEmptyChangedFilesPasses - same fix applied
// to the second file-scanning check.
func TestCrossBranchID_PreCommitEmptyChangedFilesPasses(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"example.com" = "expected-id"
`)
	writeFile(t, root, "site/page.html", `<script data-website-id="wrong-id"></script>`)

	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit with no staged files must not scan project-wide", got.Outcome)
	}
}

// TestCrossBranchID_CLIEmptyChangedFilesStillScans - cli regression guard.
func TestCrossBranchID_CLIEmptyChangedFilesStillScans(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"example.com" = "expected-id"
`)
	writeFile(t, root, "site/page.html", `<script data-website-id="wrong-id"></script>`)

	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("outcome = %s, want WARN - cli trigger must still walk project", got.Outcome)
	}
}
