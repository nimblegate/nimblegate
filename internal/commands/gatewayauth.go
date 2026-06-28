// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"context"
	"errors"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"nimblegate/internal/auth"
)

// authMode is the operator-selected auth posture.
type authMode string

const (
	authModeSetupToken authMode = "setup-token" // default for container
	authModeOff        authMode = "off"         // for Caddy-fronted deployments / dev
)

// sessionCookieName is the HttpOnly cookie set on /login success.
const sessionCookieName = "nbg_session"

// authCtxKey is the context key for the authenticated user ID inside an
// authenticated request. Unexported so external packages can't depend on it.
type authCtxKey struct{}

// userIDFromContext returns the auth'd user ID, or 0 if the request is not
// authenticated (e.g. --auth=off, or a public route).
func userIDFromContext(ctx context.Context) int64 {
	if v, ok := ctx.Value(authCtxKey{}).(int64); ok {
		return v
	}
	return 0
}

// authHandlers serves /login, /logout, /setup AND provides the middleware
// that wraps every other dashboard route.
type authHandlers struct {
	store         *auth.Store
	policyRoot    string
	sessionTTL    time.Duration
	mode          authMode
	csrfToken     string // shared with the dashboard's existing CSRF
	rateLimitMu   sync.Mutex
	rateLimitHits map[string][]time.Time // per-IP timestamps of failed attempts
}

const (
	failedAttemptsLimit  = 5
	failedAttemptsWindow = 5 * time.Minute
)

// recordFailedAttempt logs an attempt timestamp for an IP and returns whether
// the IP is now over the limit. Old attempts (older than the window) are
// pruned at every call.
func (h *authHandlers) recordFailedAttempt(ip string) bool {
	h.rateLimitMu.Lock()
	defer h.rateLimitMu.Unlock()
	now := time.Now()
	hits := h.rateLimitHits[ip]
	// Drop attempts outside the window.
	pruned := hits[:0]
	for _, t := range hits {
		if now.Sub(t) < failedAttemptsWindow {
			pruned = append(pruned, t)
		}
	}
	pruned = append(pruned, now)
	h.rateLimitHits[ip] = pruned
	return len(pruned) >= failedAttemptsLimit
}

// rateLimited returns true if the IP is currently over the limit (without
// recording a new attempt).
func (h *authHandlers) rateLimited(ip string) bool {
	h.rateLimitMu.Lock()
	defer h.rateLimitMu.Unlock()
	now := time.Now()
	count := 0
	for _, t := range h.rateLimitHits[ip] {
		if now.Sub(t) < failedAttemptsWindow {
			count++
		}
	}
	return count >= failedAttemptsLimit
}

// clearRateLimit drops all recorded attempts for an IP. Called on successful
// login so a legitimate user who finally remembered their password isn't held
// hostage by their own earlier typos.
func (h *authHandlers) clearRateLimit(ip string) {
	h.rateLimitMu.Lock()
	defer h.rateLimitMu.Unlock()
	delete(h.rateLimitHits, ip)
}

// clientIP extracts the client IP for rate-limiting. Honors X-Forwarded-For
// only for the first hop (defense in depth - we expect the gateway to bind
// localhost when fronted by a proxy, but mis-config happens).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- middleware ---

// publicRoute returns true if the path is reachable without session auth
// (login form, logout, setup when no users, static assets). The agent API
// (/api/v1/, /mcp) is exempt too: it carries its own Bearer-token auth.
func publicRoute(p string) bool {
	switch p {
	case "/login", "/logout", "/setup", "/help", "/favicon.ico", "/mcp":
		return true
	}
	return strings.HasPrefix(p, "/static/") || strings.HasPrefix(p, "/api/v1/")
}

// Middleware wraps the dashboard mux. If --auth=off the middleware is a
// passthrough. Otherwise: public routes go through unchanged; everything else
// requires a valid session cookie or redirects to /login?next=<original-path>.
func (h *authHandlers) Middleware(next http.Handler) http.Handler {
	if h == nil || h.mode == authModeOff {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicRoute(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil || c.Value == "" {
			h.redirectToLogin(w, r)
			return
		}
		uid, err := h.store.LookupSession(c.Value)
		if err != nil {
			// Expired or unknown - drop the cookie and redirect.
			h.clearCookie(w, r)
			h.redirectToLogin(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), authCtxKey{}, uid)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *authHandlers) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Path
	if r.URL.RawQuery != "" {
		next += "?" + r.URL.RawQuery
	}
	target := "/login"
	if next != "" && next != "/" && next != "/login" {
		target += "?next=" + url.QueryEscape(next)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// --- cookie helpers ---

func (h *authHandlers) setCookie(w http.ResponseWriter, r *http.Request, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.sessionTTL.Seconds()),
	})
}

