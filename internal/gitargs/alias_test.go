// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitargs

import (
	"os/exec"
	"reflect"
	"testing"
)

func repoWithAliases(t *testing.T, aliases map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for k, v := range aliases {
		if out, err := exec.Command("git", "-C", dir, "config", "alias."+k, v).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v\n%s", err, out)
		}
	}
	return dir
}

func TestResolve_Aliases(t *testing.T) {
	dir := repoWithAliases(t, map[string]string{
		"ci":     "commit -n",
		"c":      "commit",
		"cn":     "c --no-verify",
		"sc":     "!git commit --no-verify",
		"fn":     `!f() { git commit -n "$@"; }; f`,
		"pf":     "push --force",
		"status": "commit -n", // shadows a builtin: git ignores it
	})
	skips := [][]string{
		{"ci", "-m", "x"},
		{"cn", "-m", "x"},
		{"sc", "-m", "x"},
		{"fn", "-m", "x"},
		{"c", "-n", "-m", "x"},
		{"-c", "alias.zz=commit -n", "zz", "-m", "x"},
	}
	for _, args := range skips {
		g, verb, rest := Resolve(dir, args)
		if verb != "commit" {
			t.Errorf("%v: verb %q, want commit", args, verb)
			continue
		}
		if _, ok := CommitSkipsHooks(g, rest); !ok {
			t.Errorf("%v resolved to commit %v but the skip was not seen", args, rest)
		}
	}
	if _, verb, rest := Resolve(dir, []string{"pf", "origin", "main"}); verb != "push" || !reflect.DeepEqual(rest, []string{"--force", "origin", "main"}) {
		t.Errorf("pf: verb %q rest %v", verb, rest)
	}
	if _, verb, _ := Resolve(dir, []string{"status"}); verb != "status" {
		t.Errorf("an alias shadowing a builtin must not expand; got %q", verb)
	}
	g, verb, rest := Resolve(dir, []string{"c", "-m", "fix -n handling"})
	if _, ok := CommitSkipsHooks(g, rest); verb != "commit" || ok {
		t.Errorf("message containing -n flagged: verb %q rest %v", verb, rest)
	}
}

func TestShellWords(t *testing.T) {
	cases := map[string][]string{
		`git commit -m "a b"`:       {"git", "commit", "-m", "a b"},
		`a 'b c' d\ e`:              {"a", "b c", "d e"},
		`x "q\"uote" ''`:            {"x", `q"uote`, ""},
		`f() { git commit -n; }; f`: {"f()", "{", "git", "commit", "-n;", "};", "f"},
	}
	for in, want := range cases {
		if got := ShellWords(in); !reflect.DeepEqual(got, want) {
			t.Errorf("ShellWords(%q) = %q, want %q", in, got, want)
		}
	}
}
