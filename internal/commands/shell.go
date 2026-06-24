// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"nimblegate/internal/triggers/gitwrap"
)

// Shell installs / uninstalls / prints the shell git-wrap surfaces.
//
//	nimblegate shell install   [--shell=bash|zsh] [--strict]
//	nimblegate shell uninstall [--shell=bash|zsh] [--strict]
//	nimblegate shell print     [--shell=bash|zsh] [--strict]
//
// The `--strict` flag installs binary-shim wrappers under
// ~/.appframes/shims/ that work in EVERY shell context - interactive,
// non-interactive (Claude Code's Bash tool, IDE shells, CI), even sh
// invocations. This is the recommended setup when nimblegate is supposed
// to be load-bearing: shell functions (without --strict) only fire in
// interactive shells, so agents and non-interactive subprocesses can
// silently bypass them.
//
// Without --strict, behaves as it always did: writes shell functions to
// ~/.bashrc / ~/.zshrc. Kept for backward compatibility + interactive-
// shell-only use.
func Shell(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate shell: subcommand required (install|uninstall|print; --help for usage)")
		return 2
	}
	sub := args[0]
	if sub == "--help" || sub == "-h" || sub == "help" {
		fmt.Println("nimblegate shell: install / uninstall / print the command-wrap shell integration")
		fmt.Println()
		fmt.Println("Usage: nimblegate shell <subcommand> [--shell=bash|zsh] [--strict]")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  install [--shell=...] [--strict]   Install shell integration (or binary shims with --strict)")
		fmt.Println("  uninstall [--shell=...] [--strict] Remove shell integration / shims")
		fmt.Println("  print [--shell=...] [--strict]     Print the shell snippet / shim contents (no install)")
		fmt.Println()
		fmt.Println("Recommended for AI-agent use:  nimblegate shell install --strict")
		fmt.Println("--strict installs binary shims at ~/.appframes/shims/ that gate EVERY shell context")
		fmt.Println("(interactive + non-interactive + CI). Plain install only fires in interactive shells.")
		return 0
	}
	fs := flag.NewFlagSet("shell "+sub, flag.ExitOnError)
	shellFlag := fs.String("shell", detectShell(), "shell type: bash or zsh")
	strictFlag := fs.Bool("strict", false, "install binary shims at ~/.appframes/shims/ that gate every shell context (recommended for AI-agent use)")
	_ = fs.Parse(args[1:])

	switch sub {
	case "print":
		if *strictFlag {
			return shellPrintStrict()
		}
		fmt.Print(gitwrap.ShellSnippet(*shellFlag))
		return 0
	case "install":
		if *strictFlag {
			return shellInstallStrict(*shellFlag)
		}
		if err := gitwrap.Install(*shellFlag); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate shell install: %v\n", err)
			return 1
		}
		rc, _ := gitwrap.RCFile(*shellFlag)
		fmt.Printf("✓ Installed git-wrap into %s\n", rc)
		fmt.Printf("  Reload your shell or run: source %s\n", rc)
		fmt.Println()
		fmt.Println("Note: this installs shell FUNCTIONS that only fire in INTERACTIVE shells.")
		fmt.Println("Non-interactive shells (Claude Code's Bash tool, IDE shells, CI) will")
		fmt.Println("bypass them. For agent-proof gating, re-run with --strict:")
		fmt.Println("  nimblegate shell install --strict")
		return 0
	case "uninstall":
		if *strictFlag {
			return shellUninstallStrict()
		}
		if err := gitwrap.Uninstall(*shellFlag); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate shell uninstall: %v\n", err)
			return 1
		}
		rc, _ := gitwrap.RCFile(*shellFlag)
		fmt.Printf("✓ Removed git-wrap from %s\n", rc)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate shell: unknown subcommand %q\n", sub)
		return 2
	}
}

func shellInstallStrict(shell string) int {
	dir, err := gitwrap.InstallShims()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate shell install --strict: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Installed %d shim(s) at %s\n", len(gitwrap.ShimNames()), dir)
	for _, name := range gitwrap.ShimNames() {
		fmt.Printf("    - %s\n", name)
	}
	fmt.Println()
	fmt.Println("Next: add this directory to the FRONT of your PATH so the shims")
	fmt.Println("are found before /usr/bin. Add this line to your shell rc:")
	fmt.Println()
	rc := "~/.bashrc"
	if shell == "zsh" {
		rc = "~/.zshrc"
	}
	fmt.Printf("  %s:\n    export PATH=\"%s:$PATH\"\n", rc, dir)
	fmt.Println()
	fmt.Println("Then reload your shell. Verify with:")
	fmt.Println("  which git    # should resolve to the shim path above")
	fmt.Println()
	fmt.Println("With shims on PATH, EVERY shell context, including non-interactive")
	fmt.Println("ones used by AI agents and tools, routes destructive commands")
	fmt.Println("through nimblegate. Silent bypass is no longer possible without")
	fmt.Println("explicit operator action (removing the shims or editing PATH).")
	return 0
}

func shellUninstallStrict() int {
	dir, err := gitwrap.UninstallShims()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate shell uninstall --strict: %v\n", err)
		return 1
	}
	fmt.Printf("✓ Removed nimblegate shims from %s\n", dir)
	fmt.Println("  (you may also want to remove the PATH=... line from your shell rc)")
	return 0
}

func shellPrintStrict() int {
	dir, err := gitwrap.ShimsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate shell print --strict: %v\n", err)
		return 1
	}
	fmt.Printf("# `nimblegate shell install --strict` would write these shims to %s:\n\n", dir)
	for _, name := range gitwrap.ShimNames() {
		fmt.Printf("# === %s ===\n", name)
		fmt.Println(gitwrap.RenderShimForName(name))
	}
	return 0
}

func detectShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		if strings.HasSuffix(s, "/zsh") {
			return "zsh"
		}
	}
	return "bash"
}
