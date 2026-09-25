// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"net/url"
	"path/filepath"
	"strings"
)

// ValidUpstreamURL reports whether u is an upstream URL git can be handed
// safely: non-empty, not option-shaped (leading "-"), and not the
// "<helper>::<address>" remote-helper form (ext::, fd::, ...) that can run
// commands. Control characters are rejected too. Local paths and file:// stay
// valid - local upstreams are supported.
func ValidUpstreamURL(u string) bool {
	if u == "" || strings.HasPrefix(u, "-") {
		return false
	}
	if strings.ContainsFunc(u, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return false
	}
	if i := strings.Index(u, "::"); i > 0 && !strings.ContainsAny(u[:i], "/:@[") {
		return false
	}
	return true
}

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

// SameUpstream reports whether a and b name the same upstream repository, so
// the duplicate-upstream guard is not bypassed by spelling. Local upstreams
// compare by cleaned filesystem path (file:// or bare, trailing slash, ..).
// Remotes compare by lowercased host and path, ignoring scheme, port, userinfo
// and a trailing "/" or ".git" - so git@h:o/r, ssh://git@h/o/r.git and
// https://h/o/r all match.
func SameUpstream(a, b string) bool {
	ka, kb := upstreamKey(a), upstreamKey(b)
	return ka != "" && ka == kb
}

func upstreamKey(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if IsLocalUpstream(u) {
		return "local:" + filepath.Clean(LocalUpstreamPath(u))
	}
	var host, path string
	if pu, err := url.Parse(u); err == nil && pu.Scheme != "" && pu.Host != "" {
		host, path = pu.Hostname(), pu.Path
	} else if IsSSHUpstream(u) {
		hostPart, p, _ := strings.Cut(u[strings.Index(u, "@")+1:], ":")
		host, path = hostPart, p
	} else {
		return "raw:" + u
	}
	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	return "remote:" + strings.ToLower(host) + "/" + strings.Trim(path, "/")
}
