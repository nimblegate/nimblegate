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

// TestNoMixedIndent_TabsThenSpaces: in a make recipe or C source, a tab
// followed by alignment spaces is valid; a space before a tab still fires.
func TestNoMixedIndent_TabsThenSpaces(t *testing.T) {
	for rel, body := range map[string]string{
		"Makefile":   "test:\n\tfor d in a b; do \\\n\t  go test ./$$d; \\\n\tdone\n",
		"src/io.h":   "struct s {\n\tint a;\t/* x */\n\t    int b;\n};\n",
		"rules.mk":   "x:\n\t  echo hi\n",
		"src/main.c": "int main() {\n\t  return 0;\n}\n",
	} {
		if got := runMixedIndentCheck(t, rel, body); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: outcome = %s, want PASS; reason=%s", rel, got.Outcome, got.Reason)
		}
	}
	if got := runMixedIndentCheck(t, "src/main.c", "int main() {\n  \treturn 0;\n}\n"); got.Outcome != engine.OutcomeBlock {
		t.Errorf("space before tab in C: outcome = %s, want BLOCK", got.Outcome)
	}
	if got := runMixedIndentCheck(t, "app.py", "def f():\n\t  return 1\n"); got.Outcome != engine.OutcomeBlock {
		t.Errorf("tab then space in Python: outcome = %s, want BLOCK", got.Outcome)
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
