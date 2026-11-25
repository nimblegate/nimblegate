// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitargs

import (
	"os/exec"
	"strings"
	"sync"
)

// maxAliasDepth bounds alias chains (ci -> c -> commit); git itself refuses
// loops, so a real chain is short.
const maxAliasDepth = 5

// Resolve splits args (the argv after "git") and expands the verb through
// git aliases the way git does: an alias given as `-c alias.<verb>=...` or
// set in any config git reads from dir. A plain alias is split like a shell
// would and its words placed before the remaining args; a `!` alias is
// parsed when it runs `git ...`, and otherwise read for a `commit` or
// `push` word. Builtin verbs are never expanded, because git ignores an
// alias that shadows a builtin.
func Resolve(dir string, args []string) (globals []string, verb string, rest []string) {
	globals, verb, rest = Split(args)
	for depth := 0; depth < maxAliasDepth && verb != ""; depth++ {
		val, ok := lookupAlias(dir, globals, verb)
		if !ok || isBuiltin(verb) {
			return globals, verb, rest
		}
		words := ShellWords(strings.TrimPrefix(val, "!"))
		if strings.HasPrefix(val, "!") {
			i := indexOf(words, "git")
			if i < 0 {
				for j, w := range words {
					if w == "commit" || w == "push" {
						return globals, w, append(append([]string{}, words[j+1:]...), rest...)
					}
				}
				return globals, verb, rest
			}
			words = words[i+1:]
		}
		g2, v2, r2 := Split(words)
		globals = append(append([]string{}, globals...), g2...)
		verb = v2
		rest = append(append([]string{}, r2...), rest...)
	}
	return globals, verb, rest
}

func lookupAlias(dir string, globals []string, verb string) (string, bool) {
	cmd := exec.Command("git", append(append([]string{}, globals...), "config", "--get", "alias."+verb)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	val := strings.TrimSpace(string(out))
	return val, val != ""
}

var (
	builtinsOnce sync.Once
	builtins     map[string]bool
)

// isBuiltin asks git once for its builtin commands. If git cannot say, the
// verbs the frames care about still count as builtins.
func isBuiltin(verb string) bool {
	builtinsOnce.Do(func() {
		builtins = map[string]bool{"commit": true, "push": true}
		out, err := exec.Command("git", "--list-cmds=builtins").Output()
		if err != nil {
			return
		}
		for _, c := range strings.Fields(string(out)) {
			builtins[c] = true
		}
	})
	return builtins[verb]
}

func indexOf(words []string, w string) int {
	for i, x := range words {
		if x == w {
			return i
		}
	}
	return -1
}

// ShellWords splits s into words the way a POSIX shell would for quoting:
// single quotes are literal, double quotes allow backslash escapes, and an
// unquoted backslash escapes the next character. Expansions are not done.
func ShellWords(s string) []string {
	var words []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'':
			inWord = true
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				cur.WriteString(s[i+1:])
				i = len(s)
				break
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case c == '"':
			inWord = true
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
		case c == '\\' && i+1 < len(s):
			inWord = true
			i++
			cur.WriteByte(s[i])
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			inWord = true
			cur.WriteByte(c)
		}
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words
}
