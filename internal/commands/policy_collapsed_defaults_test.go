// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestPolicyPage_currentlyEnabledFramesCollapsed asserts the "Currently
// enabled frames" details renders without `open` so operators see the
// section heading but not the expanded list by default. Operator
// preference 2026-06-07 - the always-expanded summary crowded the page;
// click-to-expand is the right baseline.
func TestPolicyPage_currentlyEnabledFramesCollapsed(t *testing.T) {
	root := t.TempDir()
	vm := buildPolicyView(root, "demo", []string{"git/folder-branch-lock", "encoding/no-bom"})
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, policyPageOpts{
		AllowEdits: true,
		Repos:      []string{"demo"},
	})
	body := rec.Body.String()

	// The summary heading must be present...
	if !strings.Contains(body, "Currently enabled frames") {
		t.Fatal("Currently enabled frames section missing entirely")
	}
	// ...but the wrapping details must NOT have `open`.
	enabledSummaryOpen := regexp.MustCompile(`<details[^>]*\bgw-enabled-summary\b[^>]*\bopen\b`)
	if enabledSummaryOpen.MatchString(body) {
		t.Error("Currently enabled frames details has `open` attribute; should be collapsed by default")
	}
}

// TestPolicyPage_newLinterButtonAbsent confirms the cross-tab CTA was
// removed. Operators on the Frames tab manage frames; linter authoring
// lives on the Custom linters tab. Mixing CTAs across tabs was confusing.
func TestPolicyPage_newLinterButtonAbsent(t *testing.T) {
	root := t.TempDir()
	vm := buildPolicyView(root, "demo", nil)
	rec := httptest.NewRecorder()
	renderPolicyPage(rec, vm, policyPageOpts{
		AllowEdits: true,
		Repos:      []string{"demo"},
	})
	body := rec.Body.String()
	if strings.Contains(body, "+ New linter") {
		t.Error("Quick-start row should not surface + New linter CTA (lives on Custom linters tab)")
	}
}
