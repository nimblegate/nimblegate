// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import "testing"

// TestIsSSHUpstream covers every reasonable upstream URL shape an operator
// might paste into the registration form. SSH shapes return true (no
// per-repo credential needed); HTTP and other shapes return false (PAT
// stored at <policy-root>/<repo>/credential mode 0600).
func TestIsSSHUpstream(t *testing.T) {
	cases := []struct {
		url  string
		want bool
		why  string
	}{
		// ssh:// scheme variants
		{"ssh://git@192.0.2.20/you/x.git", true, "ssh scheme, no explicit port"},
		{"ssh://git@192.0.2.20:22/you/x.git", true, "ssh scheme, explicit port"},
		{"ssh://git@github.com/owner/repo.git", true, "ssh scheme, hostname"},

		// scp-style - what gitea's clone-URL UI emits by default
		{"git@192.0.2.20:you/x.git", true, "scp-style with IP host"},
		{"git@github.com:owner/repo.git", true, "scp-style with hostname"},
		{"git@gitea.internal:you/ai-assistant.git", true, "scp-style, multi-label host"},
		{"user@host:path", true, "scp-style, minimal"},

		// HTTP - PAT-based, credential file applicable
		{"https://gitea.internal/you/x.git", false, "https → expects credential"},
		{"https://github.com/owner/repo.git", false, "https github"},
		{"http://gitea.internal/you/x.git", false, "http (insecure) but still PAT path"},

		// Pathological / edge cases - default to "needs credential" so the
		// unset badge surfaces as a problem state rather than silently
		// hiding it.
		{"", false, "empty URL"},
		{"file:///srv/local/x.git", false, "file scheme"},
		{"git://host/x.git", false, "git daemon scheme"},
		{"just-a-name", false, "no scheme, no @host pattern"},
		{"host:/path", false, "no @, not scp-style"},
		{"@:", false, "structurally degenerate"},

		// Tricky: ssh:// URL whose path contains @ - colon comes after slash, so
		// we shouldn't get tricked into thinking it's scp-style.
		{"https://user:pat@gitea.internal/x.git", false, "https with embedded creds, still PAT path"},
	}
	for _, c := range cases {
		if got := isSSHUpstream(c.url); got != c.want {
			t.Errorf("isSSHUpstream(%q) = %v, want %v (%s)", c.url, got, c.want, c.why)
		}
	}
}
