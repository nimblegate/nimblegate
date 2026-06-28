// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
