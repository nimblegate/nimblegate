// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "testing"

func TestIsLocalUpstream(t *testing.T) {
	for url, want := range map[string]bool{
		"/srv/mirror.git":                true,
		"file:///srv/mirror.git":         true,
		"mirror.git":                     false,
		"../mirror.git":                  false,
		"git@example.test:x/y.git":       false,
		"ssh://git@example.test/x/y.git": false,
		"https://example.test/x/y.git":   false,
		"":                               false,
	} {
		if got := IsLocalUpstream(url); got != want {
			t.Errorf("IsLocalUpstream(%q) = %v, want %v", url, got, want)
		}
	}
	if got := LocalUpstreamPath("file:///srv/mirror.git"); got != "/srv/mirror.git" {
		t.Errorf("LocalUpstreamPath = %q", got)
	}
}
