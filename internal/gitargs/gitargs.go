// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package gitargs reads a git command line the way git does, so checks see
// the subcommand and its options whatever global options come first and
// however the options are spelled.
package gitargs

import "strings"

// globalsWithValue are git's global options that take the next argument as
// their value when written without "=".
var globalsWithValue = map[string]bool{
	"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
	"--namespace": true, "--config-env": true, "--super-prefix": true,
	"--list-cmds": true, "--attr-source": true,
}

// Split separates git's global options from the subcommand. args excludes
// the leading "git": ["-C", "d", "commit", "-n"] gives globals ["-C", "d"],
// verb "commit", rest ["-n"]. verb is "" when there is no subcommand.
func Split(args []string) (globals []string, verb string, rest []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return args[:i], a, args[i+1:]
		}
		if globalsWithValue[a] && i+1 < len(args) {
			i++
		}
	}
	return args, "", nil
}

// Command splits a whole command string ("git -C d commit -n") into its
// parts. ok is false when the command is not git.
func Command(cmd string) (globals []string, verb string, rest []string, ok bool) {
	f := strings.Fields(cmd)
	if len(f) == 0 || f[0] != "git" {
		return nil, "", nil, false
	}
	g, v, r := Split(f[1:])
	return g, v, r, true
}

// commitShortWithValue are `git commit` short options whose value follows,
// attached or as the next argument; the rest of a bundle after one of them
// is that value, not more flags.
const commitShortWithValue = "CcFmt"

// commitShortOptionalValue take an optional value attached in the bundle.
const commitShortOptionalValue = "uS"

// commitLongWithValue are long options whose value may be the next argument.
var commitLongWithValue = map[string]bool{
	"--message": true, "--file": true, "--author": true, "--date": true,
	"--template": true, "--reuse-message": true, "--reedit-message": true,
	"--fixup": true, "--squash": true, "--cleanup": true,
	"--pathspec-from-file": true, "--trailer": true,
}

// CommitSkipsHooks reports whether `git <globals> commit <rest>` skips the
// pre-commit hook, and the argument that does it. Covered: -n alone or in a
// bundle (-nm msg, -an), any prefix of --no-verify git would accept
// (--no-veri), and a -c / --config-env that sets core.hooksPath.
func CommitSkipsHooks(globals, rest []string) (string, bool) {
	for i := 0; i < len(globals); i++ {
		g := globals[i]
		val := ""
		switch {
		case (g == "-c" || g == "--config-env") && i+1 < len(globals):
			val = globals[i+1]
			i++
		case strings.HasPrefix(g, "--config-env="):
			val = strings.TrimPrefix(g, "--config-env=")
		default:
			continue
		}
		if strings.HasPrefix(strings.ToLower(val), "core.hookspath") {
			return g + " " + val, true
		}
	}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		switch {
		case a == "--":
			return "", false
		case strings.HasPrefix(a, "--"):
			name, _, hasValue := strings.Cut(a, "=")
			if !hasValue && len(name) >= len("--no-v") && strings.HasPrefix("--no-verify", name) {
				return a, true
			}
			if !hasValue && commitLongWithValue[name] {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j, c := range a[1:] {
				if c == 'n' {
					return a, true
				}
				if strings.ContainsRune(commitShortWithValue, c) {
					if j == len(a)-2 {
						i++ // value is the next argument
					}
					break
				}
				if strings.ContainsRune(commitShortOptionalValue, c) {
					break
				}
			}
		}
	}
	return "", false
}
