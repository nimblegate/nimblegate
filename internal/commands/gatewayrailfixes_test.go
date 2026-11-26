// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
)

func TestClientIP_TrustedProxyOnly(t *testing.T) {
	proxies, err := parseTrustedProxies("10.0.0.1, 192.168.5.0/24")
	if err != nil {
		t.Fatal(err)
	}
	req := func(remote string, xff ...string) *http.Request {
		r := httptest.NewRequest("POST", "/login", nil)
		r.RemoteAddr = remote
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		name string
		r    *http.Request
		want string
	}{
		{"untrusted peer ignores XFF", req("203.0.113.9:5000", "1.2.3.4"), "203.0.113.9"},
		{"no proxies configured", req("10.0.0.1:5000", "1.2.3.4"), "10.0.0.1"},
		{"trusted peer, right-most untrusted", req("10.0.0.1:5000", "6.6.6.6, 1.2.3.4"), "1.2.3.4"},
		{"skips trusted hops", req("10.0.0.1:5000", "1.2.3.4, 192.168.5.7"), "1.2.3.4"},
		{"multiple headers", req("10.0.0.1:5000", "9.9.9.9", "1.2.3.4"), "1.2.3.4"},
		{"garbage hop falls back to peer", req("10.0.0.1:5000", "not-an-ip"), "10.0.0.1"},
	}
	for _, tc := range cases {
		p := proxies
		if tc.name == "no proxies configured" {
			p = nil
		}
		if got := clientIP(tc.r, p); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
	if _, err := parseTrustedProxies("10.0.0.x"); err == nil {
		t.Error("invalid proxy address must be rejected")
	}
}

// Without a CSRF token (dashboard without --allow-edits) the reset handler
// must refuse every request.
func TestFeedResetLoop_RequiresCSRFAlways(t *testing.T) {
	root := t.TempDir()
	seedActiveLoopRepo(t, root, "api", 42, 2, 5, "@bot")
	r := httptest.NewRequest("POST", "/feed/reset-loop?repo=api&pr=42", nil)
	w := httptest.NewRecorder()
	feedResetLoopHandler(root, "")(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("empty token must 403, got %d", w.Code)
	}
	if s, _ := notification.ReadPRState(root, "api", 42); s == nil {
		t.Fatal("loop state must survive a refused reset")
	}
}

func TestAutoPRSetup_RejectsInvalidRepo(t *testing.T) {
	w := httptest.NewRecorder()
	autoPRSetupHandler(t.TempDir(), true, func() string { return "tok" })(w, httptest.NewRequest("GET", "/auto-pr/config?repo=../../etc", nil))
	if strings.Contains(w.Body.String(), "../../etc") {
		t.Fatal("an invalid repo name must not be used or echoed")
	}
	if v := loadNotifRailView(t.TempDir(), "../x"); v.Enabled != defaultNotifRailView().Enabled {
		t.Fatal("invalid repo must yield the default view")
	}
}

func TestAutoPRRetry_SurfacesRequeueError(t *testing.T) {
	tmp := t.TempDir()
	policyRoot, reposRoot := filepath.Join(tmp, "policy"), filepath.Join(tmp, "repos")
	_ = os.MkdirAll(policyRoot, 0o755)
	_ = os.MkdirAll(reposRoot, 0o755)
	seedActiveRepo(t, policyRoot, reposRoot, "foo")
	// A directory where the deadletter file belongs makes the read fail.
	if err := os.Mkdir(filepath.Join(policyRoot, "foo", "pr-comment-deadletter.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/auto-pr/retry", strings.NewReader(url.Values{"repo": {"foo"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "tok")
	w := httptest.NewRecorder()
	autoPRRetryHandler(policyRoot, true, func() string { return "tok" })(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("requeue failure must surface as 500, got %d", w.Code)
	}
}

func TestReadActiveLoops_IncludesExhausted(t *testing.T) {
	root := t.TempDir()
	st := notification.PRState{PRNumber: 9, Repo: "api", Loop: notification.LoopCounters{AttemptCount: 5, MaxAttempts: 5, Exhausted: true}}
	if err := notification.WritePRState(root, "api", 9, st); err != nil {
		t.Fatal(err)
	}
	loops := readActiveLoops(filepath.Join(root, "api", "pr-comment-state"), "api")
	if len(loops) != 1 || !loops[0].Exhausted {
		t.Fatalf("exhausted loop must be listed for reset, got %+v", loops)
	}
}

func TestInlineNotifOrchestrator(t *testing.T) {
	root := t.TempDir()
	pol := gateway.Policy{
		Repo:        "demo",
		UpstreamURL: "https://gitea.example/o/demo.git",
		Notification: &gateway.NotificationConfig{
			Enabled:    true,
			WebhookURL: "https://hooks.example/x",
		},
	}
	if err := os.MkdirAll(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	o := inlineNotifOrchestrator(root, "demo", pol)
	if o == nil || o.Webhooks["demo"].URL != "https://hooks.example/x" {
		t.Fatalf("want an orchestrator resolving this repo's webhook, got %+v", o)
	}
	if _, err := o.Upstreams.LookupByURL(pol.UpstreamURL); err != nil {
		t.Fatalf("adapter for the upstream must be registered: %v", err)
	}
	if inlineNotifOrchestrator(root, "demo", gateway.Policy{Repo: "demo"}) != nil {
		t.Error("rail off must skip inline delivery")
	}
	// A credential the hook user cannot read (privilege-separated relay):
	// skip inline, leave it to the daemon. A directory stands in for an
	// unreadable file so the test also holds when run as root.
	if err := os.Mkdir(filepath.Join(root, "demo", "credential"), 0o700); err != nil {
		t.Fatal(err)
	}
	if inlineNotifOrchestrator(root, "demo", pol) != nil {
		t.Error("unreadable credential must skip inline delivery")
	}
}
