// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "testing"

func TestIsGatedRef(t *testing.T) {
	p := Policy{ProtectedRefs: []string{"refs/heads/main", "refs/heads/release/*"}}
	cases := []struct {
		ref  string
		want bool
	}{
		{"refs/heads/main", true},
		{"refs/heads/release/1.2", true},
		{"refs/heads/feature/x", false},
		{"refs/heads/mainline", false},
		{"refs/tags/v1", false},
	}
	for _, c := range cases {
		if got := isGatedRef(p, c.ref); got != c.want {
			t.Errorf("isGatedRef(%q) = %v, want %v", c.ref, got, c.want)
		}
	}
}

func TestIsGatedRef_emptyMeansNoneGated(t *testing.T) {
	if isGatedRef(Policy{}, "refs/heads/main") {
		t.Error("empty ProtectedRefs should gate nothing")
	}
}

// GateAllRefs makes every ref gated regardless of ProtectedRefs - so feature
// branches are checked + fail-closed too, not relayed unfiltered.
func TestIsGatedRef_gateAllRefs(t *testing.T) {
	p := Policy{GateAllRefs: true} // no ProtectedRefs at all
	for _, ref := range []string{"refs/heads/main", "refs/heads/feature/x", "refs/tags/v1", "refs/heads/anything"} {
		if !isGatedRef(p, ref) {
			t.Errorf("with GateAllRefs, %q should be gated", ref)
		}
	}
}

// The regression this matcher exists for: "refs/heads/*" is the shipped default
// and reads as "gate every branch", but plain path.Match stops at "/", so the
// branch naming agents actually use relayed unchecked and silently - no audit
// row, no finding, exit 0. Verified live 2026-08-22: identical credential
// fixtures blocked on a flat branch and relayed on "agent/...".
func TestIsGatedRef_trailingStarSpansSlashes(t *testing.T) {
	p := Policy{ProtectedRefs: []string{"refs/heads/*"}}
	for _, ref := range []string{
		"refs/heads/main",
		"refs/heads/hotfix-123",
		"refs/heads/agent/task-1",
		"refs/heads/feature/login",
		"refs/heads/fix/bug",
		"refs/heads/dependabot/npm_and_yarn/lib/axios-1.2.3",
	} {
		if !isGatedRef(p, ref) {
			t.Errorf("refs/heads/* should gate %q", ref)
		}
	}
	// Scope still ends at the prefix: other ref namespaces stay ungated.
	for _, ref := range []string{"refs/tags/v1", "refs/remotes/origin/main"} {
		if isGatedRef(p, ref) {
			t.Errorf("refs/heads/* must not gate %q", ref)
		}
	}
}

func TestMatchRefPattern_recursiveUnderPrefix(t *testing.T) {
	cases := []struct {
		pat, ref string
		want     bool
	}{
		{"refs/heads/release/*", "refs/heads/release/1.2", true},
		{"refs/heads/release/*", "refs/heads/release/1.2/hotfix", true},
		{"refs/heads/release/*", "refs/heads/release", false}, // needs a segment beneath
		{"refs/heads/release/*", "refs/heads/main", false},
		{"refs/heads/feat-*/*", "refs/heads/feat-auth/step-1", true}, // glob in the prefix
		{"refs/heads/feat-*/*", "refs/heads/other/step-1", false},
		// No trailing "/*": single-segment path.Match semantics are unchanged.
		{"refs/heads/feat-*", "refs/heads/feat-auth", true},
		{"refs/heads/feat-*", "refs/heads/feat-auth/step-1", false},
		{"refs/heads/main", "refs/heads/main", true},
		{"refs/heads/main", "refs/heads/mainline", false},
	}
	for _, c := range cases {
		if got := matchRefPattern(c.pat, c.ref); got != c.want {
			t.Errorf("matchRefPattern(%q, %q) = %v, want %v", c.pat, c.ref, got, c.want)
		}
	}
}

// Validation must interpret the same shape as matching, so a pattern that
// loads cannot then fail to gate for a reason validation never checked.
func TestRefPatternErr_matchesWhatGatingAccepts(t *testing.T) {
	for _, pat := range []string{"refs/heads/*", "refs/heads/release/*", "refs/heads/main", "refs/heads/feat-*/*"} {
		if err := refPatternErr(pat); err != nil {
			t.Errorf("refPatternErr(%q) = %v, want nil", pat, err)
		}
		if err := (Policy{ProtectedRefs: []string{pat}}).Validate(); err != nil {
			t.Errorf("Validate with %q = %v, want nil", pat, err)
		}
	}
	for _, pat := range []string{"refs/heads/[", "refs/heads/[/*"} {
		if err := refPatternErr(pat); err == nil {
			t.Errorf("refPatternErr(%q) = nil, want a syntax error", pat)
		}
		if err := (Policy{ProtectedRefs: []string{pat}}).Validate(); err == nil {
			t.Errorf("Validate with %q = nil, want a syntax error", pat)
		}
	}
}
