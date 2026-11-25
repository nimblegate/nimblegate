// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitargs

import (
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	cases := []struct{ cmd, verb, rest string }{
		{"git commit -m x", "commit", "-m x"},
		{"git -C /tmp/r commit --no-verify", "commit", "--no-verify"},
		{"git -c user.name=a --no-pager -C d push origin main", "push", "origin main"},
		{"git --git-dir=/r/.git push", "push", ""},
		{"git --git-dir /r/.git --work-tree /r commit -n", "commit", "-n"},
		{"git --version", "", ""},
	}
	for _, c := range cases {
		_, verb, rest, ok := Command(c.cmd)
		if !ok || verb != c.verb || strings.Join(rest, " ") != c.rest {
			t.Errorf("%q: verb %q rest %q, want %q %q", c.cmd, verb, rest, c.verb, c.rest)
		}
	}
	if _, _, _, ok := Command("rm -rf x"); ok {
		t.Error("non-git command reported as git")
	}
}

func TestCommitSkipsHooks(t *testing.T) {
	skips := []string{
		"git commit --no-verify -m x",
		"git commit -n -m x",
		"git commit -nm msg",
		"git commit -anm msg",
		"git commit -an",
		"git commit --no-veri -m x",
		"git commit --no-verif",
		"git commit --no-v",
		"git -C /tmp/r commit --no-verify",
		"git -c core.hooksPath=/dev/null commit -m x",
		"git -c core.hookspath=/tmp commit",
		"git --config-env=core.hooksPath=EMPTY commit",
		"git -C d -c core.hooksPath=/x commit",
	}
	for _, cmd := range skips {
		g, _, r, _ := Command(cmd)
		if _, ok := CommitSkipsHooks(g, r); !ok {
			t.Errorf("%q skips the hook but was not caught", cmd)
		}
	}
	keeps := []string{
		"git commit -m x",
		"git commit -mn",          // message "n"
		"git commit -m -n",        // message "-n"
		"git commit --message -n", // message "-n"
		"git commit -am nothing",  // n only inside the message
		"git commit -F notes.txt",
		"git commit --no-edit --amend", // other --no- options
		"git commit --no-verbose",
		"git commit -- -n", // a pathspec named -n
		"git commit --author=n -m x",
		"git -c user.name=n commit -m x",
		"git commit -S -m x",
		"git commit -unormal -m x",
	}
	for _, cmd := range keeps {
		g, _, r, _ := Command(cmd)
		if arg, ok := CommitSkipsHooks(g, r); ok {
			t.Errorf("%q does not skip the hook but was flagged on %q", cmd, arg)
		}
	}
}
