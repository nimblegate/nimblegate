// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"fmt"
	"net"
)

// The relay service is the privilege boundary for upstream delivery. It runs as
// a dedicated relay user that alone can read the upstream credential; the git
// user (which receives dev/agent pushes and runs the post-receive hook) cannot.
// The hook hands a credential-free job - repo + accepted ref updates - to the
// service over a local Unix-domain socket and blocks for the result, so the
// pusher still gets immediate relay-failure feedback while git never touches
// the credential. The service calls Relay(), which refuses any rev that does
// not match the gated repo's current state, so a forged or replayed job cannot
// make the service push un-gated content.

// relayRequest is the credential-free job the git-side hook sends. The service
// looks up the repo's upstream + credential itself (Resolve) - the wire carries
// no secret, only what to relay.
type relayRequest struct {
	Repo string      `json:"repo"`
	Refs []RefUpdate `json:"refs"`
}

// relayResponse is the result returned to the hook. Error is already
// credential-redacted by Relay(); it reaches the pusher's terminal.
type relayResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// RelayService accepts relay jobs on a Unix socket and performs the upstream
// push. It holds the credential indirectly via Resolve; callers (the hook) do
// not.
type RelayService struct {
	// Resolve maps a logical repo name to its bare-repo dir, upstream URL, and
	// credential. In production it reads the repo's gateway.toml (readable only
	// by the relay user); in tests it is a stub. A non-nil error rejects the job
	// without revealing why beyond the repo name.
	Resolve func(repo string) (bareDir, upstreamURL, cred string, err error)
}

// Serve accepts and handles one job per connection until ln is closed. Each
// connection carries one JSON request and receives one JSON response.
func (s *RelayService) Serve(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *RelayService) handle(conn net.Conn) {
	defer conn.Close()
	var req relayRequest
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		writeResp(conn, relayResponse{Error: "relay: malformed request"})
		return
	}
	writeResp(conn, s.run(req))
}

// run resolves the repo and relays its accepted refs. Kept separate from the
// connection handling so it is unit-testable and so no credential ever enters a
// response (Resolve errors name only the repo; Relay errors are pre-redacted).
func (s *RelayService) run(req relayRequest) relayResponse {
	bareDir, url, cred, err := s.Resolve(req.Repo)
	if err != nil {
		return relayResponse{Error: fmt.Sprintf("relay: cannot resolve repo %q", req.Repo)}
	}
	if err := Relay(url, cred, bareDir, req.Refs); err != nil {
		return relayResponse{Error: err.Error()}
	}
	return relayResponse{OK: true}
}

func writeResp(conn net.Conn, r relayResponse) {
	_ = json.NewEncoder(conn).Encode(r)
}

// NewRepoResolver builds the production Resolve for a RelayService running on
// the gateway: a repo name maps to its symlink-safe, root-confined bare dir
// (resolveRepoBare), its upstream URL (gateway.toml), and its credential (the
// per-repo credential file, readable only by the relay user). Tests inject
// their own Resolve instead.
func NewRepoResolver(reposRoot, policyRoot string) func(repo string) (string, string, string, error) {
	return func(repo string) (string, string, string, error) {
		bareDir, err := resolveRepoBare(reposRoot, repo)
		if err != nil {
			return "", "", "", err
		}
		pol, err := (FilePolicyStore{Root: policyRoot}).Load(repo)
		if err != nil {
			return "", "", "", fmt.Errorf("load policy: %w", err)
		}
		cred, err := (FileCredentialStore{Root: policyRoot}).Load(repo)
		if err != nil {
			return "", "", "", fmt.Errorf("load credential: %w", err)
		}
		return bareDir, pol.UpstreamURL, cred, nil
	}
}

// RelayViaSocket is the git-side client: it hands a credential-free relay job to
// the service at socketPath and blocks for the result. Used by the post-receive
// hook so the relay runs under the relay user, not git. Returns the (redacted)
// relay error on failure so the pusher still sees why a relay failed.
func RelayViaSocket(socketPath, repo string, refs []RefUpdate) error {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return fmt.Errorf("relay service unreachable at %s: %w", socketPath, err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(relayRequest{Repo: repo, Refs: refs}); err != nil {
		return fmt.Errorf("relay request send failed: %w", err)
	}
	var resp relayResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("relay service gave no response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("%s", resp.Error)
	}
	return nil
}
