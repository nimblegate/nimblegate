// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"

	"nimblegate/internal/paths"
	"nimblegate/internal/state"
)

// Resume implements `nimblegate resume`. Without a scope flag, it resumes
// the most-specific paused scope: project marker if present, else global
// pause. Errors if nothing is paused so the user notices invocations that
// would otherwise silently no-op.
func Resume(args []string) int {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	globalFlag := fs.Bool("global", false, "resume the global pause specifically")
	projectFlag := fs.Bool("project", false, "resume the current project's pause specifically")
	allFlag := fs.Bool("all", false, "resume both global and current-project pauses")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	store, err := state.NewStore()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
		return 2
	}

	// Resolve project root if we're inside one - needed by --project, --all,
	// and the default-pick branch. Failure isn't fatal; --global doesn't need it.
	var projectRoot string
	if cwd, err := os.Getwd(); err == nil {
		if r, err := paths.FindProjectRoot(cwd); err == nil {
			projectRoot = r
		}
	}

	// Read current state once so we can produce useful messages.
	st, _ := store.IsPaused(projectRoot)

	switch {
	case *allFlag:
		didSomething := false
		if st.GlobalPaused {
			if err := store.ResumeGlobal(); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
				return 2
			}
			fmt.Println("resumed global pause")
			didSomething = true
		}
		if st.ProjectPaused && projectRoot != "" {
			if err := store.ResumeProject(projectRoot); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
				return 2
			}
			fmt.Printf("resumed project pause: %s\n", projectRoot)
			didSomething = true
		}
		if !didSomething {
			fmt.Println("nimblegate resume: nothing was paused")
		}
		return 0

	case *globalFlag && *projectFlag:
		fmt.Fprintln(os.Stderr, "nimblegate resume: --global and --project are mutually exclusive (use --all to resume both)")
		return 2

	case *globalFlag:
		if !st.GlobalPaused {
			fmt.Println("nimblegate resume: global pause is not active")
			return 0
		}
		if err := store.ResumeGlobal(); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
			return 2
		}
		fmt.Println("resumed global pause")
		return 0

	case *projectFlag:
		if projectRoot == "" {
			fmt.Fprintln(os.Stderr, "nimblegate resume: --project requires being inside an onboarded project")
			return 2
		}
		if !st.ProjectPaused {
			fmt.Println("nimblegate resume: this project is not paused")
			return 0
		}
		if err := store.ResumeProject(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
			return 2
		}
		fmt.Printf("resumed project pause: %s\n", projectRoot)
		return 0
	}

	// Default: pick the most-specific paused scope automatically. Project
	// wins over global so a user who paused both can step through resumes
	// one at a time.
	switch {
	case st.ProjectPaused && projectRoot != "":
		if err := store.ResumeProject(projectRoot); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
			return 2
		}
		fmt.Printf("resumed project pause: %s\n", projectRoot)
		if st.GlobalPaused {
			fmt.Println("  (global pause still active, `nimblegate resume --global` to clear)")
		}
		return 0
	case st.GlobalPaused:
		if err := store.ResumeGlobal(); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate resume: %v\n", err)
			return 2
		}
		fmt.Println("resumed global pause")
		return 0
	default:
		fmt.Println("nimblegate resume: nothing is paused")
		return 0
	}
}
