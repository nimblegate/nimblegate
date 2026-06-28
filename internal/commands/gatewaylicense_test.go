// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

func TestLicenseSaveFlipsAttestation(t *testing.T) {
	dir := t.TempDir()
	h := licenseHandlers{policyRoot: dir, token: "tok"}

	form := url.Values{"commercial": {"1"}, "order_ref": {"LS-9"}}
	req := httptest.NewRequest(http.MethodPost, "/settings/license", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()

	h.save(rec, req)

	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d", rec.Code)
	}
	lic, err := gateway.LoadLicense(dir)
	if err != nil {
		t.Fatalf("LoadLicense: %v", err)
	}
	if !lic.Commercial || lic.OrderRef != "LS-9" {
		t.Fatalf("attestation not saved: %+v", lic)
	}
}

func TestLicenseSaveKeepsOrderRefOutOfAuditLog(t *testing.T) {
	const secretRef = "SECRET-REF-DO-NOT-LOG-9999"
	dir := t.TempDir()
	h := licenseHandlers{policyRoot: dir, token: "tok"}

	form := url.Values{"commercial": {"1"}, "order_ref": {secretRef}}
	req := httptest.NewRequest(http.MethodPost, "/settings/license", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	rec := httptest.NewRecorder()

	h.save(rec, req)

	// Sanity: attestation was written to license.toml with the order ref.
	lic, err := gateway.LoadLicense(dir)
	if err != nil {
		t.Fatalf("LoadLicense: %v", err)
	}
	if lic.OrderRef != secretRef {
		t.Fatalf("expected OrderRef %q in license.toml, got %q", secretRef, lic.OrderRef)
	}

	// Privacy: the event log must not contain the order-ref string.
	logPath := dir + "/" + gateway.EventsFile
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("could not read audit log %s: %v", logPath, err)
	}
	if strings.Contains(string(raw), secretRef) {
		t.Fatalf("order-ref %q must not appear in audit log; log contents:\n%s", secretRef, raw)
	}
	// The event must have been logged (proves the ref exclusion is meaningful).
	if !strings.Contains(string(raw), "license-attestation-update") {
		t.Fatalf("expected 'license-attestation-update' in audit log; log contents:\n%s", raw)
	}
	if !strings.Contains(string(raw), "has_order_ref") {
		t.Fatalf("expected 'has_order_ref' in audit log; log contents:\n%s", raw)
	}
}

func TestLicenseSaveRejectsBadCSRF(t *testing.T) {
	dir := t.TempDir()
	h := licenseHandlers{policyRoot: dir, token: "tok"}
	req := httptest.NewRequest(http.MethodPost, "/settings/license", strings.NewReader("commercial=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "wrong")
	rec := httptest.NewRecorder()
	h.save(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("bad CSRF should be 403, got %d", rec.Code)
	}
}
