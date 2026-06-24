// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"testing"

	"nimblegate/internal/engine"
)

func TestRelativizeResults(t *testing.T) {
	root := "/tmp/afgw-123"
	in := []engine.CheckResult{{
		FrameID: "security/no-private-keys-in-repo",
		Reason:  "private keys detected: /tmp/afgw-123/work.txt:1 - OpenSSH private key",
		Hits:    []engine.Hit{{File: "/tmp/afgw-123/work.txt", Line: 1, Label: "OpenSSH private key"}},
	}}
	out := relativizeResults(in, root)

	if want := "private keys detected: work.txt:1 - OpenSSH private key"; out[0].Reason != want {
		t.Errorf("Reason = %q, want %q", out[0].Reason, want)
	}
	if out[0].Hits[0].File != "work.txt" {
		t.Errorf("Hit.File = %q, want work.txt", out[0].Hits[0].File)
	}
	if got := out[0].Hits[0].Format(); got != "work.txt:1 - OpenSSH private key" {
		t.Errorf("Hit.Format = %q", got)
	}
}

func TestRelativizeResults_trailingSlashRoot(t *testing.T) {
	out := relativizeResults([]engine.CheckResult{{
		Hits: []engine.Hit{{File: "/tmp/afgw-9/src/secrets.pem", Line: 42, Label: "key"}},
	}}, "/tmp/afgw-9/")
	if out[0].Hits[0].File != "src/secrets.pem" {
		t.Errorf("Hit.File = %q, want src/secrets.pem", out[0].Hits[0].File)
	}
}
