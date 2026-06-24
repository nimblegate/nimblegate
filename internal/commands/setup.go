// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/prompt"
	"nimblegate/internal/triggers/gitwrap"
)

// Setup implements `nimblegate setup` - an interactive install flow that
// composes the existing install primitives (shim install, shell snippet
// install) and adds the missing piece: auto-editing the shell rc file to
// put the shim directory on PATH.
//
// Existing `nimblegate shell install --strict` ships the binary shims but
// only PRINTS the PATH export line; the user has to copy-paste it. setup
// closes that loop by writing the line itself (with a fenced marker block
// so a future purge can find and remove it cleanly).
//
// Flags:
//
//	--yes       answer Y to every prompt (non-interactive / CI)
//	--dry-run   describe what would happen, change nothing
//	--check     report current install state and exit (read-only)
//	--shell=X   target shell (bash or zsh); default: detect from $SHELL
//
// Idempotent: re-running setup detects what's already done and only
// prompts for the missing pieces.
func Setup(args []string) int {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	yesFlag := fs.Bool("yes", false, "answer yes to every prompt (non-interactive)")
	dryRunFlag := fs.Bool("dry-run", false, "describe planned changes without making them")
	checkFlag := fs.Bool("check", false, "report current install state and exit (read-only)")
	shellFlag := fs.String("shell", detectShell(), "shell type (bash or zsh)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	st, err := detectSetupState(*shellFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate setup: detect state: %v\n", err)
		return 2
	}

	// --check is pure reporting. Exits 0 if everything looks installed; 1
	// if some step is missing (so it's useful as a CI gate).
	if *checkFlag {
		printSetupState(os.Stdout, st)
		if st.complete() {
			fmt.Println("\n✓ nimblegate setup looks complete on this machine.")
			return 0
		}
		fmt.Println("\n⚠ Some setup steps are missing. Run `nimblegate setup` (no flags) to install.")
		return 1
	}

	printSetupState(os.Stdout, st)
	fmt.Println()

	if st.complete() && !*dryRunFlag {
		fmt.Println("✓ nimblegate setup is already complete on this machine. Nothing to do.")
		fmt.Println("  Re-run with --check to verify, or `nimblegate status` to see runtime state.")
		return 0
	}

	// Choose a prompter. --yes + non-TTY both produce a non-interactive
	// flow; --dry-run also acts non-interactively (we ask but treat answers
	// as no-side-effect since we're not going to write anyway).
	var p prompt.Prompter
	switch {
	case *yesFlag:
		p = prompt.Always(true)
	default:
		p = prompt.Stdio()
	}

	rcPath, _ := gitwrap.RCFile(*shellFlag)
	shimsDir, _ := gitwrap.ShimsDir()

	// Step 1: install binary shims at ~/.appframes/shims/.
	if !st.shimsInstalled {
		if p.YesNo(fmt.Sprintf("Install binary shims to %s?", shimsDir), true) {
			if *dryRunFlag {
				fmt.Printf("  [dry-run] would write %d shim(s) to %s\n", len(gitwrap.ShimNames()), shimsDir)
			} else {
				if _, err := gitwrap.InstallShims(); err != nil {
					fmt.Fprintf(os.Stderr, "nimblegate setup: install shims: %v\n", err)
					return 1
				}
				fmt.Printf("  ✓ installed %d shim(s)\n", len(gitwrap.ShimNames()))
			}
		} else {
			fmt.Println("  skipped: shims are required for agent-proof gating")
		}
	}

	// Step 2: add PATH export line to rc file. This is the genuinely new
	// piece - existing `shell install --strict` only prints the instructions.
	if !st.pathOnRc {
		if p.YesNo(fmt.Sprintf("Add shim directory to PATH in %s?", rcPath), true) {
			if *dryRunFlag {
				fmt.Printf("  [dry-run] would append a marker block to %s with:\n", rcPath)
				fmt.Printf("    export PATH=\"%s:$PATH\"\n", shimsDir)
			} else {
				if err := addPATHToRC(rcPath, shimsDir, time.Now()); err != nil {
					fmt.Fprintf(os.Stderr, "nimblegate setup: edit %s: %v\n", rcPath, err)
					return 1
				}
				fmt.Printf("  ✓ added PATH export to %s\n", rcPath)
			}
		} else {
			fmt.Printf("  skipped: without the PATH edit, shims won't be active. Add manually:\n")
			fmt.Printf("    export PATH=\"%s:$PATH\"\n", shimsDir)
		}
	}

	// Step 3: install shell-function wrapper (interactive shells only).
	// This is a separate layer from the strict shim - provides a fallback
	// when the user runs the shim directly without --strict. Optional.
	if !st.snippetInstalled {
		if p.YesNo(fmt.Sprintf("Also install shell-function fallback in %s? (interactive shells only)", rcPath), false) {
			if *dryRunFlag {
				fmt.Printf("  [dry-run] would append the gitwrap shell snippet to %s\n", rcPath)
			} else {
				if err := gitwrap.Install(*shellFlag); err != nil {
					fmt.Fprintf(os.Stderr, "nimblegate setup: install snippet: %v\n", err)
					return 1
				}
				fmt.Printf("  ✓ installed shell snippet in %s\n", rcPath)
			}
		}
	}

	// Verification + next steps.
	fmt.Println()
	if *dryRunFlag {
		fmt.Println("Dry run complete. Re-run without --dry-run to apply.")
		return 0
	}
	fmt.Println("Next:")
	fmt.Printf("  source %s          # reload your shell so PATH takes effect\n", rcPath)
	fmt.Println("  which git                  # should resolve to the shim path")
	fmt.Println("  nimblegate status           # confirm baseline")
	fmt.Println()
	fmt.Println("Then onboard a project with:  nimblegate init")
	return 0
}

