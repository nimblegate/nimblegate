// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
)

// Git is the internal handler invoked by the git-wrap shell function.
// It runs git-wrap-targeted checks against the would-be git invocation, then
// (if checks pass) execs the real git binary.
//
// Implemented as a thin wrapper around interceptAndExec; see intercept.go.
func Git(args []string) int {
	fs := flag.NewFlagSet("git", flag.ExitOnError)
	forceYes := fs.Bool("force-yes", false, "bypass nimblegate checks; recorded to audit log")
	reason := fs.String("reason", "", "human-readable reason for --force-yes (recorded)")
	_ = fs.Parse(args)

	gitArgs := fs.Args()
	if len(gitArgs) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate git: no git subcommand provided")
		return 2
	}

	return interceptAndExec(interceptOptions{
		cmdName:       "git",
		cmdArgs:       gitArgs,
		label:         "nimblegate git",
		forceYes:      *forceYes,
		reason:        *reason,
		resolveBinary: resolveGitBinary,
	})
}
