// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/kits"
	"nimblegate/internal/triggers/precommit"
)

// isTTY reports whether f looks like an interactive terminal. Used by init
// to decide between interactive ambiguity prompt and non-interactive error.
// Stubbable for tests via Init's stdin override (initAtWith).
func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

// Init scaffolds appframes.toml + .appframes/ in the current directory.
// Refuses to overwrite an existing appframes.toml.
func Init(args []string) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: %v\n", err)
		return 1
	}
	return initAt(root, args)
}

func initAt(root string, args []string) int {
	return initAtWith(root, args, os.Stdin, os.Stdout, os.Stderr, isTTY(os.Stdin))
}

// initAtWith is the testable entry point - stdin/stdout/stderr + tty flag
// injected so tests can simulate interactive prompts + non-TTY error paths
// without touching real file descriptors.
func initAtWith(root string, args []string, stdin io.Reader, stdout, stderr io.Writer, tty bool) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(stderr)
	kitName := flags.String("kit", "core", "Starter kit to apply (v1 mode: core/web-app/cf-pages-project/cf-workers-project/security-strict/none)")
	useV2 := flags.Bool("v2", false, "Write a v2 schema config (axis-based: framework/platform/domains) instead of v1 kit-based")
	flagFw := flags.String("framework", "", "v2 only: framework axis pick (svelte/astro/go/html); skips detection prompt")
	flagPf := flags.String("platform", "", "v2 only: platform axis pick (cloudflare/static-host); skips detection prompt")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	if *useV2 {
		return initAtV2(root, *flagFw, *flagPf, stdin, stdout, stderr, tty)
	}

	cfgPath := filepath.Join(root, "appframes.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: %s already exists; refusing to overwrite\n", cfgPath)
		return 1
	} else if !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "nimblegate init: stat: %v\n", err)
		return 2
	}

	dir := filepath.Join(root, ".appframes")
	if err := os.MkdirAll(filepath.Join(dir, "_canonical"), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: mkdir: %v\n", err)
		return 2
	}

	ks, err := kits.LoadStdlib()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: %v\n", err)
		return 1
	}

	var enabled []string
	var applied []string
	if *kitName != "none" {
		k, ok := ks.Get(*kitName)
		if !ok {
			fmt.Fprintf(os.Stderr, "nimblegate init: unknown kit %q. Available: %s\n",
				*kitName, strings.Join(ks.Names(), ", "))
			return 1
		}
		enabled = append(enabled, k.Frames...)
		applied = []string{*kitName}
	}

	var b strings.Builder
	b.WriteString("[frames]\nenabled = [\n")
	for _, id := range enabled {
		fmt.Fprintf(&b, "    %q,\n", id)
	}
	b.WriteString("]\n\n[ui]\napplied_kits = [")
	for i, a := range applied {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", a)
	}
	b.WriteString("]\n")

	out := b.String()
	if len(enabled) == 0 {
		out = strings.Replace(out, "enabled = [\n]\n", "enabled = []\n", 1)
	}

	if err := os.WriteFile(cfgPath, []byte(out), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: write config: %v\n", err)
		return 2
	}

	gitignorePath := filepath.Join(dir, ".gitignore")
	gitignoreContent := "# Local-only run-state, do not commit\naudit.log\naudit.log.*\naudit.parts/\n_canonical/findings-ledger.json\n_canonical/whitelist-stats.json\n_canonical/slice-state.json\n_canonical/slice-history.json\n"
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate init: write .gitignore: %v\n", err)
		return 2
	}

	fmt.Printf("nimblegate init: applied kit %q (%d frames). Dashboard: nimblegate gateway start\n",
		*kitName, len(enabled))

	// If this is a git repo, install the pre-commit hook.
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if err := precommit.Install(root); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate init: pre-commit hook install: %v\n", err)
			fmt.Fprintln(os.Stderr, "(non-fatal; other parts of init succeeded)")
		}
	}

	return 0
}

// initAtV2 is the v2 schema path for `nimblegate init --v2`. Detects axes
// from the filesystem, resolves ambiguity via flags or interactive prompt
// (TTY) / clear error (non-TTY), and writes a v2-shaped appframes.toml.
//
// Per spec §6.4: explicit --framework / --platform flags override
// detection silently. Per §6.3: conflicts halt with a numbered choice.
func initAtV2(root, flagFw, flagPf string, stdin io.Reader, stdout, stderr io.Writer, tty bool) int {
	cfgPath := filepath.Join(root, "appframes.toml")
	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Fprintf(stderr, "nimblegate init: %s already exists; refusing to overwrite\n", cfgPath)
		return 1
	} else if !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "nimblegate init: stat: %v\n", err)
		return 2
	}

	signals := detectSignals(root)
	detected := DetectAxes(signals)
	decision := ResolveAxes(detected, flagFw, flagPf)

	if decision.PromptFramework || decision.PromptPlatform {
		if !tty {
			fmt.Fprintln(stderr, NonInteractiveAmbiguityError(decision, detected).Error())
			return 1
		}
		if decision.PromptFramework {
			pick, err := PromptAxis(stdin, stdout, "framework", detected.CandidatesByAxis["framework"])
			if err != nil {
				fmt.Fprintf(stderr, "nimblegate init: %v\n", err)
				return 1
			}
			decision.Framework = pick
		}
		if decision.PromptPlatform {
			pick, err := PromptAxis(stdin, stdout, "platform", detected.CandidatesByAxis["platform"])
			if err != nil {
				fmt.Fprintf(stderr, "nimblegate init: %v\n", err)
				return 1
			}
			decision.Platform = pick
		}
	}

	dir := filepath.Join(root, ".appframes")
	if err := os.MkdirAll(filepath.Join(dir, "_canonical"), 0o755); err != nil {
		fmt.Fprintf(stderr, "nimblegate init: mkdir: %v\n", err)
		return 2
	}

	cfg := &v2.Config{
		Core: v2.CoreSel{Enabled: true},
	}
	cfg.Appframes.Schema.Version = 2
	if decision.Framework != "" {
		cfg.Framework.Selected = decision.Framework
	}
	if decision.Platform != "" {
		cfg.Platform.Selected = decision.Platform
	}

	rendered, err := renderKitsV2TOML(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "nimblegate init: render config: %v\n", err)
		return 2
	}
	if err := os.WriteFile(cfgPath, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(stderr, "nimblegate init: write config: %v\n", err)
		return 2
	}

	gitignorePath := filepath.Join(dir, ".gitignore")
	gitignoreContent := "# Local-only run-state, do not commit\naudit.log\naudit.log.*\naudit.parts/\n_canonical/findings-ledger.json\n_canonical/whitelist-stats.json\n_canonical/slice-state.json\n_canonical/slice-history.json\n"
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0o644); err != nil {
		fmt.Fprintf(stderr, "nimblegate init: write .gitignore: %v\n", err)
		return 2
	}

	fmt.Fprintf(stdout, "nimblegate init: v2 config written. Framework: %s, Platform: %s. Apply a starter kit with `nimblegate kits apply <kit>` or hand-pick domains via [domains].selected in appframes.toml.\n",
		fallback(decision.Framework, "(none)"), fallback(decision.Platform, "(none)"))

	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		if err := precommit.Install(root); err != nil {
			fmt.Fprintf(stderr, "nimblegate init: pre-commit hook install: %v\n", err)
			fmt.Fprintln(stderr, "(non-fatal; other parts of init succeeded)")
		}
	}

	return 0
}

func fallback(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}
