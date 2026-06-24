// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

// TestNoInnerHTML_StagedNoiseFileNotScanned
// regression: a violation staged inside node_modules/ used to be scanned
// because the ChangedFiles iteration ignored the exclusion list. Now it
// must be skipped just like during a project-wide walk.
func TestNoInnerHTML_StagedNoiseFileNotScanned(t *testing.T) {
	root := t.TempDir()
	// Real violation in src/ - should be detected.
	writeJS(t, root, "src/legit.js", "el.innerHTML = userInput;\n")
	// Violation in node_modules/ - should be SKIPPED even when staged.
	writeJS(t, root, "node_modules/dep/vendored.js", "el.innerHTML = vendorEval;\n")

	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:     engine.TriggerPreCommit,
		ProjectRoot: root,
		ChangedFiles: []string{
			filepath.Join(root, "src/legit.js"),
			filepath.Join(root, "node_modules/dep/vendored.js"),
		},
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK (src/legit.js should fire)", got.Outcome)
	}
	// The reason must mention src/legit.js but NOT vendored.js.
	if !contains(got.Reason, "src/legit.js") {
		t.Errorf("reason missing src/legit.js: %s", got.Reason)
	}
	if contains(got.Reason, "vendored.js") || contains(got.Reason, "node_modules") {
		t.Errorf("reason should not mention vendored / node_modules; got: %s", got.Reason)
	}
}

// TestCrossBranchID_ChangedFilesPathRespectsExclusion
// fix applied to the second file-scanning check.
func TestCrossBranchID_ChangedFilesPathRespectsExclusion(t *testing.T) {
	root := t.TempDir()
	writeWebsiteIDsTable(t, root, `
[ids]
"example.com" = "good"
`)
	writeFile(t, root, "src/page.html", `<script data-website-id="bad-id"></script>`)
	writeFile(t, root, "node_modules/badge/index.html", `<script data-website-id="bundled-bad"></script>`)

	// Pass both via ChangedFiles; the noise-dir file must be skipped.
	got := CrossBranchIDConsistency(engine.CheckContext{
		Trigger:     engine.TriggerPreCommit,
		ProjectRoot: root,
		ChangedFiles: []string{
			filepath.Join(root, "src/page.html"),
			filepath.Join(root, "node_modules/badge/index.html"),
		},
		ExcludedDirs: DefaultExcludes(),
	})

	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN (src/page.html should fire)", got.Outcome)
	}
	if !contains(got.Reason, "src/page.html") {
		t.Errorf("reason missing src/page.html: %s", got.Reason)
	}
	if contains(got.Reason, "node_modules") || contains(got.Reason, "bundled-bad") {
		t.Errorf("noise-dir id leaked into reason: %s", got.Reason)
	}
}

// TestNoInnerHTML_ProjectConfigCustomExclude - when the project config
// overrides ExcludedDirs (e.g. adds "vendor" to the list), the new entry
// is honored.
func TestNoInnerHTML_ProjectConfigCustomExclude(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "vendor/legacy/inline.js", "el.innerHTML = whatever;\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: append(DefaultExcludes(), "vendor"),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (vendor/ should be excluded by custom list)", got.Outcome)
	}
}

// TestNoInnerHTML_EmptyExcludedDirsFallsBackToDefaults - protects against
// callers forgetting to populate ExcludedDirs.
func TestNoInnerHTML_EmptyExcludedDirsFallsBackToDefaults(t *testing.T) {
	root := t.TempDir()
	writeJS(t, root, "node_modules/x/y.js", "el.innerHTML = z;\n")
	got := NoInnerHTMLUserInput(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: nil, // not populated
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded by default fallback)", got.Outcome)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
