// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func restService(t *testing.T) *Service {
	t.Helper()
	svc := testService(t, seedPolicyRoot(t))
	svc.Verify = func(tok string) (bool, error) { return tok == "nbg_good", nil }
	return svc
}

func TestRESTAuth(t *testing.T) {
	h := restService(t).RESTHandler()
	for _, tc := range []struct {
		auth string
		want int
	}{
		{"", http.StatusUnauthorized},
		{"Bearer nbg_bad", http.StatusForbidden},
		{"Bearer nbg_good", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats?days=30", nil)
		if tc.auth != "" {
			req.Header.Set("Authorization", tc.auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("auth %q: got %d want %d (%s)", tc.auth, rec.Code, tc.want, rec.Body.String())
		}
	}
}

func TestRESTRoutesAndJSON(t *testing.T) {
	h := restService(t).RESTHandler()
	for _, path := range []string{
		"/api/v1/stats",
		"/api/v1/bounce-rate?min=1",
		"/api/v1/top-rules",
		"/api/v1/recurring?repo=demo",
		"/api/v1/decisions?result=rejected",
		"/api/v1/changes?repo=demo",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer nbg_good")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("%s: content-type %q", path, ct)
		}
		// Verify envelope structure: responses must have a "data" key.
		var env struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Errorf("%s: envelope parse error: %v (%s)", path, err, rec.Body.String())
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nope", nil)
	req.Header.Set("Authorization", "Bearer nbg_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown route: %d", rec.Code)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/stats", nil)
	req2.Header.Set("Authorization", "Bearer nbg_good")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST must be rejected (read-only): %d", rec2.Code)
	}
}

func TestRESTDisabledWithoutVerify(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	svc.Verify = nil
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	rec := httptest.NewRecorder()
	svc.RESTHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil Verify must yield 503: %d", rec.Code)
	}
}

func TestBearerCaseInsensitive(t *testing.T) {
	h := restService(t).RESTHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "bearer nbg_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("lowercase bearer must work: %d", rec.Code)
	}
}

func TestRESTEnvelopeAndSeverityFilter(t *testing.T) {
	h := restService(t).RESTHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/top-rules?severity=BLOCK&repo=demoo", nil)
	req.Header.Set("Authorization", "Bearer nbg_good")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var env struct {
		Data  []map[string]any `json:"data"`
		Notes []string         `json:"notes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("envelope: %v (%s)", err, rec.Body.String())
	}
	for _, d := range env.Data {
		if d["severity"] != "BLOCK" {
			t.Errorf("JSON must be severity-filtered: %v", d)
		}
	}
	found := false
	for _, n := range env.Notes {
		if strings.Contains(n, `repo "demoo" not found`) {
			found = true
		}
	}
	if !found {
		t.Errorf("recovery note must reach JSON consumers: %v", env.Notes)
	}
}

func TestAllowConcurrent(t *testing.T) {
	svc := testService(t, seedPolicyRoot(t))
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				svc.allow("tok")
			}
		}()
	}
	wg.Wait()
}

func TestRESTRateLimit429(t *testing.T) {
	svc := restService(t)
	h := svc.RESTHandler()
	var last *httptest.ResponseRecorder
	for i := 0; i <= rateLimitPerMin; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
		req.Header.Set("Authorization", "Bearer nbg_good")
		last = httptest.NewRecorder()
		h.ServeHTTP(last, req)
	}
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d", last.Code)
	}
	if last.Header().Get("Retry-After") == "" {
		t.Error("429 must carry Retry-After")
	}
}

func TestCORSPreflightAndHeaders(t *testing.T) {
	svc := restService(t)
	for _, h := range []http.Handler{svc.RESTHandler(), svc.MCPHandler()} {
		// Preflight: no Authorization header (browsers never send it on
		// preflight) - must succeed without auth.
		req := httptest.NewRequest(http.MethodOptions, "/api/v1/stats", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Errorf("preflight must return 204, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
			!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Errorf("CORS headers missing on preflight: %v", rec.Header())
		}
	}
	// Normal authenticated response also carries Allow-Origin.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/stats", nil)
	req.Header.Set("Authorization", "Bearer nbg_good")
	rec := httptest.NewRecorder()
	svc.RESTHandler().ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS header missing on response: %v", rec.Header())
	}
}
