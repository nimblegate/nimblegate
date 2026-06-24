// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import "testing"

func TestFingerprint_Stable(t *testing.T) {
	a := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 1)
	b := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 1)
	if a != b {
		t.Errorf("same input should produce same fingerprint: %q vs %q", a, b)
	}
	if a[:7] != "sha256:" {
		t.Errorf("fingerprint must start with sha256: prefix, got %q", a)
	}
}

func TestFingerprint_DifferentFile(t *testing.T) {
	a := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 1)
	b := Fingerprint("security/no-private-keys-in-repo", "other/key.pem", 1)
	if a == b {
		t.Errorf("different file should change fingerprint")
	}
}

func TestFingerprint_DifferentLine(t *testing.T) {
	a := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 1)
	b := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 2)
	if a == b {
		t.Errorf("different line should change fingerprint")
	}
}

func TestFingerprint_DifferentFrame(t *testing.T) {
	a := Fingerprint("security/no-private-keys-in-repo", "config/key.pem", 1)
	b := Fingerprint("convention/html-required-meta", "config/key.pem", 1)
	if a == b {
		t.Errorf("different frame should change fingerprint")
	}
}
