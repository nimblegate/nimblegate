// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "strings"

// IsSSHUpstream reports whether url is an SSH-shaped git remote - either
// the ssh:// scheme or the scp-style "<user>@<host>:<path>" form. Both
// authenticate via the gateway's SSH identity (deploy key / service
// account), so per-repo credential files aren't applicable.
//
// HTTP/HTTPS URLs return false: those use PAT-based relay where a
// credential file at <policy-root>/<repo>/credential is genuinely needed
// for push to succeed.
//
// Edge cases:
//   - Empty URL → false (no upstream configured; show as "unset" so the
//     operator notices the registration is incomplete)
//   - file:// or git:// → false (unsupported relay modes; treat as
//     "needs credential" until proven otherwise)
//   - URLs with no scheme AND no @host: pattern → false (probably typo)
func IsSSHUpstream(url string) bool {
	if url == "" {
		return false
	}
	if strings.HasPrefix(url, "ssh://") {
		return true
	}
	at := strings.Index(url, "@")
	colon := strings.Index(url, ":")
	slash := strings.Index(url, "/")
	if at > 0 && colon > at && (slash == -1 || colon < slash) {
		return true
	}
	return false
}
