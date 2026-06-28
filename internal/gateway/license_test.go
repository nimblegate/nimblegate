// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLicenseMissingFileDefaultsNonCommercial(t *testing.T) {
	dir := t.TempDir()
	lic, err := LoadLicense(dir)
	if err != nil {
		t.Fatalf("LoadLicense on empty dir: %v", err)
	}
	if lic.Commercial {
		t.Fatalf("missing file should default to non-commercial, got Commercial=true")
	}
	if lic.OrderRef != "" {
		t.Fatalf("missing file should have empty OrderRef, got %q", lic.OrderRef)
	}
}

func TestSaveThenLoadLicenseRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := License{Commercial: true, OrderRef: "LS-ORDER-12345"}
	if err := SaveLicense(dir, want); err != nil {
		t.Fatalf("SaveLicense: %v", err)
	}
	got, err := LoadLicense(dir)
	if err != nil {
		t.Fatalf("LoadLicense: %v", err)
	}
	if got != want {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestLoadLicenseMalformedReturnsError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "license.toml"), []byte("this is not = valid = toml ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLicense(dir); err == nil {
		t.Fatalf("malformed license.toml should return an error, got nil")
	}
}
