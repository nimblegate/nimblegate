// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// startTestRelayService spins up a RelayService on a Unix socket in a temp dir
// and returns the socket path. Resolve maps the one known repo to its bare dir +
// upstream; an unknown repo errors.
func startTestRelayService(t *testing.T, repo, bareDir, upstreamURL, cred string) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "relay.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	svc := &RelayService{Resolve: func(r string) (string, string, string, error) {
		if r != repo {
			return "", "", "", os.ErrNotExist
		}
		return bareDir, upstreamURL, cred, nil
	}}
	go func() { _ = svc.Serve(ln) }()
	t.Cleanup(func() { _ = ln.Close() })
	return sock
}

// NewRepoResolver wires the symlink-safe resolver + gateway.toml + credential
// file into the service's Resolve. It returns the bare dir, upstream URL, and
// credential for an active repo.
func TestNewRepoResolver_resolvesActiveRepo(t *testing.T) {
	reposRoot := t.TempDir()
	policyRoot := t.TempDir()
	activateRepo(t, reposRoot, "demo")
	if err := (FilePolicyStore{Root: policyRoot}).Save(Policy{Repo: "demo", UpstreamURL: "https://git.example/o/demo.git"}); err != nil {
		t.Fatal(err)
	}
	if err := (FileCredentialStore{Root: policyRoot}).Save("demo", "tok123"); err != nil {
		t.Fatal(err)
	}
	bare, url, cred, err := NewRepoResolver(reposRoot, policyRoot)("demo")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	wantBare, _ := filepath.EvalSymlinks(filepath.Join(reposRoot, "_repos", "demo.git"))
	if bare != wantBare {
		t.Errorf("bare = %q, want %q", bare, wantBare)
	}
	if url != "https://git.example/o/demo.git" {
		t.Errorf("url = %q", url)
	}
	if cred != "tok123" {
		t.Errorf("cred = %q", cred)
	}
}

func TestNewRepoResolver_rejectsInactiveAndUnsafe(t *testing.T) {
	resolve := NewRepoResolver(t.TempDir(), t.TempDir())
	if _, _, _, err := resolve("ghost"); err == nil {
		t.Error("inactive repo must error")
	}
	if _, _, _, err := resolve("../escape"); err == nil {
		t.Error("unsafe name must error")
	}
}

// The hook (running as git) hands a valid accepted job to the service over the
// socket; the service (which alone holds the credential) relays it and the
// upstream receives the ref. git never touches the credential.
func TestRelayService_relaysAcceptedJob(t *testing.T) {
	bare, sha := makeBareWithCommit(t) // bare main = sha
	upstream := t.TempDir()
	mustGit(t, ".", "init", "--bare", "-q", upstream)
	sock := startTestRelayService(t, "demo", bare, "file://"+upstream, "")

	if err := RelayViaSocket(sock, "demo", []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: sha},
	}); err != nil {
		t.Fatalf("RelayViaSocket: %v", err)
	}
	got := strings.TrimSpace(mustGit(t, ".", "--git-dir", upstream, "rev-parse", "refs/heads/main"))
	if got != sha {
		t.Fatalf("upstream main = %s, want relayed %s", got, sha)
	}
}

// The no-bypass guard must hold ACROSS the socket: a forged job naming a
// real-but-wrong rev gets refused by the service, and nothing reaches upstream.
func TestRelayService_forgedJobRefusedOverSocket(t *testing.T) {
	bare, _ := makeBareWithCommit(t)
	upstream := t.TempDir()
	mustGit(t, ".", "init", "--bare", "-q", upstream)

	// second real rev in the bare repo, on a different ref
	work := t.TempDir()
	mustGit(t, work, "clone", "-q", bare, work)
	if err := os.WriteFile(filepath.Join(work, "x.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", ".")
	mustGit(t, work, "commit", "-qm", "second")
	other := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	mustGit(t, work, "push", "-q", "origin", "HEAD:refs/heads/other")

	sock := startTestRelayService(t, "demo", bare, "file://"+upstream, "")
	err := RelayViaSocket(sock, "demo", []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: other},
	})
	if err == nil {
		t.Fatal("service should refuse a forged rev over the socket")
	}
	if out, _ := tryGit(upstream, "--git-dir", upstream, "rev-parse", "--verify", "-q", "refs/heads/main"); strings.TrimSpace(out) != "" {
		t.Fatalf("upstream got a ref from a forged socket job: %s", out)
	}
}

// A relay failure returned over the socket must never carry the credential /
// token (the response reaches the git-side hook, an agent's context).
func TestRelayService_errorDoesNotLeakCredential(t *testing.T) {
	bare, sha := makeBareWithCommit(t) // main = sha, so the guard passes and we reach the push
	// Unroutable upstream with a token embedded in the URL; the push fails.
	sock := startTestRelayService(t, "demo", bare, "http://you:sekrit-tok@127.0.0.1:1/o/r.git", "")
	err := RelayViaSocket(sock, "demo", []RefUpdate{
		{Name: "refs/heads/main", OldRev: zeroRev, NewRev: sha},
	})
	if err == nil {
		t.Fatal("relay to unroutable upstream should fail")
	}
	if strings.Contains(err.Error(), "sekrit-tok") {
		t.Fatalf("socket response leaked the token:\n%s", err)
	}
}
