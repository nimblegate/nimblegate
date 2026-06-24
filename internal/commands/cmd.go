// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
)

// Cmd is the internal handler invoked by the generic command-wrap shell
// functions installed by `nimblegate shell install` (e.g. apt, apt-get).
// It runs git-wrap-triggered checks against the would-be command invocation,
// then (if checks pass) execs the real command.
//
// Usage:
//
//	nimblegate cmd [--force-yes] [--reason="..."] <command> [args...]
//
// Same --force-yes / --reason semantics as `nimblegate git`.
func Cmd(args []string) int {
	fs := flag.NewFlagSet("cmd", flag.ExitOnError)
	forceYes := fs.Bool("force-yes", false, "bypass nimblegate checks; recorded to audit log")
	reason := fs.String("reason", "", "human-readable reason for --force-yes (recorded)")
	_ = fs.Parse(args)

	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate cmd: no command provided")
		fmt.Fprintln(os.Stderr, "Usage: nimblegate cmd [--force-yes --reason=\"...\"] <command> [args...]")
		return 2
	}

	cmdName := rest[0]
	cmdArgs := rest[1:]

	return interceptAndExec(interceptOptions{
		cmdName:       cmdName,
		cmdArgs:       cmdArgs,
		label:         "nimblegate cmd",
		forceYes:      *forceYes,
		reason:        *reason,
		resolveBinary: resolveBinaryFromPATH,
	})
}
