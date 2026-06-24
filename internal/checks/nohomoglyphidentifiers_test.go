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
	// Cyrillic 'а' (U+0430) inside an "admin" identifier.
	body := "func аdmin() {}\n"
	got := runHomoglyphCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
	}
	if !strings.Contains(got.Reason, "U+0430") {
		t.Errorf("reason should name codepoint: %s", got.Reason)
	}
}

func TestNoHomoglyphIdentifiers_GreekRhoWarns(t *testing.T) {
	body := "func ρush() {}\n"
	got := runHomoglyphCheck(t, "stack.go", body)
	if got.Outcome != engine.OutcomeWarn {
		t.Fatalf("outcome = %s, want WARN", got.Outcome)
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
	body := "Cyrillic а everywhere here.\n"
	got := runHomoglyphCheck(t, "README.md", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("markdown is out of scope; got %s reason=%s", got.Outcome, got.Reason)
	}
}

func TestNoHomoglyphIdentifiers_FileLevelMarkerSuppresses(t *testing.T) {
	body := "// appframes:disable security/no-homoglyph-identifiers\nfunc аdmin() {}\n"
	got := runHomoglyphCheck(t, "auth.go", body)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("file-level marker should suppress; outcome=%s", got.Outcome)
	}
}