func (h *authHandlers) clearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// --- handlers ---

// login serves both GET (render form) and POST (verify credentials).
func (h *authHandlers) login(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// If already authenticated, redirect to / (don't show login form when
		// session is good).
		if c, err := r.Cookie(sessionCookieName); err == nil {
			if _, err := h.store.LookupSession(c.Value); err == nil {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
		errMsg := r.URL.Query().Get("err")
		next := r.URL.Query().Get("next")
		renderAuthPage(w, authPageData{
			Page:  "login",
			Error: errMsg,
			Next:  next,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ip := clientIP(r)
	if h.rateLimited(ip) {
		http.Error(w, "too many failed attempts, try again in a few minutes", http.StatusTooManyRequests)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	next := r.FormValue("next")
	if username == "" || password == "" {
		http.Redirect(w, r, "/login?err=please-fill-both-fields"+nextQS(next), http.StatusSeeOther)
		return
	}
	uid, err := h.store.VerifyPassword(username, password)
	if err != nil {
		_ = h.recordFailedAttempt(ip)
		http.Redirect(w, r, "/login?err=invalid-credentials"+nextQS(next), http.StatusSeeOther)
		return
	}
	// Successful login: clear any earlier-typo records so the user isn't
	// punished by their own warmup attempts.
	h.clearRateLimit(ip)
	sid, err := h.store.NewSession(uid, h.sessionTTL)
	if err != nil {
		http.Error(w, "session create failed", http.StatusInternalServerError)
		return
	}
	h.setCookie(w, r, sid)
	http.Redirect(w, r, localRedirectTarget(next), http.StatusSeeOther)
}

// localRedirectTarget turns a user-supplied `next` into a guaranteed same-site
// path. It first applies the structural gate (safeNextPath rejects "//" and
// "/\" forms), then parses the value and keeps ONLY its path and query,
// discarding any scheme or host - extracting url.URL.EscapedPath is the
// recognized sanitizer for go/unvalidated-url-redirection. Anything that fails
// either step falls back to "/".
func localRedirectTarget(next string) string {
	if !safeNextPath(next) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil {
		return "/"
	}
	target := u.EscapedPath()
	if target == "" {
		target = "/"
	}
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return target
}

// logout deletes the session row + clears the cookie. GET is accepted so a
// plain link works; in practice the rail's logout button POSTs.
func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil && c.Value != "" {
		_ = h.store.DeleteSession(c.Value)
	}
	h.clearCookie(w, r)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// setup serves GET (form, only when no users exist) and POST (claim token +
// create the admin user).
func (h *authHandlers) setup(w http.ResponseWriter, r *http.Request) {
	count, err := h.store.UserCount()
	if err != nil {
		http.Error(w, "user count: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if count > 0 {
		// Setup is over. The token file is deleted at first-claim; if a leftover
		// file exists for some reason, clean it up.
		_ = auth.DeleteSetupToken(h.policyRoot)
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		errMsg := r.URL.Query().Get("err")
		renderAuthPage(w, authPageData{
			Page:  "setup",
			Error: errMsg,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tok := r.FormValue("setup_token")
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if tok == "" || username == "" || password == "" {
		http.Redirect(w, r, "/setup?err=please-fill-all-fields", http.StatusSeeOther)
		return
	}
	ok, err := auth.ConsumeSetupToken(h.policyRoot, tok)
	if err != nil {
		http.Error(w, "consume setup token: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Redirect(w, r, "/setup?err=wrong-setup-token", http.StatusSeeOther)
		return
	}
	if _, err := h.store.CreateUser(username, password); err != nil {
		// On error AFTER consuming the token, the operator is stuck - restart
		// will regenerate the token because the user wasn't created. Print to
		// stderr so they know what happened.
		// The error surface for the operator is a 400 with the failure reason.
		if errors.Is(err, auth.ErrUserExists) {
			http.Redirect(w, r, "/setup?err=username-taken", http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/setup?err="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login?err=account-created-please-sign-in", http.StatusSeeOther)
}

// safeNextPath reports whether next is a same-site local path safe to redirect
// to. It must start with "/" but not "//" or "/\" - browsers treat both of the
// latter as absolute URLs (//host, /\host), which would be an open redirect.
func safeNextPath(next string) bool {
	if next == "" || next[0] != '/' {
		return false
	}
	if len(next) > 1 && (next[1] == '/' || next[1] == '\\') {
		return false
	}
	return true
}

func nextQS(next string) string {
	if next == "" {
		return ""
	}
	return "&next=" + url.QueryEscape(next)
}

// joinPath defends against open-redirect via query parameter; consumers should
// use this when constructing redirects from user-provided `next` values.
func joinPath(base, sub string) string {
	if sub == "" {
		return base
	}
	if !strings.HasPrefix(sub, "/") {
		return base
	}
	return path.Clean(sub)
}

// --- rendering ---

type authPageData struct {
	Page  string // "login" or "setup"
	Error string
	Next  string
}

func renderAuthPage(w http.ResponseWriter, d authPageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = authPageTmpl.ExecuteTemplate(w, "page", d)
}

var authPageTmpl = template.Must(template.New("auth").Funcs(template.FuncMap{
	"prettyErr": prettyAuthError,
}).New("page").Parse(`<!doctype html><html><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if eq .Page "setup"}}First-run setup{{else}}Sign in{{end}} : nimblegate</title>
<link rel="icon" type="image/svg+xml" href="/static/favicon.svg">
<style>` + gwRootVars + `
 :root{color-scheme:dark}
 html,body{margin:0;height:100%;background:var(--gw-bg-input);color:var(--gw-text);font:14px/1.45 system-ui,-apple-system,Segoe UI,sans-serif}
 main{max-width:380px;margin:8vh auto 0;padding:24px}
 .card{background:var(--gw-bg-panel);border:1px solid var(--gw-border);border-radius:8px;padding:22px}
 h1{margin:0 0 4px;font-size:18px;color:var(--gw-text)}
 .sub{color:var(--gw-text-muted);font-size:13px;margin:0 0 16px}
 form{display:flex;flex-direction:column;gap:12px}
 label{display:flex;flex-direction:column;gap:4px;color:var(--gw-text-soft);font-size:13px}
 input{background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:7px 9px;font:inherit}
 input:focus{outline:none;border-color:var(--gw-accent)}
 /* Primary-action button: Accent 2 (#79c0ff) bg with Surface 0 (#0b0d11) text. Both colors from the 5-step brand palette; max contrast (~13:1) and instantly identifies this as THE action on the page. Hover steps down to Accent 1 (#5e93c4) so the active press reads as a small darkening rather than a colour shift. */
 button{align-self:flex-start;padding:8px 20px;background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);border-radius:6px;cursor:pointer;font:inherit;font-weight:500}
 button:hover{background:#5e93c4;border-color:#5e93c4}
 .err{background:var(--gw-error-bg);border:1px solid var(--gw-block-border);color:var(--gw-error-text);padding:9px 12px;border-radius:6px;font-size:13px;margin:0 0 14px}
 .ok{background:var(--gw-ok-bg-mid);border:1px solid var(--gw-ok-accent);color:var(--gw-ok-text-soft);padding:9px 12px;border-radius:6px;font-size:13px;margin:0 0 14px}
 .brand{font-weight:600;font-size:20px;color:var(--gw-accent);margin-bottom:22px;display:flex;align-items:center;gap:10px}
 .brand svg{width:26px;height:26px}
 .hint{color:var(--gw-text-fainter);font-size:12px;margin-top:6px}
</style></head><body><main><div class="brand"><svg viewBox="0 0 64 64" fill="currentColor" aria-hidden="true"><circle cx="32.00" cy="4.00" r="2.50" opacity="0.65"/><circle cx="24.47" cy="5.93" r="2.77" opacity="0.74"/><circle cx="33.24" cy="7.86" r="1.94" opacity="0.47"/><circle cx="42.38" cy="9.79" r="3.03" opacity="0.82"/><circle cx="12.98" cy="11.72" r="2.37" opacity="0.61"/><circle cx="49.85" cy="13.66" r="2.05" opacity="0.51"/><circle cx="26.11" cy="15.59" r="3.36" opacity="0.92"/><circle cx="20.96" cy="17.52" r="1.66" opacity="0.38"/><circle cx="55.51" cy="19.45" r="2.84" opacity="0.76"/><circle cx="8.05" cy="21.38" r="2.89" opacity="0.77"/><circle cx="43.28" cy="23.31" r="1.55" opacity="0.35"/><circle cx="40.13" cy="25.24" r="3.52" opacity="0.97"/><circle cx="8.14" cy="27.17" r="1.96" opacity="0.48"/><circle cx="59.20" cy="29.10" r="2.27" opacity="0.58"/><circle cx="15.91" cy="31.03" r="3.40" opacity="0.94"/><circle cx="28.40" cy="32.97" r="1.41" opacity="0.30"/><circle cx="53.30" cy="34.90" r="3.21" opacity="0.87"/><circle cx="4.44" cy="36.83" r="2.54" opacity="0.66"/><circle cx="51.26" cy="38.76" r="1.75" opacity="0.41"/><circle cx="30.77" cy="40.69" r="3.54" opacity="0.98"/><circle cx="15.40" cy="42.62" r="1.72" opacity="0.40"/><circle cx="56.81" cy="44.55" r="2.63" opacity="0.69"/><circle cx="12.33" cy="46.48" r="3.04" opacity="0.82"/><circle cx="36.98" cy="48.41" r="1.63" opacity="0.37"/><circle cx="42.52" cy="50.34" r="3.22" opacity="0.88"/><circle cx="13.60" cy="52.28" r="2.27" opacity="0.58"/><circle cx="47.48" cy="54.21" r="2.22" opacity="0.56"/><circle cx="26.52" cy="56.14" r="3.01" opacity="0.81"/><circle cx="28.54" cy="58.07" r="2.12" opacity="0.53"/><circle cx="32.00" cy="60.00" r="2.50" opacity="0.65"/></svg> nimblegate</div><div class="card">

{{if eq .Page "setup"}}
<h1>First-run setup</h1>
<p class="sub">Paste the one-time setup token (shown when the dashboard first started) and choose your admin credentials.</p>
{{if .Error}}<div class="err">{{prettyErr .Error}}</div>{{end}}
<form method="post" action="/setup">
  <label>Setup token <input type="text" name="setup_token" required autocomplete="off" autofocus placeholder="XXXX-XXXX-XXXX-XXXX"></label>
  <label>Username <input type="text" name="username" required autocomplete="username" minlength="2" maxlength="64"></label>
  <label>Password <input type="password" name="password" required autocomplete="new-password" minlength="8"></label>
  <button type="submit">Create admin account</button>
  <div class="hint">Bare-metal: run <code>nimblegate gateway setup-token --policy-root /srv/gateway/cfg</code>, or read <code>/srv/gateway/cfg/_setup_token</code> directly. Container: <code>docker logs &lt;container&gt; | grep "setup token"</code>.</div>
</form>
{{else}}
<h1>Sign in</h1>
<p class="sub">Sign in to manage the nimblegate gateway.</p>
{{if .Error}}{{if eq .Error "account-created-please-sign-in"}}<div class="ok">Account created. Sign in to continue.</div>{{else}}<div class="err">{{prettyErr .Error}}</div>{{end}}{{end}}
<form method="post" action="/login">
  <input type="hidden" name="next" value="{{.Next}}">
  <label>Username <input type="text" name="username" required autocomplete="username" autofocus></label>
  <label>Password <input type="password" name="password" required autocomplete="current-password"></label>
  <button type="submit">Sign in</button>
</form>
{{end}}
</div></main></body></html>`))

// prettyAuthError converts the small set of internal error codes used in
// /login and /setup redirects into human messages. Anything else is shown
// verbatim (deliberate - operator errors like "username-taken" don't need
// translation).
func prettyAuthError(code string) string {
	switch code {
	case "invalid-credentials":
		return "Username or password is wrong."
	case "please-fill-both-fields":
		return "Please fill in both username and password."
	case "please-fill-all-fields":
		return "Please fill in all fields."
	case "wrong-setup-token":
		return "Setup token didn't match. Check the value from docker logs."
	case "username-taken":
		return "That username is already taken."
	default:
		return code
	}
}
