// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package gitwrap installs the shell function that intercepts git commands
// and routes destructive ones through `nimblegate git`.
package gitwrap

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	BeginMarker = "# nimblegate git-wrap BEGIN"
	EndMarker   = "# nimblegate git-wrap END"
)

// ShellSnippet returns the shell function definition for the given shell.
// Supports bash and zsh; falls back to bash syntax otherwise.
//
// The snippet installs two kinds of wrapper:
//
//  1. A `git()` function that routes destructive verbs (push, reset, branch,
//     clean, rebase, filter-branch, filter-repo, stash) through
//     `nimblegate git`. Non-destructive verbs pass through to the real git.
//
//  2. Wrappers for non-git tools whose destructive verbs need intercepting
//     (apt, apt-get). These route through `nimblegate cmd` which fires
//     git-wrap-triggered frames (`commands/apt-purge-preview` etc.).
//
// Both wrappers are guarded by BeginMarker/EndMarker for clean uninstall.
func ShellSnippet(shell string) string {
	return BeginMarker + `
# git: route destructive verbs through nimblegate; pass everything else through.
git() {
    case "$1" in
        push|reset|branch|clean|rebase|filter-branch|filter-repo|stash)
            command nimblegate git "$@"
            ;;
        *)
            command git "$@"
            ;;
    esac
}

# apt / apt-get: route destructive verbs through nimblegate; pass everything
# else through. Catches the V0 ` + "`commands/apt-purge-preview`" + ` frame.
apt() {
    case "$1" in
        purge|remove|autoremove)
            command nimblegate cmd apt "$@"
            ;;
        *)
            command apt "$@"
            ;;
    esac
}

apt-get() {
    case "$1" in
        purge|remove|autoremove)
            command nimblegate cmd apt-get "$@"
            ;;
        *)
            command apt-get "$@"
            ;;
    esac
}

# rm: route through nimblegate ONLY when a recursive flag is present, so
# common 'rm file.txt' invocations don't pay the nimblegate-invocation cost.
# Matches -r, -R, --recursive, and combined short flags like -rf / -fr / -Rf.
# Catches V0.5 filesystem/rm-rf-protected-paths.
rm() {
    for arg in "$@"; do
        case "$arg" in
            -r|-R|--recursive|-rf|-Rf|-fr|-fR|-rfv|-vfr|-rfi|-irf|-rRf|-rfR)
                command nimblegate cmd rm "$@"
                return $?
                ;;
            -[A-Za-z]*[rR]*[A-Za-z]*)
                command nimblegate cmd rm "$@"
                return $?
                ;;
        esac
    done
    command rm "$@"
}
` + EndMarker + "\n"
}

// RCFile returns the user's rc file for the given shell.
func RCFile(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "bash":
		return home + "/.bashrc", nil
	case "zsh":
		return home + "/.zshrc", nil
	}
	return "", fmt.Errorf("unknown shell: %s", shell)
}

// Install appends the shell snippet to the user's rc file if not already present.
func Install(shell string) error {
	rc, err := RCFile(shell)
	if err != nil {
		return err
	}
	existing, _ := os.ReadFile(rc)
	if strings.Contains(string(existing), BeginMarker) {
		return fmt.Errorf("already installed in %s", rc)
	}
	f, err := os.OpenFile(rc, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.WriteString("\n" + ShellSnippet(shell)); err != nil {
		return err
	}
	return nil
}

// Uninstall removes the snippet from the rc file (matching BEGIN/END markers).
func Uninstall(shell string) error {
	rc, err := RCFile(shell)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(rc)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var out []string
	inside := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, BeginMarker) {
			inside = true
			continue
		}
		if strings.Contains(line, EndMarker) {
			inside = false
			continue
		}
		if !inside {
			out = append(out, line)
		}
	}
	return os.WriteFile(rc, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}
