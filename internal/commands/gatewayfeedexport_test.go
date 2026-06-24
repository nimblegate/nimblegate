// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeedExportHandler_CSVDownload(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	_ = os.MkdirAll(repo, 0o755)
	_ = os.WriteFile(filepath.Join(repo, "audit.log"),
		[]byte(`{"time":"2026-06-01T00:00:00Z","repo":"r","accept":true}`+"\n"), 0o600)

	req := httptest.NewRequest(http.MethodGet, "/feed/export?format=csv", nil)
	rec := httptest.NewRecorder()
	feedExportHandler(root)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("missing attachment disposition: %q", cd)
	}
	if !strings.HasPrefix(rec.Body.String(), "time,repo,refs,accept") {
		t.Fatalf("unexpected csv body: %q", rec.Body.String())
	}
}

func TestFeedExportHandler_BadFormat400(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/feed/export?format=xml", nil)
	rec := httptest.NewRecorder()
	feedExportHandler(t.TempDir())(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rec.Code)
	}
}
