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
//   - file:// → false; see IsLocalUpstream, which needs no credential either
//   - git:// → false (unsupported relay mode)
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

// IsLocalUpstream reports whether url names a repository on this machine: an
// absolute path or a file:// URL. The relay reaches it with a plain git push,
// so no credential is involved. A relative path is not local in this sense -
// it would resolve against whatever directory the relay happens to run in.
func IsLocalUpstream(url string) bool {
	return strings.HasPrefix(url, "file://") || strings.HasPrefix(url, "/")
}

// LocalUpstreamPath is the filesystem path a local upstream names.
func LocalUpstreamPath(url string) string { return strings.TrimPrefix(url, "file://") }
