// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
)

// When a relay socket is configured, post-receive must route the relay through
// the service (git-side, carrying no credential) rather than relaying inline.
// The synchronous result still gates the exit code, preserving pusher feedback.
func TestRunPostReceive_relaysViaSocketWhenConfigured(t *testing.T) {
	bare, sha := makeBareWithCommit(t) // bare main = sha
	upstream := t.TempDir()
	mustGit(t, ".", "init", "--bare", "-q", upstream)
	sock := startTestRelayService(t, "demo", bare, "file://"+upstream, "")

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out strings.Builder
	// PolicyRoot/ReposRoot/SelfExe left empty -> scan-on-first-push is skipped,
	// keeping the test focused on relay routing.
	code := RunPostReceive(PostReceiveDeps{
		GitDir:      bare,
		Repo:        "demo",
		RelaySocket: sock,
	}, stdin, &out)
	if code != 0 {
		t.Fatalf("RunPostReceive code=%d, out=%q", code, out.String())
	}
	got := strings.TrimSpace(mustGit(t, ".", "--git-dir", upstream, "rev-parse", "refs/heads/main"))
	if got != sha {
		t.Fatalf("upstream main = %s, want %s relayed via socket", got, sha)
	}
}

// A relay-service failure over the socket must be SILENT to the pusher
// (revealing it would mark the gateway/relay) and recorded operator-side.
func TestRunPostReceive_socketRelayFailureSilentToPusher(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	sock := startTestRelayService(t, "demo", bare, "http://127.0.0.1:1/o/r.git", "") // unroutable
	policyRoot := t.TempDir()

	stdin := strings.NewReader(zeroRev + " " + sha + " refs/heads/main\n")
	var out strings.Builder
	code := RunPostReceive(PostReceiveDeps{GitDir: bare, Repo: "demo", RelaySocket: sock, PolicyRoot: policyRoot}, stdin, &out)
	if code != 0 {
		t.Errorf("relay failure must not fail the push to the pusher, got %d", code)
	}
	for _, leak := range []string{"WARNING", "relay", "upstream", "gateway", "FAILED"} {
		if strings.Contains(out.String(), leak) {
			t.Errorf("pusher output leaked %q:\n%s", leak, out.String())
		}
	}
	evs, _ := ReadEvents(policyRoot, func(e Event) bool { return e.Event == "relay-failed" })
	if len(evs) == 0 {
		t.Error("relay failure should be recorded as an operator event")
	}
}
