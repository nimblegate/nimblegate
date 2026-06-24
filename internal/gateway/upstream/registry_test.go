// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import "testing"

func TestRegistry_LookupByHost(t *testing.T) {
	r := NewRegistry()
	stub := NewStub()
	r.Register("stub", stub)
	r.RegisterHost("example.com", "stub")

	got, err := r.LookupByURL("https://example.com/owner/repo")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.Name() != "stub" {
		t.Errorf("expected stub, got %s", got.Name())
	}
}

func TestRegistry_UnknownHostReturnsError(t *testing.T) {
	r := NewRegistry()
	_, err := r.LookupByURL("https://unknown.example/foo")
	if err == nil {
		t.Errorf("expected error for unknown host")
	}
}

func TestRegistry_ExplicitOverride(t *testing.T) {
	r := NewRegistry()
	stub := NewStub()
	r.Register("stub", stub)
	r.RegisterOverride("https://git.internal.example.com/foo", "stub")

	got, err := r.LookupByURL("https://git.internal.example.com/foo")
	if err != nil || got.Name() != "stub" {
		t.Errorf("explicit override should resolve, err=%v got=%v", err, got)
	}
}
