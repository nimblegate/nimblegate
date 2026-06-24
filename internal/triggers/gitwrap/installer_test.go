// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitwrap

import (
	"strings"
	"testing"
)

func TestShellSnippet_BashContainsGuard(t *testing.T) {
	s := ShellSnippet("bash")
	if !strings.Contains(s, "# nimblegate git-wrap BEGIN") {
		t.Errorf("missing begin marker; got: %s", s)
	}
	if !strings.Contains(s, "# nimblegate git-wrap END") {
		t.Errorf("missing end marker; got: %s", s)
	}
	if !strings.Contains(s, `command nimblegate`) {
		t.Errorf("wrapper does not call nimblegate via 'command' keyword")
	}
}

func TestShellSnippet_ZshSameStructure(t *testing.T) {
	s := ShellSnippet("zsh")
	if !strings.Contains(s, "# nimblegate git-wrap BEGIN") {
		t.Errorf("zsh snippet missing begin marker")
	}
}

func TestShellSnippet_UnknownShellFallsBackToBash(t *testing.T) {
	s := ShellSnippet("unknown")
	if !strings.Contains(s, "# nimblegate git-wrap BEGIN") {
		t.Errorf("fallback snippet missing begin marker")
	}
}

func TestShellSnippet_IncludesAptAndAptGetWrappers(t *testing.T) {
	s := ShellSnippet("bash")
	for _, want := range []string{
		"apt() {",
		"apt-get() {",
		"command nimblegate cmd apt ",
		"command nimblegate cmd apt-get ",
		"purge|remove|autoremove",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("snippet missing %q; got:\n%s", want, s)
		}
	}
}

func TestShellSnippet_GitWrapperUnchanged(t *testing.T) {
	s := ShellSnippet("bash")
	for _, want := range []string{
		"git() {",
		"command nimblegate git ",
		"push|reset|branch|clean|rebase|filter-branch|filter-repo|stash",
		"command git ",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("git wrapper regressed; missing %q", want)
		}
	}
}

func TestShellSnippet_IncludesRmWrapper(t *testing.T) {
	s := ShellSnippet("bash")
	for _, want := range []string{
		"rm() {",
		"command nimblegate cmd rm ",
		"command rm ",
		"-r|-R|--recursive",
		// Combined short flags must be present so common -rf / -fr typing
		// gets routed.
		"-rf",
		"-fr",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rm wrapper missing %q; got:\n%s", want, s)
		}
	}
}
