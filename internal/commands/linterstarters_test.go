// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"regexp"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// TestLinterStarters_allCompile is the load-bearing test: every shipped
// starter regex MUST compile under Go's regexp (RE2). A starter with a
// broken regex would silently fail when the operator picks it and tries
// to save - much worse UX than catching it at build time.
func TestLinterStarters_allCompile(t *testing.T) {
	for _, s := range LinterStarters {
		_, err := regexp.Compile(s.Regex)
		if err != nil {
			t.Errorf("starter %q (%s): regex doesn't compile: %v", s.ID, s.Label, err)
		}
	}
}

// TestLinterStarters_requiredFields catches accidentally-shipped entries
// missing a required field (ID/Label/Name/Regex/Severity). Description +
// Patterns are operator-tunable so empty is allowed.
func TestLinterStarters_requiredFields(t *testing.T) {
	for _, s := range LinterStarters {
		if s.ID == "" {
			t.Errorf("starter has empty ID: %+v", s)
		}
		if s.Label == "" {
			t.Errorf("starter %q has empty Label", s.ID)
		}
		if s.Name == "" {
			t.Errorf("starter %q has empty Name", s.ID)
		}
		if s.Regex == "" {
			t.Errorf("starter %q has empty Regex", s.ID)
		}
		if s.Severity != "WARN" && s.Severity != "INFO" && s.Severity != "BLOCK" {
			t.Errorf("starter %q has invalid Severity %q (must be WARN/INFO/BLOCK)", s.ID, s.Severity)
		}
	}
}

// TestLinterStarters_uniqueIDs catches accidental ID collisions which
// would break the dropdown's onchange handler (data-* attributes wouldn't
// disambiguate the picked starter).
func TestLinterStarters_uniqueIDs(t *testing.T) {
	seen := make(map[string]string)
	for _, s := range LinterStarters {
		if other, dup := seen[s.ID]; dup {
			t.Errorf("starter ID %q used by both %q and %q", s.ID, other, s.Label)
		}
		seen[s.ID] = s.Label
	}
}

// TestLinterStarters_renderedInForm confirms the starter dropdown surfaces
// in the rendered authoring section when AllowEdits is true.
func TestLinterStarters_renderedInForm(t *testing.T) {
	body := renderAuthoringSection("demo", emptyPolicyForTest(), true)
	for _, want := range []string{
		"Start from a pattern",
		"gwLintStarterApply",
		"data-regex",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered authoring section missing %q", want)
		}
	}
	// Every starter's Label must appear as an option text.
	for _, s := range LinterStarters {
		if !strings.Contains(body, s.Label) {
			t.Errorf("starter %q (Label %q) not rendered in dropdown", s.ID, s.Label)
		}
	}
}

func TestLinterStarters_dropdownHiddenWithoutAllowEdits(t *testing.T) {
	body := renderAuthoringSection("demo", emptyPolicyForTest(), false)
	if strings.Contains(body, "Start from a pattern") {
		t.Error("starter dropdown should hide when AllowEdits is false (read-only mode)")
	}
}

// emptyPolicyForTest returns the zero-value gateway.LinterPolicy. Empty
// is fine for these tests - we only assert on the rendered form chrome
// (dropdown, autofill JS), not on iterated check rows.
func emptyPolicyForTest() gateway.LinterPolicy {
	return gateway.LinterPolicy{}
}
