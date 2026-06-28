// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/auth"
)

// newAuthFixture sets up a Store in a temp policyRoot, an authHandlers
// configured to use it, and a tiny inner handler that records "OK\n" so the
// middleware test can prove pass-through. The fresh setup token (returned)
// is the one the caller can consume.
func newAuthFixture(t *testing.T, mode authMode) (*authHandlers, string, *http.ServeMux) {
	t.Helper()
	tmp := t.TempDir()
	store, err := auth.Open(filepath.Join(tmp, "_auth.db"))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	tok, _, err := auth.EnsureSetupToken(tmp)
	if err != nil {
		t.Fatalf("EnsureSetupToken: %v", err)
	}
	ah := &authHandlers{
		store:         store,
		policyRoot:    tmp,
		sessionTTL:    1 * time.Hour,
		mode:          mode,
		rateLimitHits: map[string][]time.Time{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("OK\n"))
	})
	mux.HandleFunc("/static/htmx.min.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("htmx-stub"))
	})
	mux.HandleFunc("/login", ah.login)
	mux.HandleFunc("/logout", ah.logout)
	mux.HandleFunc("/setup", ah.setup)
	return ah, tok, mux
}

func TestAuth_middleware_unauthenticatedRedirects(t *testing.T) {
	ah, _, mux := newAuthFixture(t, authModeSetupToken)
	h := ah.Middleware(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/login") {
		t.Errorf("Location = %q, want /login…", loc)
	}
}

func TestAuth_middleware_publicRoutesPassthrough(t *testing.T) {
	ah, _, mux := newAuthFixture(t, authModeSetupToken)
	h := ah.Middleware(mux)
	for _, path := range []string{"/login", "/setup", "/static/htmx.min.js"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusSeeOther && strings.HasPrefix(rec.Header().Get("Location"), "/login") {
				t.Errorf("path %s redirected to /login despite being public; body=%s", path, rec.Body.String())
			}
		})
	}
}

func TestAuth_middleware_authOffPassthrough(t *testing.T) {
	ah, _, mux := newAuthFixture(t, authModeOff)
	h := ah.Middleware(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200 (auth=off should pass through)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OK") {
		t.Errorf("body = %q, want 'OK'", rec.Body.String())
	}
}

func TestAuth_setup_getFormWhenNoUsers(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	ah.setup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "First-run setup") || !strings.Contains(body, `name="setup_token"`) {
		t.Errorf("setup page missing key elements; body=%s", body)
	}
}

func TestAuth_setup_404WhenUserExists(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	if _, err := ah.store.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	ah.setup(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestAuth_setup_postCorrectTokenCreatesUser(t *testing.T) {
	ah, tok, _ := newAuthFixture(t, authModeSetupToken)
	form := url.Values{
		"setup_token": {tok},
		"username":    {"admin"},
		"password":    {"supersecret123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ah.setup(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("Location = %q, want /login…", rec.Header().Get("Location"))
	}
	if n, _ := ah.store.UserCount(); n != 1 {
		t.Errorf("UserCount = %d, want 1 after setup", n)
	}
	// Subsequent GET /setup must 404.
	rec2 := httptest.NewRecorder()
	ah.setup(rec2, httptest.NewRequest(http.MethodGet, "/setup", nil))
	if rec2.Code != http.StatusNotFound {
		t.Errorf("second GET /setup = %d, want 404", rec2.Code)
	}
}

func TestAuth_setup_postWrongTokenRejected(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	form := url.Values{
		"setup_token": {"WRONG-WRONG-WRONG-WRONG"},
		"username":    {"admin"},
		"password":    {"supersecret123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ah.setup(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "wrong-setup-token") {
		t.Errorf("Location = %q, want err=wrong-setup-token", loc)
	}
	if n, _ := ah.store.UserCount(); n != 0 {
		t.Errorf("UserCount = %d, want 0 after rejected setup", n)
	}
}

func TestAuth_login_postCorrectCredsSetsCookie(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	if _, err := ah.store.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"username": {"admin"},
		"password": {"supersecret123"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ah.login(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var sess *http.Cookie
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			sess = c
			break
		}
	}
	if sess == nil {
		t.Fatal("nbg_session cookie not set on successful login")
	}
	if !sess.HttpOnly {
		t.Errorf("session cookie not HttpOnly")
	}
	if sess.SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie SameSite = %v, want Lax", sess.SameSite)
	}
	if sess.MaxAge <= 0 {
		t.Errorf("session cookie MaxAge = %d, want >0", sess.MaxAge)
	}
}

func TestAuth_login_postWrongCredsRedirectsWithError(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	if _, err := ah.store.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{
		"username": {"admin"},
		"password": {"wrong"},
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	ah.login(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "invalid-credentials") {
		t.Errorf("Location = %q, want err=invalid-credentials", rec.Header().Get("Location"))
	}
	// Should NOT set a session cookie.
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge > 0 {
			t.Errorf("session cookie set on failed login")
		}
	}
}

func TestAuth_login_rateLimitTriggers(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	if _, err := ah.store.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	// 5 attempts → on 6th the rate limit blocks.
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "10.0.0.5:1234"
		rec := httptest.NewRecorder()
		ah.login(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("rate limit triggered prematurely at attempt %d", i+1)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "10.0.0.5:1234"
	rec := httptest.NewRecorder()
	ah.login(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("6th attempt: status = %d, want 429", rec.Code)
	}
}

func TestAuth_middleware_passesWithValidCookie(t *testing.T) {
	ah, _, mux := newAuthFixture(t, authModeSetupToken)
	uid, err := ah.store.CreateUser("admin", "supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := ah.store.NewSession(uid, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := ah.Middleware(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OK") {
		t.Errorf("body = %q, want 'OK'", rec.Body.String())
	}
}

func TestAuth_logout_deletesSession(t *testing.T) {
	ah, _, _ := newAuthFixture(t, authModeSetupToken)
	uid, _ := ah.store.CreateUser("admin", "supersecret123")
	sid, _ := ah.store.NewSession(uid, time.Hour)

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	rec := httptest.NewRecorder()
	ah.logout(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("Location") != "/login" {
		t.Errorf("Location = %q, want /login", rec.Header().Get("Location"))
	}
	// Session row should be gone.
	if _, err := ah.store.LookupSession(sid); err == nil {
		t.Errorf("session still present after logout")
	}
	// Cookie should be cleared (MaxAge < 0).
	var clear *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			clear = c
		}
	}
	if clear == nil || clear.MaxAge >= 0 {
		t.Errorf("session cookie not cleared on logout: %+v", clear)
	}
}

func TestSafeNextPath(t *testing.T) {
	good := []string{"/", "/settings", "/policy?repo=x"}
	bad := []string{"", "//evil.com", `/\evil.com`, "http://evil", "evil", `\\evil`}
	for _, g := range good {
		if !safeNextPath(g) {
			t.Errorf("safeNextPath(%q)=false want true", g)
		}
	}
	for _, b := range bad {
		if safeNextPath(b) {
			t.Errorf("safeNextPath(%q)=true want false", b)
		}
	}
}

func TestAuth_middleware_expiredCookieClearedAndRedirects(t *testing.T) {
	ah, _, mux := newAuthFixture(t, authModeSetupToken)
	uid, _ := ah.store.CreateUser("admin", "supersecret123")
	sid, _ := ah.store.NewSession(uid, -1*time.Second) // already expired

	h := ah.Middleware(mux)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sid})
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Errorf("expired cookie → status = %d, want 303", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Location"), "/login") {
		t.Errorf("Location = %q, want /login…", rec.Header().Get("Location"))
	}
}

