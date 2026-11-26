// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/gitargs"
)

// gitCommand reads the git invocation the frame was asked about, with
// aliases resolved. It prefers the argv as received (ctx.Args), since the
// joined Command string loses quoting; from the CLI only the string exists.
// ok is false when the command is not git.
func gitCommand(ctx engine.CheckContext) (globals []string, verb string, rest []string, ok bool) {
	f := strings.Fields(ctx.Command)
	if len(f) == 0 || f[0] != "git" {
		return nil, "", nil, false
	}
	args := ctx.Args
	if len(args) == 0 {
		args = f[1:]
	}
	dir := ctx.WorkingDir
	if dir == "" {
		dir = ctx.ProjectRoot
	}
	g, v, r := gitargs.Resolve(dir, args)
	return g, v, r, true
}
