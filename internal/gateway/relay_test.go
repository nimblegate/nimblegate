// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthedURL(t *testing.T) {
	cases := []struct {
		name, url, cred, want string
	}{
		{"https with cred", "https://host/foo.git", "tok123", "https://tok123@host/foo.git"},
		{"http with cred (lan gitea)", "http://192.0.2.20:3000/you/nimblegate.git", "tok123", "http://tok123@192.0.2.20:3000/you/nimblegate.git"},
		{"https no cred", "https://host/foo.git", "", "https://host/foo.git"},
		{"http no cred", "http://host/foo.git", "", "http://host/foo.git"},
		{"ssh with cred (cred ignored)", "ssh://git@host/foo.git", "tok123", "ssh://git@host/foo.git"},
		{"file with cred (cred ignored)", "file:///tmp/foo.git", "tok123", "file:///tmp/foo.git"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := authedURL(c.url, c.cred); got != c.want {
				t.Errorf("authedURL(%q, %q) = %q, want %q", c.url, c.cred, got, c.want)
			}
		})
	}
}

func TestRedactCred(t *testing.T) {
	result := redactCred("pushing to https://ghp_abc@host", "ghp_abc")
	if strings.Contains(result, "ghp_abc") {
		t.Errorf("redactCred did not redact: %q", result)
	}
	if !strings.Contains(result, "***") {
		t.Errorf("redactCred missing replacement: %q", result)
	}
	if got := redactCred("x", ""); got != "x" {
		t.Errorf("redactCred with empty cred: got %q, want %q", got, "x")
	}
}

func TestRelay_deletesUpstreamRef(t *testing.T) {
	gatewayBare, sha := makeBareWithCommit(t)
	upstream := t.TempDir()
	if err := exec.Command("git", "init", "--bare", "-q", upstream).Run(); err != nil {
		t.Fatal(err)
	}
	// First relay: create the ref on upstream.
	if err := Relay("file://"+upstream, "", gatewayBare, []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: sha},
	}); err != nil {
		t.Fatalf("Relay create: %v", err)
	}
	// Second relay: delete the ref.
	if err := Relay("file://"+upstream, "", gatewayBare, []RefUpdate{
		{Name: "refs/heads/main", OldRev: sha, NewRev: zeroRev},
	}); err != nil {
		t.Fatalf("Relay delete: %v", err)
	}
	// The ref must be gone on upstream.
	if err := exec.Command("git", "--git-dir", upstream, "rev-parse", "refs/heads/main").Run(); err == nil {
		t.Error("refs/heads/main should be gone after delete relay, but it still exists")
	}
}

func TestRelay_pushesAcceptedRefs(t *testing.T) {
	gatewayBare, sha := makeBareWithCommit(t)
	upstream := t.TempDir()
	if err := exec.Command("git", "init", "--bare", "-q", upstream).Run(); err != nil {
		t.Fatal(err)
	}
	err := Relay("file://"+upstream, "", gatewayBare, []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: sha},
	})
	if err != nil {
		t.Fatalf("Relay: %v", err)
	}
	out, err := exec.Command("git", "--git-dir", upstream, "rev-parse", "refs/heads/main").Output()
	if err != nil {
		t.Fatalf("upstream rev-parse: %v", err)
	}
	if string(out[:40]) != sha {
		t.Errorf("upstream main = %s, want %s", out[:40], sha)
	}
}

func TestRedactURLUserinfo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://user:tok123@host:3000/org/repo.git", "http://***@host:3000/org/repo.git"},
		{"https://tok123@github.com/org/repo.git", "https://***@github.com/org/repo.git"},
		{"http://192.0.2.20:3000/org/repo.git", "http://192.0.2.20:3000/org/repo.git"},
		{"ssh://git@host/repo.git", "ssh://git@host/repo.git"}, // ssh userinfo is routing, not secret
		{"fatal: unable to access 'http://u:t@h/r.git/': 401", "fatal: unable to access 'http://***@h/r.git/': 401"},
	}
	for _, c := range cases {
		if got := redactURLUserinfo(c.in); got != c.want {
			t.Errorf("redactURLUserinfo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A failed relay against a token-in-URL upstream must not leak the token in
// the returned error - neither from the URL echo nor from git's own output.
func TestRelay_errorNeverLeaksURLToken(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	// Unroutable host: git fails fast with an error that echoes the URL.
	err := Relay("http://you:sekrit-tok@127.0.0.1:1/org/repo.git", "", bare,
		[]RefUpdate{{Name: "refs/heads/main", OldRev: zeroRev, NewRev: sha}})
	if err == nil {
		t.Fatal("relay to unroutable host should fail")
	}
	if strings.Contains(err.Error(), "sekrit-tok") {
		t.Errorf("relay error leaked the URL-embedded token:\n%s", err)
	}
	if !strings.Contains(err.Error(), "***@") {
		t.Errorf("expected redaction marker in error:\n%s", err)
	}
}

func TestUpstreamURLHasEmbeddedToken(t *testing.T) {
	for _, u := range []string{"http://u:tok@h/r.git", "https://tok@h/r.git"} {
		if !UpstreamURLHasEmbeddedToken(u) {
			t.Errorf("%q should be detected as token-bearing", u)
		}
	}
	for _, u := range []string{"https://h/r.git", "ssh://git@h/r.git", "file:///srv/up.git"} {
		if UpstreamURLHasEmbeddedToken(u) {
			t.Errorf("%q should NOT be flagged (no embedded http token)", u)
		}
	}
}

// A forged/replayed relay job naming a rev that EXISTS in the gated bare repo
// but is NOT that ref's current value must be REFUSED. git push alone won't
// catch it (the object exists, so it would deliver the wrong rev upstream); the
// relay must bind to the gated repo's actual current ref state. This guard is
// what keeps the post-receive -> relay handoff (esp. a socket-fed job from a
// separate relay user) from being usable as a bypass.
func TestRelay_refusesRevMismatchingGatedState(t *testing.T) {
	bare, _ := makeBareWithCommit(t) // bare refs/heads/main = sha
	upstream := t.TempDir()
	if err := exec.Command("git", "init", "--bare", "-q", upstream).Run(); err != nil {
		t.Fatal(err)
	}
	// Make a SECOND real commit, present in the bare repo on a different ref,
	// so its rev is a valid object yet not the value of main.
	work := t.TempDir()
	mustGit(t, work, "clone", "-q", bare, work)
	if err := os.WriteFile(filepath.Join(work, "other.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-qm", "second")
	other := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	mustGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/other") // bare now holds `other`

	// Forged job: relay the OTHER rev (a real object in the bare repo) onto
	// main, which is still at sha. Must be refused, and nothing delivered.
	err := Relay("file://"+upstream, "", bare, []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: other},
	})
	if err == nil {
		t.Fatal("relay should refuse a rev that doesn't match the gated repo's current main")
	}
	if got, _ := exec.Command("git", "--git-dir", upstream, "rev-parse", "--verify", "-q", "refs/heads/main").Output(); len(got) > 0 {
		t.Fatalf("upstream received a ref from a forged relay job: %s", got)
	}
}
