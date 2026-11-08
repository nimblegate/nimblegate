// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func runMixedIndentCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoMixedIndent(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoMixedIndent_PythonTabAfterSpacesBlocks(t *testing.T) {
	body := "def x():\n  \treturn 1\n"
	got := runMixedIndentCheck(t, "app.py", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

// TestNoMixedIndent_GoLiterals: inside a raw string (embedded SQL/JSON) or a
// block comment the whitespace is content; mixed indent on a code line
// after them still fires.
func TestNoMixedIndent_GoLiterals(t *testing.T) {
	body := "package q\n\nconst sql = `\n\tSELECT a,\n\t       b\n\tFROM t`\n\n/*\n\t  indented example\n*/\nvar s = \"`\"\n\nfunc f() {\n\t  return\n}\n"
	got := runMixedIndentCheck(t, "query.go", body)
	if got.Outcome != engine.OutcomeBlock || len(got.Hits) != 1 || got.Hits[0].Line != 14 {
		t.Fatalf("want one BLOCK hit on line 14, got %s %+v", got.Outcome, got.Hits)
	}
}

func TestNoMixedIndent_MakefileSpaceBeforeTabBlocks(t *testing.T) {
	body := "all:\n \techo hi\n"
	got := runMixedIndentCheck(t, "Makefile", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoMixedIndent_PureTabPasses(t *testing.T) {
	body := "all:\n\techo hi\n"
	got := runMixedIndentCheck(t, "Makefile", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("pure-tab Makefile should pass; got %s", got.Outcome)
	}
}

func TestNoMixedIndent_PureSpacesPasses(t *testing.T) {
	body := "def x():\n    return 1\n"
	got := runMixedIndentCheck(t, "app.py", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("pure-space Python should pass; got %s", got.Outcome)
	}
}

func TestNoMixedIndent_MarkdownIgnored(t *testing.T) {
	body := "  \tmixed indent\n"
	got := runMixedIndentCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("markdown is out of scope; got %s", got.Outcome)
	}
}
