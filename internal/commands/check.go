// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/linters"
	"nimblegate/internal/paths"
	"nimblegate/internal/state"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/tasks"
	"nimblegate/internal/whitelist"
)

func Check(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	triggerFlag := fs.String("trigger", "cli", "trigger surface to evaluate (cli, pre-commit, git-wrap, server)")
	includeUnstaged := fs.Bool("include-unstaged", false, "for --trigger=pre-commit: include working-tree changes (not just staged) so you can preview what the hook would say before `git add`")
	noLinters := fs.Bool("no-linters", false, "skip language-native linters ([linters] in appframes.toml) for a faster frames-only run")
	_ = fs.Parse(args)

	trigger := engine.Trigger(*triggerFlag)
	switch trigger {
	case engine.TriggerCLI, engine.TriggerPreCommit, engine.TriggerGitWrap, engine.TriggerServer:
	default:
		fmt.Fprintf(os.Stderr, "nimblegate check: unknown trigger %q\n", trigger)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate check: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate check: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	// Pause fast-path: when paused (globally or per-project), exit 0
	// silently so the pre-commit hook passes through. Pause windows are
	// intentionally absent from the audit log - see internal/state docs.
	if store, err := state.NewStore(); err == nil {
		if st, err := store.IsPaused(root); err == nil && st.AnyPaused() {
			return 0
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate check: warning: pause state unreadable: %v\n", err)
		}
	}

	stdlibFrames, err := stdlib.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate check: load stdlib: %v\n", err)
		return 2
	}
	projectFrames, loadErrs := frames.LoadFromDir(paths.AppframesDir(root))

	// Phase 1 Slice 4: filter by lifecycle. Only frames whose lifecycle
	// is active/candidate (or empty, defaulting to active for pre-Phase-1
	// frames) participate in gating. proposed/deprecated/archived frames
	// stay loaded for queries (history/list/info) but never fire.
	gatedStdlib, skippedStdlib := filterGated(stdlibFrames)
	gatedProject, skippedProject := filterGated(projectFrames)
	totalSkipped := len(skippedStdlib) + len(skippedProject)

	e, err := engine.New(engine.Options{
		ProjectRoot:   root,
		StdlibFrames:  gatedStdlib,
		ProjectFrames: gatedProject,
		CheckFuncs:    BuiltinCheckFuncs(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate check: engine init: %v\n", err)
		return 2
	}
	defer e.Close()

	ctx := engine.CheckContext{
		Trigger:       trigger,
		ProjectRoot:   root,
		WorkingDir:    cwd,
		CurrentBranch: currentGitBranch(root),
		ExcludedDirs:  e.ExcludedDirs(),
		IgnorePath:    e.IgnorePathFunc(),
	}
	if trigger == engine.TriggerPreCommit {
		ctx.ChangedFiles = preCommitChangedFiles(root, *includeUnstaged)
	}

	results := engine.Run(e.Registry, ctx)

	// Language-native linters (go vet, …) run after native frames and feed
	// the same results slice, so audit + whitelist + format treat their
	// findings identically. They're off unless [linters] enables them;
	// --no-linters forces a frames-only run regardless of config.
	if !*noLinters {
		lintResults, _ := linters.RunEnabled(e.ProjectConfig.Linters, root, e.ExcludedDirs())
		results = append(results, lintResults...)
	}

	// Audit log records RAW results before any suppression - the trail
	// must capture what frames found, not just what the user saw.
	for _, r := range results {
		_ = e.Audit.Write(ctx, r)
	}

	// Load the project whitelist (V0.5 spec §6). Missing file is fine;
	// any load failure is fatal - the whole point of fail-closed is that
	// a broken whitelist never silently grants exemptions.
	wl, err := whitelist.LoadFromProject(root, knownIDsWithLinters(e), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate check: %v\n", err)
		return 2
	}

	// Suppression pass. Whitelisted hits get filtered out and each one
	// is also recorded in the audit log so the bypass is never silent.
	filtered, suppressed := engine.ApplyWhitelist(results, wl, root)
	for _, s := range suppressed {
		_ = e.Audit.WriteSuppression(ctx, s)
	}

	// Phase 1 Slice 9: persist whitelist hygiene counters across runs.
	// Best-effort - a failure to write stats must not break the check
	// pipeline. Stats are advisory; the suppression itself works without them.
	if wl != nil {
		if stats, err := whitelist.LoadStats(root); err == nil {
			stats.Merge(wl, time.Now().UTC())
			_ = stats.Save(root)
		}
	}

	engine.FormatLoadWarnings(os.Stdout, loadErrs)
	exit := engine.FormatResults(os.Stdout, filtered)

	// Phase 1 Slice 4: surface lifecycle-filtered frames as a one-line
	// notice. The contract: non-gated frames are loaded (so history /
	// list / info can still see them) but never fire here. Visibility
	// without alarm.
	if totalSkipped > 0 {
		fmt.Printf("\nNote: %d frame(s) not gated (lifecycle ∈ proposed/deprecated/archived).\n", totalSkipped)
		fmt.Printf("      See `nimblegate history list` and `nimblegate history check` for details.\n")
	}

	// Track stage: fold this run's findings into the persistent task-list so
	// the list shrinks as findings get fixed. Only on the CLI trigger - it's
	// a full-tree run; the scoped pre-commit must NOT resolve out-of-scope
	// findings. Best-effort: a ledger failure never breaks the gate.
	if trigger == engine.TriggerCLI {
		if ledger, lerr := tasks.Load(root); lerr == nil {
			next, resolved := tasks.Reconcile(ledger, tasks.KeysFromResults(filtered, root), time.Now().UTC())
			_ = next.Save(root)
			if open := len(next.OpenTasks()); open > 0 || len(resolved) > 0 {
				fmt.Printf("\nTasks: %d open", open)
				if len(resolved) > 0 {
					fmt.Printf(" (%d resolved since last run)", len(resolved))
				}
				fmt.Printf(". See `nimblegate tasks`\n")
			}
		}
	}

	// Broken frames don't fail the gate by themselves - they're already
	// surfaced via the banner. Keep exit driven by BLOCK/ERROR results.
	return exit
}

// knownIDsWithLinters is the frame-ID set the whitelist loader validates
// against: the frame registry's IDs plus the enabled linters' synthetic IDs
// (e.g. app-correctness/shellcheck). Every path that loads the whitelist must
// use this - the loader fails closed on unknown IDs, so without the linter IDs
// a whitelist entry suppressing a linter finding breaks the load.
func knownIDsWithLinters(e *engine.Engine) map[string]bool {
	known := e.KnownFrameIDs()
	if known == nil {
		known = map[string]bool{}
	}
	for _, id := range linters.EnabledIDs(e.ProjectConfig.Linters) {
		known[id] = true
	}
	return known
}

// allKnownIDsWithLinters is the broader-scope known-ID set used by the
// gateway whitelist validator: every loaded stdlib + project frame ID
// (regardless of whether the project has the frame currently enabled),
// plus the enabled linters' synthetic IDs.
//
// Why broader than knownIDsWithLinters: the gateway whitelist describes
// suppressions for ANY frame that might fire. A frame can become enabled
// later (kit apply, manual toggle) and the whitelist entry should already
// be in place. Validating against the post-enablement registry rejects
// otherwise-valid whitelist entries with a confusing "unknown frame ID"
// error when the truth is simply "not currently enabled".
func allKnownIDsWithLinters(stdlib, project []frames.Frame, e *engine.Engine) map[string]bool {
	known := map[string]bool{}
	for _, f := range stdlib {
		known[f.ID()] = true
	}
	for _, f := range project {
		known[f.ID()] = true
	}
	if e != nil {
		for _, id := range linters.EnabledIDs(e.ProjectConfig.Linters) {
			known[id] = true
		}
	}
	return known
}

// filterGated splits a frame slice into (gated, non-gated). Gated frames
// fire at gates; non-gated stay loaded for queries. Used by `nimblegate
// check` and the pre-commit trigger codepath. Added 2026-05-20 with
// Phase 1 Slice 4.
func filterGated(in []frames.Frame) (gated, skipped []frames.Frame) {
	for _, f := range in {
		if frames.IsGated(f.Frontmatter.EffectiveLifecycle()) {
			gated = append(gated, f)
		} else {
			skipped = append(skipped, f)
		}
	}
	return gated, skipped
}

// currentGitBranch returns the current branch name, or "" if not a git repo.
func currentGitBranch(root string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// stagedChangedFiles returns the list of staged file paths (absolute).
func stagedChangedFiles(root string) []string {
	return gitDiffNames(root, "--cached")
}

// preCommitChangedFiles returns the file list the pre-commit trigger should
// scan. By default that's the staged set (`git diff --cached`); with
// includeUnstaged=true it adds working-tree changes (`git diff`) so the
// user can preview what the hook will say before running `git add`.
func preCommitChangedFiles(root string, includeUnstaged bool) []string {
	staged := stagedChangedFiles(root)
	if !includeUnstaged {
		return staged
	}
	working := gitDiffNames(root)
	if len(working) == 0 {
		return staged
	}
	seen := map[string]bool{}
	merged := make([]string, 0, len(staged)+len(working))
	for _, p := range staged {
		if !seen[p] {
			seen[p] = true
			merged = append(merged, p)
		}
	}
	for _, p := range working {
		if !seen[p] {
			seen[p] = true
			merged = append(merged, p)
		}
	}
	return merged
}

// gitDiffNames runs `git diff [extraArgs...] --name-only` and returns the
// absolute file paths it emits. Empty on error.
func gitDiffNames(root string, extraArgs ...string) []string {
	args := append([]string{"diff"}, extraArgs...)
	args = append(args, "--name-only")
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		files = append(files, root+"/"+line)
	}
	return files
}
