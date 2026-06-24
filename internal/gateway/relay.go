// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// redactCred replaces the credential substring with *** so tokens never appear
// in error strings / logs.
func redactCred(s, cred string) string {
	if cred == "" {
		return s
	}
	return strings.ReplaceAll(s, cred, "***")
}

// urlUserinfoRe matches the userinfo component of http(s) URLs. ssh:// is
// deliberately not matched - its user (git@) is routing, not a secret.
var urlUserinfoRe = regexp.MustCompile(`(https?://)[^@/\s]+@`)

// redactURLUserinfo replaces any http(s) URL userinfo (user:token@ or bare
// token@) in s with ***@. Covers credentials embedded in the configured
// upstream URL itself, which redactCred can't know about.
func redactURLUserinfo(s string) string {
	return urlUserinfoRe.ReplaceAllString(s, "${1}***@")
}

// UpstreamURLHasEmbeddedToken reports whether an http(s) upstream URL carries a
// credential in its userinfo (user:token@ or token@). In the privilege-
// separated relay model the credential must live ONLY in the per-repo
// credential file (readable solely by the relay user) - never in the URL, since
// gateway.toml is also git-readable for gating, so a token there is a bypass.
func UpstreamURLHasEmbeddedToken(u string) bool {
	return urlUserinfoRe.MatchString(u)
}

// authedURL injects cred as a token into upstreamURL when both are non-empty
// and the URL uses http(s). For non-http schemes (file://, ssh://) the URL is
// returned unchanged - ssh URLs auth via the host's key store, not tokens.
//
// http:// is supported in addition to https:// for LAN gitea / on-prem setups
// where TLS termination happens upstream or isn't needed. The injected
// credential travels in cleartext on http; callers should prefer https.
func authedURL(upstreamURL, cred string) string {
	if cred == "" {
		return upstreamURL
	}
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(upstreamURL, scheme) {
			return scheme + cred + "@" + strings.TrimPrefix(upstreamURL, scheme)
		}
	}
	return upstreamURL
}

// Relay pushes the given ref updates from the gateway bare repo (gitDir) to the
// upstream. cred, when non-empty and the URL is http(s), is injected as a token.
// Returns an error if any ref fails to push (caller surfaces it + flags
// out-of-sync). Deletions (NewRev == zeroRev) are relayed as upstream deletes.
func Relay(upstreamURL, cred, gitDir string, refs []RefUpdate) error {
	// Guard: the relay holds the upstream credential, so it must deliver only
	// the gated repo's ACTUAL current state - never an arbitrary rev from a
	// (possibly forged or replayed) job. For each non-delete ref, require the
	// job's NewRev to equal the bare repo's current value for that ref; refuse
	// the entire relay otherwise (deliver nothing). git push alone does not
	// enforce this - it will happily deliver any rev whose object exists. This
	// is what keeps the post-receive -> relay handoff (incl. a socket-fed job
	// from a separate relay user) from being usable to push un-gated content.
	for _, r := range refs {
		if r.IsDelete() {
			continue // deletes carry no content to validate
		}
		cur, err := exec.Command("git", "--git-dir", gitDir, "rev-parse", "--verify", "-q", r.Name).Output()
		if err != nil {
			return fmt.Errorf("relay refused: ref %s not present in gated repo", r.Name)
		}
		if got := strings.TrimSpace(string(cur)); got != r.NewRev {
			return fmt.Errorf("relay refused: ref %s rev %s does not match gated repo state %s", r.Name, r.NewRev, got)
		}
	}
	url := authedURL(upstreamURL, cred)
	var specs []string
	for _, r := range refs {
		if r.IsDelete() {
			specs = append(specs, ":"+r.Name)
		} else {
			specs = append(specs, r.NewRev+":"+r.Name)
		}
	}
	if len(specs) == 0 {
		return nil
	}
	args := append([]string{"--git-dir", gitDir, "push", url}, specs...)
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		// This error reaches the pusher's terminal via post-receive - an
		// agent's context - so no credential may survive in it under any
		// config style: redact the known credential value AND any http(s)
		// userinfo (tokens embedded in the configured URL itself).
		msg := redactURLUserinfo(redactCred(string(out), cred))
		return fmt.Errorf("relay to %s failed: %w\n%s", redactURLUserinfo(upstreamURL), err, msg)
	}
	return nil
}
