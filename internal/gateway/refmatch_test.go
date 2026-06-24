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
