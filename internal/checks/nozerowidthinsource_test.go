// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runZeroWidthSourceCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoZeroWidthInSource(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoZeroWidthInSource_ZWSPInIdentifierBlocks(t *testing.T) {
	// Function name "is​admin" looks like "isadmin".
	body := "func is​admin() bool { return true }\n"
	got := runZeroWidthSourceCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "U+200B") {
		t.Errorf("reason should name codepoint: %s", got.Reason)
	}
}

func TestNoZeroWidthInSource_ZWNJBlocks(t *testing.T) {
	body := "let var‌name = 1;\n"
	got := runZeroWidthSourceCheck(t, "app.js", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoZeroWidthInSource_MidFileBOMBlocks(t *testing.T) {
	// U+FEFF in middle of file is suspicious - not the BOM case.
	body := "x = 1\nfoo\uFEFFbar\n"
	got := runZeroWidthSourceCheck(t, "x.py", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoZeroWidthInSource_LeadingBOMIgnored(t *testing.T) {
	// File starts with BOM - that's encoding/no-bom's responsibility,
	// not ours. Should pass here.
	body := "\uFEFFpackage main\n"
	got := runZeroWidthSourceCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("leading BOM should be ignored here; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoZeroWidthInSource_CleanPasses(t *testing.T) {
	body := "func main() { println(\"ok\") }\n"
	got := runZeroWidthSourceCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS", got.Outcome)
	}
}

func TestNoZeroWidthInSource_NonSourceExtensionIgnored(t *testing.T) {
	// Markdown is NOT in scope - encoding/no-zero-width-in-content handles it.
	body := "regular prose with a sneaky ​ inside\n"
	got := runZeroWidthSourceCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("markdown is out of scope here; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoZeroWidthInSource_FileLevelMarkerSuppresses(t *testing.T) {
	body := "// appframes:disable security/no-zero-width-in-source\nfunc x​y() {}\n"
	got := runZeroWidthSourceCheck(t, "main.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s", got.Outcome)
	}
}
