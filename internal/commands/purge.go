// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/prompt"
	"nimblegate/internal/state"
	"nimblegate/internal/triggers/gitwrap"
)

// Purge implements `nimblegate purge` - full uninstall, reverse of setup.
//
// Removes (each conditional on being present):
//   - Binary shims at ~/.appframes/shims/
//   - PATH marker block in the shell rc file (added by setup)
//   - Shell snippet in the rc file (added by `shell install` / setup)
//   - ~/.appframes/ directory in full (unless --keep-config)
//
// Does NOT touch per-project state (.git/hooks/pre-commit, .appframes/
// directories inside projects). We don't track which projects are
// onboarded, so we can't safely walk them. The terminal output lists
// the manual cleanup commands at the end.
//
// Default confirmation prompt defaults to NO - purge is destructive, the
// user has to explicitly type y. --yes bypasses for non-interactive use.
//
// Flags:
//
//	--yes          answer yes to every prompt (non-interactive)
//	--dry-run      describe planned changes without making them
//	--keep-config  preserve ~/.appframes/ entirely (only remove shims + rc edits)
//	--shell=X      target shell rc file (bash or zsh)
func Purge(args []string) int {
	fs := flag.NewFlagSet("purge", flag.ContinueOnError)
	yesFlag := fs.Bool("yes", false, "answer yes to every prompt (non-interactive)")
	dryRunFlag := fs.Bool("dry-run", false, "describe planned changes without making them")
	keepConfigFlag := fs.Bool("keep-config", false, "preserve ~/.appframes/ entirely (only remove shims + rc edits)")
	shellFlag := fs.String("shell", detectShell(), "shell type (bash or zsh)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	st, err := detectSetupState(*shellFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate purge: detect state: %v\n", err)
		return 2
	}

	// Detect the install.sh-written PATH block independently (setup state
	// only knows about the setup PATH marker, not install.sh's).
	installPATHOnRc := rcHasMarker(st.rcPath, installPathMarkerBegin)

	printSetupState(os.Stdout, st)
	if installPATHOnRc {
		fmt.Printf("  install dir PATH:     ✓ (from install.sh, in %s)\n", st.rcPath)
	}
	fmt.Println()

	if !st.shimsInstalled && !st.pathOnRc && !st.snippetInstalled && !installPATHOnRc && !appframesHomeExists() {
		fmt.Println("✓ nimblegate is not installed on this machine. Nothing to purge.")
		return 0
	}

	fmt.Println("nimblegate purge will:")
	if st.shimsInstalled {
		fmt.Printf("  - remove binary shims from %s\n", st.shimsDir)
	}
	if st.pathOnRc {
		fmt.Printf("  - remove shim PATH marker block from %s\n", st.rcPath)
	}
	if installPATHOnRc {
		fmt.Printf("  - remove install-dir PATH marker block from %s\n", st.rcPath)
	}
	if st.snippetInstalled {
		fmt.Printf("  - remove shell snippet from %s\n", st.rcPath)
	}
	if !*keepConfigFlag && appframesHomeExists() {
		fmt.Println("  - remove ~/.appframes/ entirely (logs, history, state, binary; use --keep-config to preserve)")
	}
	fmt.Println()

	var p prompt.Prompter
	if *yesFlag {
		p = prompt.Always(true)
	} else {
		p = prompt.Stdio()
	}

	// Default NO on the main confirmation - purge is destructive.
	if !p.YesNo("Proceed with purge?", false) {
		fmt.Println("aborted: nothing was changed")
		return 0
	}

	// Step 1: shims
	if st.shimsInstalled {
		if *dryRunFlag {
			fmt.Printf("  [dry-run] would remove shims from %s\n", st.shimsDir)
		} else {
			dir, err := gitwrap.UninstallShims()
			if err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate purge: uninstall shims: %v\n", err)
				return 1
			}
			fmt.Printf("  ✓ removed shims from %s\n", dir)
		}
	}

	// Step 2a: shim-PATH marker block in rc (added by `nimblegate setup`)
	if st.pathOnRc {
		if *dryRunFlag {
			fmt.Printf("  [dry-run] would remove shim PATH marker block from %s\n", st.rcPath)
		} else {
			if err := removePATHFromRC(st.rcPath); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate purge: remove shim PATH marker: %v\n", err)
				return 1
			}
			fmt.Printf("  ✓ removed shim PATH marker block from %s\n", st.rcPath)
		}
	}

	// Step 2b: install-dir PATH marker block in rc (added by install.sh)
	if installPATHOnRc {
		if *dryRunFlag {
			fmt.Printf("  [dry-run] would remove install-dir PATH marker block from %s\n", st.rcPath)
		} else {
			if err := removeInstallPATHFromRC(st.rcPath); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate purge: remove install PATH marker: %v\n", err)
				return 1
			}
			fmt.Printf("  ✓ removed install-dir PATH marker block from %s\n", st.rcPath)
		}
	}

	// Step 3: shell snippet in rc
	if st.snippetInstalled {
		if *dryRunFlag {
			fmt.Printf("  [dry-run] would remove shell snippet from %s\n", st.rcPath)
		} else {
			if err := gitwrap.Uninstall(*shellFlag); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate purge: uninstall snippet: %v\n", err)
				return 1
			}
			fmt.Printf("  ✓ removed shell snippet from %s\n", st.rcPath)
		}
	}

	// Step 4: ~/.appframes/ home dir (unless --keep-config)
	if !*keepConfigFlag {
		home, err := appframesHomeDir()
		if err == nil && dirExists(home) {
			if *dryRunFlag {
				fmt.Printf("  [dry-run] would remove %s\n", home)
			} else {
				if err := os.RemoveAll(home); err != nil {
					fmt.Fprintf(os.Stderr, "nimblegate purge: remove %s: %v\n", home, err)
					return 1
				}
				fmt.Printf("  ✓ removed %s\n", home)
			}
		}
	}

	fmt.Println()
	if *dryRunFlag {
		fmt.Println("Dry run complete. Re-run without --dry-run to apply.")
		return 0
	}
	fmt.Println("nimblegate purge complete.")
	fmt.Println()
	fmt.Println("Not touched (cleanup manually if you want):")
	fmt.Println("  - Per-project .git/hooks/pre-commit in onboarded projects")
	fmt.Println("    remove with: cd <project>; rm .git/hooks/pre-commit")
	fmt.Println("  - Per-project .appframes/ + appframes.toml in onboarded projects")
	fmt.Println("    remove with: cd <project>; rm -rf .appframes/ appframes.toml")
	fmt.Println("  - The nimblegate binary itself (whatever installed it owns the cleanup)")
	fmt.Println()
	fmt.Printf("Reload your shell to clear the PATH:  source %s\n", st.rcPath)
	return 0
}

