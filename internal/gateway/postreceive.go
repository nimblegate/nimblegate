// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PostReceiveDeps are the injected dependencies for one repo's post-receive run.
type PostReceiveDeps struct {
	Policy     Policy
	GitDir     string
	Cred       string
	Repo       string // logical repo name (for scan-on-first-push)
	PolicyRoot string // per-repo policy dir root
	ReposRoot  string // bare-repo root (for scan-on-first-push)
	SelfExe    string // path to the running nimblegate binary
	// RelaySocket, when set, routes the upstream relay through the relay service
	// at this Unix-socket path instead of relaying inline. The service (running
	// as the relay user) holds the credential; the git-side hook does not. The
	// call still blocks for the result, so the pusher gets synchronous feedback.
	// Empty = legacy inline relay (git holds the credential).
	RelaySocket string
}

// RunPostReceive relays accepted refs to upstream. Runs only after pre-receive
// accepted (git skips post-receive on rejection).
func RunPostReceive(d PostReceiveDeps, stdin io.Reader, stdout io.Writer) int {
	refs, err := parseRefLines(stdin)
	if err != nil {
		fmt.Fprintf(stdout, "error: cannot read refs: %v\n", err)
		return 1
	}
	var relayErr error
	if d.RelaySocket != "" {
		// Privilege-separated relay: hand the job to the relay service (which
		// holds the credential); git never reads it. Blocks for the result.
		relayErr = RelayViaSocket(d.RelaySocket, d.Repo, refs)
	} else {
		// Legacy inline relay: this process (git) reads the credential.
		relayErr = Relay(d.Policy.UpstreamURL, d.Cred, d.GitDir, refs)
	}
	if relayErr != nil {
		// The push already succeeded from the client's view; a normal git host
		// doesn't report internal replication failures, and naming the relay
		// would reveal the gateway. Record it operator-side (the reconciler
		// recovers it) and say NOTHING to the pusher. git ignores post-receive
		// exit codes, so the push still shows success.
		if d.PolicyRoot != "" {
			_ = AppendEvent(d.PolicyRoot, Event{Event: "relay-failed", Repo: d.Repo, OK: false, Payload: map[string]any{"error": relayErr.Error()}})
		}
		return 0
	}
	// Relay succeeded: record it so the dashboard can show the repo's LATEST
	// relay outcome (ok vs failed). Without this the relay only ever logged
	// failures, so a since-recovered relay looked permanently broken.
	if d.PolicyRoot != "" && len(refs) > 0 {
		_ = AppendEvent(d.PolicyRoot, Event{Event: "relay-ok", Repo: d.Repo, OK: true})
	}
	// Scan-on-first-push: best-effort. Errors logged, never block the push
	// (relay already succeeded). Only runs when the rec file is absent - the
	// "Rescan" path overwrites it via a different handler.
	if d.Repo != "" && d.PolicyRoot != "" && d.ReposRoot != "" && d.SelfExe != "" {
		recPath := filepath.Join(d.PolicyRoot, d.Repo, "scan-recommendation.json")
		if _, err := os.Stat(recPath); os.IsNotExist(err) {
			if err := ScanFirstPush(d.GitDir, d.Repo, d.PolicyRoot, d.SelfExe); err != nil {
				// git relays post-receive stderr to the client too - keep this
				// operator-side so nothing leaks to the pusher.
				_ = AppendEvent(d.PolicyRoot, Event{Event: "scan-first-push", Repo: d.Repo, OK: false, Payload: map[string]any{"error": err.Error()}})
			} else {
				_ = AppendEvent(d.PolicyRoot, Event{
					Event: "scan-first-push",
					Repo:  d.Repo,
					OK:    true,
				})
			}
		}
	}
	return 0
}
