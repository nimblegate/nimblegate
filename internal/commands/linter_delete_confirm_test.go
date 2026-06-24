// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// TestLinterDeleteConfirm_renderedMarkupShape pins the structural
// invariants that make the delete-confirm look like a compact inline
// banner instead of a full-width column block:
//
//  1. The span carries gw-lint-confirm class so the scoped CSS override
//     in gatewayshell.go can switch flex-direction back to row + width:auto
//  2. The Confirm button is wrapped in a <form> so it picks up the
//     primary-button styling from .wlconfirm form button (whitelist tab
//     CSS rule that linter delete deliberately reuses)
//
// Without (1) the banner stretches site-wide; without (2) the Confirm
// button looks identical to Cancel (both render as neutral .wlconfirm>button).
func TestLinterDeleteConfirm_renderedMarkupShape(t *testing.T) {
	var b bytes.Buffer
	err := authoringTmpl.ExecuteTemplate(&b, "deleteConfirm", struct {
		Name string
		Repo string
	}{
		Name: "internal-secret",
		Repo: "demo",
	})
	if err != nil {
		t.Fatalf("deleteConfirm execute: %v", err)
	}
	body := b.String()

	for _, want := range []string{
		`class="wlconfirm gw-lint-confirm"`, // (1) scope class for CSS override
		`<form `,                            // (2) Confirm wrapped in form for primary styling
		`type="submit"`,
		"internal-secret",
		`type="button"`, // Cancel stays a plain button
		"Cancel",
		"Confirm delete",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("deleteConfirm markup missing %q in:\n%s", want, body)
		}
	}
}

// TestLinterDeleteConfirm_compactCSSRulePresent confirms the CSS override
// that makes the banner compact ships in the page shell.
func TestLinterDeleteConfirm_compactCSSRulePresent(t *testing.T) {
	vm := authoringVM{Repo: "demo", AllowEdits: true, Starters: LinterStarters}
	_ = vm
	// Render via the page shell rather than the form directly so we get the
	// <style> blocks too. Use any handler that goes through renderGwShell -
	// renderAuthoringSection itself only produces the section markup.
	body := gwShellCSS()
	for _, want := range []string{
		`.wlconfirm.gw-lint-confirm`,
		`inline-flex`,
		`width:auto`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page CSS missing scoped rule %q", want)
		}
	}
}

// gwShellCSS returns the CSS payload the dashboard ships. Mirrors what
// renderGwShell embeds; here we just need any sentinel snippet that
// proves the rules are wired.
func gwShellCSS() string {
	rec := httptest.NewRecorder()
	renderGwShell(rec, gwLayout{Title: "x", Content: ""})
	return rec.Body.String()
}

// Sanity guard: the empty LinterPolicy type assertion compiles, mirroring
// the helper in linterstarters_test.go but kept private to this file.
var _ gateway.LinterPolicy = gateway.LinterPolicy{}
