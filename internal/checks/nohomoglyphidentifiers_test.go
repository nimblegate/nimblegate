// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runHomoglyphCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoHomoglyphIdentifiers(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoHomoglyphIdentifiers_CyrillicAWarns(t *testing.T) {
	// Cyrillic '\u0430' (U+0430) inside an "admin" identifier.
	body := "func \u0430dmin() {}\n"
	got := runHomoglyphCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
	if !strings.Contains(got.Reason, "U+0430") {
		t.Errorf("reason should name codepoint: %s", got.Reason)
	}
}

func TestNoHomoglyphIdentifiers_GreekRhoWarns(t *testing.T) {
	body := "func \u03c1ush() {}\n"
	got := runHomoglyphCheck(t, "stack.go", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
}

func TestNoHomoglyphIdentifiers_WholeScriptWordsAndCommentsPass(t *testing.T) {
	pass := map[string]string{
		"i18n.go":  "var ru = map[string]string{\"city\": \"\u041c\u043e\u0441\u043a\u0432\u0430\"}\n",
		"hubs.py":  "\"A\": (\"Rows whose depth \u03c1 is below \u03b1 ~ 0.171 and 2k + 1 < \u03b1n\"),\n",
		"stats.py": "\u03b1 = 0.05\n",
		"notes.go": "// the Cyrillic \u0430dmin trick is what this guards against\n",
	}
	for rel, body := range pass {
		if got := runHomoglyphCheck(t, rel, body); got.Outcome != engine.OutcomePass {
			t.Errorf("%s: want PASS, got %s %s", rel, got.Outcome, got.Reason)
		}
	}
	if got := runHomoglyphCheck(t, "login.js", "export function l\u043egin(user) { return user; }\n"); got.Outcome != engine.OutcomeWarn {
		t.Errorf("mixed-script identifier should warn; got %s", got.Outcome)
	}
}

func TestNoHomoglyphIdentifiers_CleanASCIIPasses(t *testing.T) {
	body := "func admin() { return }\n"
	got := runHomoglyphCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
}

func TestNoHomoglyphIdentifiers_NonSourceExtensionIgnored(t *testing.T) {
	// Markdown is out of scope.
	body := "Cyrillic \u0430 everywhere here.\n"
	got := runHomoglyphCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("markdown is out of scope; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoHomoglyphIdentifiers_FileLevelMarkerSuppresses(t *testing.T) {
	body := "// appframes:disable security/no-homoglyph-identifiers\nfunc \u0430dmin() {}\n"
	got := runHomoglyphCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s", got.Outcome)
	}
}
