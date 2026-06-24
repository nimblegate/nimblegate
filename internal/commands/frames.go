// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
)

// Frames is the dispatcher for `nimblegate frames <sub>`.
func Frames(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate frames {list|enable|disable} [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return List(args[1:])
	case "enable":
		return Enable(args[1:])
	case "disable":
		return Disable(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "nimblegate frames: unknown subcommand %q\n", args[0])
		return 2
	}
}
