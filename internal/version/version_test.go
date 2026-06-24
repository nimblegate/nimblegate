// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package version

import "testing"

func TestVersionIsNonEmpty(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must not be empty")
	}
}

func TestResolve(t *testing.T) {
	cases := []struct {
		name, ldflag, rev string
		modified          bool
		want              string
	}{
		{"ldflag override wins over vcs", "8fff140", "abcdef1234567890", true, "8fff140"},
		{"no ldflag → short vcs rev", "0.0.0-dev", "abcdef1234567890", false, "abcdef1"},
		{"no ldflag → vcs rev + dirty", "0.0.0-dev", "abcdef1234567890", true, "abcdef1-dirty"},
		{"no ldflag, no vcs → default", "0.0.0-dev", "", false, "0.0.0-dev"},
		{"short rev left intact", "0.0.0-dev", "abc", false, "abc"},
		{"empty ldflag, no vcs → empty", "", "", false, ""},
	}
	for _, c := range cases {
		if got := resolve(c.ldflag, c.rev, c.modified); got != c.want {
			t.Errorf("%s: resolve(%q,%q,%v) = %q, want %q", c.name, c.ldflag, c.rev, c.modified, got, c.want)
		}
	}
}
