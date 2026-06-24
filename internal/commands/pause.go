// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"time"

	"nimblegate/internal/paths"
	"nimblegate/internal/state"
)

// Pause implements `nimblegate pause`. Scope (--global or --project) is
// REQUIRED - there's no implicit default because the blast radius differs
// substantially between the two and a wrong silent default would be the
// kind of footgun nimblegate exists to prevent.
func Pause(args []string) int {
	fs := flag.NewFlagSet("pause", flag.ContinueOnError)
	globalFlag := fs.Bool("global", false, "pause nimblegate for every onboarded project on this machine")
	projectFlag := fs.Bool("project", false, "pause nimblegate only for the current project")
	reasonFlag := fs.String("reason", "", "optional human-readable reason (recorded with the pause)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch {
	case *globalFlag && *projectFlag:
		fmt.Fprintln(os.Stderr, "nimblegate pause: --global and --project are mutually exclusive")
		return 2
	case !*globalFlag && !*projectFlag:
		fmt.Fprintln(os.Stderr, "nimblegate pause: scope required (--global or --project)")
		fmt.Fprintln(os.Stderr, "  --global   pause nimblegate everywhere on this machine")
		fmt.Fprintln(os.Stderr, "  --project  pause only the current project")
		return 2
	}

	store, err := state.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate pause: %v\n", err)
		return 2
	}
	now := time.Now()

	if *globalFlag {
		if err := store.PauseGlobal(*reasonFlag, now); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate pause: %v\n", err)
			return 2
		}
		fmt.Println("nimblegate paused globally: frame checks suspended on every onboarded project")
		fmt.Println("  state file:", store.GlobalStateFile())
		fmt.Println("  resume with: nimblegate resume --global")
		return 0
	}

	// --project: must be inside an onboarded project (appframes.toml ancestor).
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate pause: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate pause: %v\nHint: run from inside an nimblegate-onboarded project, or use --global.\n", err)
		return 2
	}
	if err := store.PauseProject(root, *reasonFlag, now); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate pause: %v\n", err)
		return 2
	}
	fmt.Printf("nimblegate paused for project: %s\n", root)
	fmt.Println("  marker file:", state.ProjectMarkerFile(root))
	fmt.Println("  resume with: nimblegate resume --project")
	return 0
}
