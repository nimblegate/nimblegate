// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// authRecorder is an http upstream that demands basic auth and records the
// user:pass it was sent, per request path.
type authRecorder struct {
	mu       sync.Mutex
	seen     []string
	requests int
}

func (a *authRecorder) handler(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	a.requests++
	a.mu.Unlock()
	h := r.Header.Get("Authorization")
	if h == "" {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	b, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Basic "))
	a.mu.Lock()
	a.seen = append(a.seen, string(b))
	a.mu.Unlock()
	w.WriteHeader(http.StatusNotFound)
}

func (a *authRecorder) got() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.seen...)
}

// argvLoggingGit puts a git wrapper first on PATH that appends each argv to a
// file and then runs the real git, so a test can see every git command line.
func argvLoggingGit(t *testing.T) string {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	return log
}

func TestUpstreamCredential_neverInArgvAndSameAuthAsTokenURL(t *testing.T) {
	log := argvLoggingGit(t)
	rec := &authRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer srv.Close()
	url := srv.URL + "/org/repo.git"

	_, _ = remoteHeads(url, "tok-bare-123")
	_, _ = remoteHeads(url, "svc:tok-pair-456")
	_ = remoteDefaultBranch(url, "tok-sym-789")
	bare := t.TempDir()
	if err := exec.Command("git", "init", "--bare", "-q", bare).Run(); err != nil {
		t.Fatal(err)
	}
	_, _ = SeedFromUpstream(bare, url, "tok-seed-000")

	got := strings.Join(rec.got(), " ")
	for _, want := range []string{"tok-bare-123:", "svc:tok-pair-456", "tok-sym-789:", "tok-seed-000:"} {
		if !strings.Contains(got, want) {
			t.Errorf("upstream never received %q; got %q", want, got)
		}
	}
	argv, _ := os.ReadFile(log)
	for _, tok := range []string{"tok-bare-123", "tok-pair-456", "tok-sym-789", "tok-seed-000"} {
		if strings.Contains(string(argv), tok) {
			t.Errorf("credential %q appeared in a git argv:\n%s", tok, argv)
		}
	}
}

func TestRelay_credentialNotInArgv(t *testing.T) {
	log := argvLoggingGit(t)
	rec := &authRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handler))
	defer srv.Close()

	work, bare := t.TempDir(), t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main", work},
		{"-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"},
		{"clone", "-q", "--bare", work, bare},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	head, _ := exec.Command("git", "--git-dir", bare, "rev-parse", "refs/heads/main").Output()
	err := Relay(srv.URL+"/org/repo.git", "tok-relay-321", bare,
		[]RefUpdate{{Name: "refs/heads/main", OldRev: zeroRev, NewRev: strings.TrimSpace(string(head))}})
	if err == nil {
		t.Fatal("push to a 404 upstream should fail")
	}
	if strings.Contains(err.Error(), "tok-relay-321") {
		t.Errorf("relay error leaks the credential: %v", err)
	}
	if got := strings.Join(rec.got(), " "); !strings.Contains(got, "tok-relay-321:") {
		t.Errorf("upstream never received the credential; got %q", got)
	}
	argv, _ := os.ReadFile(log)
	if strings.Contains(string(argv), "tok-relay-321") {
		t.Errorf("credential appeared in a git argv:\n%s", argv)
	}
}

// A redirect to another host must not carry the credential: the helper is
// scoped to the configured upstream URL.
func TestUpstreamCredential_notSentAcrossRedirect(t *testing.T) {
	argvLoggingGit(t)
	other := &authRecorder{}
	elsewhere := httptest.NewServer(http.HandlerFunc(other.handler))
	defer elsewhere.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path+"?"+r.URL.RawQuery, http.StatusFound)
	}))
	defer upstream.Close()

	_, _ = remoteHeads(upstream.URL+"/org/repo.git", "tok-redirect-555")
	other.mu.Lock()
	n := other.requests
	other.mu.Unlock()
	if n == 0 {
		t.Fatal("git did not follow the redirect; the test proves nothing")
	}
	for _, s := range other.got() {
		if strings.Contains(s, "tok-redirect-555") {
			t.Errorf("redirect target received the credential: %q", s)
		}
	}
}

func TestUpstreamGitEnv_noCredentialForSSHOrEmpty(t *testing.T) {
	for _, c := range []struct{ url, cred string }{
		{"ssh://git@host/r.git", "tok"},
		{"file:///tmp/r.git", "tok"},
		{"https://host/r.git", ""},
	} {
		env := strings.Join(upstreamGitEnv(c.url, c.cred), "\n")
		if strings.Contains(env, "NBG_UPSTREAM_USER=") || strings.Contains(env, "credential.") {
			t.Errorf("%s with cred %q got a credential helper", c.url, c.cred)
		}
	}
}