// setupState captures the on-disk install state at one moment. Used to
// drive idempotent prompting and the --check report.
type setupState struct {
	shellName        string
	shimsDir         string
	rcPath           string
	shimsInstalled   bool   // any files under ~/.appframes/shims/?
	pathOnRc         bool   // rc file contains the nimblegate PATH marker block
	snippetInstalled bool   // rc file contains the gitwrap shell-snippet marker
	whichGit         string // result of exec.LookPath("git"), if any
	pathPointsToShim bool   // does whichGit resolve to a path under shimsDir?
}

func (s setupState) complete() bool {
	return s.shimsInstalled && s.pathOnRc
}

func detectSetupState(shell string) (setupState, error) {
	st := setupState{shellName: shell}
	dir, err := gitwrap.ShimsDir()
	if err != nil {
		return st, fmt.Errorf("resolve shims dir: %w", err)
	}
	st.shimsDir = dir
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		st.shimsInstalled = true
	}
	rc, err := gitwrap.RCFile(shell)
	if err != nil {
		return st, fmt.Errorf("resolve rc file: %w", err)
	}
	st.rcPath = rc
	if data, err := os.ReadFile(rc); err == nil {
		body := string(data)
		st.pathOnRc = strings.Contains(body, pathMarkerBegin)
		// gitwrap.Install writes a block fenced with "nimblegate git-wrap"
		// markers (see internal/triggers/gitwrap/installer.go). Match on
		// that literal so detection survives even if the user has other
		// references to nimblegate elsewhere in their rc file.
		st.snippetInstalled = strings.Contains(body, "nimblegate git-wrap")
	}
	if path, err := exec.LookPath("git"); err == nil {
		st.whichGit = path
		st.pathPointsToShim = strings.HasPrefix(path, dir+string(filepath.Separator))
	}
	return st, nil
}

func printSetupState(w io.Writer, st setupState) {
	fmt.Fprintln(w, "nimblegate setup state:")
	fmt.Fprintf(w, "  shell:                %s\n", st.shellName)
	fmt.Fprintf(w, "  shims directory:      %s  %s\n", st.shimsDir, checkmark(st.shimsInstalled))
	fmt.Fprintf(w, "  rc file:              %s\n", st.rcPath)
	fmt.Fprintf(w, "    PATH export:        %s\n", checkmark(st.pathOnRc))
	fmt.Fprintf(w, "    shell snippet:      %s\n", checkmark(st.snippetInstalled))
	if st.whichGit != "" {
		marker := "(system)"
		if st.pathPointsToShim {
			marker = "(✓ shim active)"
		}
		fmt.Fprintf(w, "  which git:            %s  %s\n", st.whichGit, marker)
	} else {
		fmt.Fprintln(w, "  which git:            (not found on PATH)")
	}
}

func checkmark(installed bool) string {
	if installed {
		return "✓"
	}
	return "(not present)"
}

// Marker block for the PATH export. Surrounded by recognizable delimiters
// so `nimblegate purge` (slice 3) can find and remove the block without
// needing to know the exact text. The date in the begin line is
// informational; purge matches on the literal pathMarkerBegin substring.
const (
	pathMarkerBegin = "# >>> nimblegate setup PATH"
	pathMarkerEnd   = "# <<< nimblegate setup PATH"
)

// addPATHToRC appends a fenced marker block to rcPath that exports the
// shim directory at the front of PATH. Creates the rc file if it doesn't
// exist. Idempotent: if the marker block is already present, returns
// without modifying.
func addPATHToRC(rcPath, shimsDir string, now time.Time) error {
	// Read existing content; missing file = empty start.
	existing, err := os.ReadFile(rcPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", rcPath, err)
	}
	if strings.Contains(string(existing), pathMarkerBegin) {
		return nil // already installed
	}
	// Build the block. Trailing newline keeps neighbors clean.
	var sb strings.Builder
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("\n")
	sb.WriteString(pathMarkerBegin)
	sb.WriteString(" (added ")
	sb.WriteString(now.Format("2006-01-02"))
	sb.WriteString(") >>>\n")
	sb.WriteString(fmt.Sprintf("export PATH=\"%s:$PATH\"\n", shimsDir))
	sb.WriteString(pathMarkerEnd)
	sb.WriteString(" <<<\n")
	// Append atomically: write to a temp sibling, then rename. This keeps
	// the rc file from getting truncated mid-write if something interrupts.
	dir := filepath.Dir(rcPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".nimblegate-rc.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(existing); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write existing: %w", err)
	}
	if _, err := tmp.WriteString(sb.String()); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write block: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, rcPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
