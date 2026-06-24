// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"testing"

	"nimblegate/internal/gateway"
)

func TestMaxSeverity(t *testing.T) {
	cases := []struct {
		name string
		in   []gateway.Finding
		want string
	}{
		{"none", nil, ""},
		{"only info", []gateway.Finding{{Severity: "INFO"}}, "INFO"},
		{"warn over info", []gateway.Finding{{Severity: "INFO"}, {Severity: "WARN"}}, "WARN"},
		{"block wins", []gateway.Finding{{Severity: "WARN"}, {Severity: "BLOCK"}, {Severity: "ERROR"}}, "BLOCK"},
		{"unknown ignored", []gateway.Finding{{Severity: "WEIRD"}, {Severity: "INFO"}}, "INFO"},
	}
	for _, c := range cases {
		if got := maxSeverity(c.in); got != c.want {
			t.Errorf("%s: maxSeverity = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDedupHashStableAndDistinct(t *testing.T) {
	a := dedupHash([]byte(`{"repo":"x","accept":true}`))
	again := dedupHash([]byte(`{"repo":"x","accept":true}`))
	b := dedupHash([]byte(`{"repo":"y","accept":true}`))
	if a != again {
		t.Errorf("hash not stable: %q vs %q", a, again)
	}
	if a == b {
		t.Errorf("distinct inputs hashed the same: %q", a)
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex len = %d, want 64", len(a))
	}
}

func TestB2I(t *testing.T) {
	if b2i(true) != 1 || b2i(false) != 0 {
		t.Errorf("b2i wrong: true=%d false=%d", b2i(true), b2i(false))
	}
}

func TestFingerprintStableAndDistinct(t *testing.T) {
	a := fingerprint("security/no-private-keys-in-repo", "a.pem:1 - PEM key")
	b := fingerprint("security/no-private-keys-in-repo", "a.pem:1 - PEM key")
	c := fingerprint("security/no-private-keys-in-repo", "b.pem:9 - PEM key")
	d := fingerprint("command-safety/curl-pipe-shell", "a.pem:1 - PEM key")
	if a != b {
		t.Error("same frame+message must produce the same fingerprint")
	}
	if a == c {
		t.Error("different message must produce a different fingerprint")
	}
	if a == d {
		t.Error("different frame must produce a different fingerprint")
	}
}