// appframesHomeDir returns ~/.nimblegate (the directory, not the shims
// subdir). Uses state.NewStore() as the canonical home resolver so this
// stays in sync with where state.json lives.
func appframesHomeDir() (string, error) {
	store, err := state.NewStore()
	if err != nil {
		return "", err
	}
	// GlobalStateFile is <home>/.appframes/state.json; parent is the home dir.
	return filepath.Dir(store.GlobalStateFile()), nil
}

func appframesHomeExists() bool {
	dir, err := appframesHomeDir()
	if err != nil {
		return false
	}
	return dirExists(dir)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// rcHasMarker returns true if the rc file contains the given begin-marker
// substring. Used to detect install.sh's PATH block independently of
// setup state (setupState only tracks setup's own marker).
func rcHasMarker(rcPath, beginMarker string) bool {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), beginMarker)
}

// Marker constants for the binary-PATH block written by cmd/installer/install.sh.
// MUST stay in sync with PATH_MARKER_BEGIN / PATH_MARKER_END in install.sh.
const (
	installPathMarkerBegin = "# >>> nimblegate install PATH"
	installPathMarkerEnd   = "# <<< nimblegate install PATH"
)

// removeMarkerBlockFromRC strips a fenced marker block from rcPath, given
// the begin/end marker strings (matched as substrings on their own lines).
// A preceding blank line is also removed (paired with the leading blank
// our writers add as a separator).
//
// Idempotent: missing marker is a no-op (returns nil).
// Half-block (begin present but end absent) returns an error rather than
// silently removing - that situation indicates manual editing the user
// should see.
func removeMarkerBlockFromRC(rcPath, begin, end string) error {
	data, err := os.ReadFile(rcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", rcPath, err)
	}

	lines := strings.Split(string(data), "\n")
	beginLine := -1
	endLine := -1
	for i, line := range lines {
		if beginLine == -1 && strings.Contains(line, begin) {
			beginLine = i
			continue
		}
		if beginLine != -1 && strings.Contains(line, end) {
			endLine = i
			break
		}
	}
	if beginLine == -1 {
		return nil
	}
	if endLine == -1 {
		return fmt.Errorf("found %q but not %q in %s: refusing to remove half a block (manual edit suspected)",
			begin, end, rcPath)
	}

	removeFrom := beginLine
	if removeFrom > 0 && lines[removeFrom-1] == "" {
		removeFrom--
	}

	newLines := make([]string, 0, len(lines)-(endLine-removeFrom+1))
	newLines = append(newLines, lines[:removeFrom]...)
	newLines = append(newLines, lines[endLine+1:]...)
	newBody := strings.Join(newLines, "\n")

	dir := filepath.Dir(rcPath)
	tmp, err := os.CreateTemp(dir, ".nimblegate-rc.*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(newBody); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod: %w", err)
	}
	if err := os.Rename(tmpName, rcPath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// removePATHFromRC strips the nimblegate-setup PATH marker block (shim path)
// from rcPath. Thin wrapper over removeMarkerBlockFromRC kept for the
// existing test surface; setup's pathMarkerBegin / pathMarkerEnd are
// defined in setup.go.
func removePATHFromRC(rcPath string) error {
	return removeMarkerBlockFromRC(rcPath, pathMarkerBegin, pathMarkerEnd)
}

// removeInstallPATHFromRC strips the install.sh-written binary-PATH marker
// block (the one that puts ~/.appframes/bin on PATH).
func removeInstallPATHFromRC(rcPath string) error {
	return removeMarkerBlockFromRC(rcPath, installPathMarkerBegin, installPathMarkerEnd)
}
