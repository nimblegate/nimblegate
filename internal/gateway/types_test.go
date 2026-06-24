// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
)

func TestPolicy_Validate_valid(t *testing.T) {
	p := Policy{
		Repo:          "demo",
		ProtectedRefs: []string{"refs/heads/main", "refs/heads/*"},
		Enabled:       true,
	}
	if err := p.Validate(); err != nil {
		t.Errorf("valid policy must return nil, got: %v", err)
	}
}

func TestPolicy_Validate_malformedPattern(t *testing.T) {
	p := Policy{
		Repo:          "bad",
		ProtectedRefs: []string{"[bad"},
		Enabled:       true,
	}
	err := p.Validate()
	if err == nil {
		t.Fatal("expected non-nil error for malformed pattern, got nil")
	}
	// error message must reference the bad pattern
	if want := "[bad"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q should mention pattern %q", err.Error(), want)
	}
}
