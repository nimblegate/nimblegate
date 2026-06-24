// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/banner"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/incident"
	"nimblegate/internal/paths"
	"nimblegate/internal/state"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/whitelist"
)

// interceptOptions controls one intercept-and-exec invocation.
type interceptOptions struct {
	// cmdName is the leading command word ("git", "apt", "apt-get", "npm", ...).
	// It becomes args[0] in the audit log's target field and is what gets exec'd.
	cmdName string

	// cmdArgs is the rest of the invocation. For `nimblegate git push --force origin main`,
	// this is ["push", "--force", "origin", "main"].
	cmdArgs []string

	// label is the nimblegate subcommand name used in error messages
	// ("nimblegate git" or "nimblegate cmd"). Affects user-visible diagnostics only.
	label string

	// forceYes bypasses checks; the override is recorded to the audit log.
	forceYes bool

	// reason is the human-readable justification recorded alongside an override.
	reason string

	// resolveBinary returns the absolute path of the command to exec.
	// For git, this prefers /usr/bin/git etc.; for arbitrary commands,
	// the generic path is exec.LookPath.
	resolveBinary func(cmdName string) (string, error)
}

// interceptAndExec runs frame checks against the would-be command invocation
// and, if checks pass (or --force-yes was set), execs the real command. It is
// the shared core of `nimblegate git` and `nimblegate cmd`.
func interceptAndExec(opts interceptOptions) int {
	if len(opts.cmdArgs) == 0 && opts.cmdName == "" {
		fmt.Fprintf(os.Stderr, "%s: no command provided\n", opts.label)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		// Outside a nimblegate project - pass through with no checks.
		return execCommand(opts)
	}

	// Pause fast-path: if nimblegate is paused (globally or for this project)
	// fall through to the underlying command without running frames or
	// writing audit entries. Errors reading the state are surfaced to
	// stderr but do not block - fail-closed means enforcement stays on if
	// state is unreadable, which is the same as today's behavior.
	if store, err := state.NewStore(); err == nil {
		if st, err := store.IsPaused(root); err == nil && st.AnyPaused() {
			return execCommand(opts)
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "%s: warning: pause state unreadable: %v\n", opts.label, err)
		}
	}

	stdlibFrames, _ := stdlib.Load()
	projectFrames, loadErrs := frames.LoadFromDir(paths.AppframesDir(root))

	e, err := engine.New(engine.Options{
		ProjectRoot:   root,
		StdlibFrames:  stdlibFrames,
		ProjectFrames: projectFrames,
		CheckFuncs:    BuiltinCheckFuncs(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: engine init: %v\n", opts.label, err)
		return 2
	}
	defer e.Close()

	cmdStr := opts.cmdName
	if len(opts.cmdArgs) > 0 {
		cmdStr += " " + strings.Join(opts.cmdArgs, " ")
	}

	// Banner: rich first-time intro (once per user-per-project), then
	// always-on header on every invocation. Goes to stderr so it doesn't
	// pollute pipes consuming the wrapped command's stdout.
	bctx := banner.Context{
		ProjectRoot:   root,
		ProjectName:   banner.DefaultProjectName(root),
		EnabledGroups: nil, // engine doesn't surface raw group names today; design doc points there
		FrameCount:    len(e.EnabledExpanded),
		Command:       cmdStr,
	}
	bctx.DesignDocPath, bctx.FutureDocPath = banner.DetectDocPaths(root)
	banner.RenderIntro(os.Stderr, bctx)
	banner.RenderHeader(os.Stderr, bctx)

	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    cwd,
		Command:       cmdStr,
		ExcludedDirs:  e.ExcludedDirs(),
		IgnorePath:    e.IgnorePathFunc(),
		CurrentBranch: currentGitBranch(root),
	}

	if opts.forceYes {
		_ = e.Audit.Write(ctx, engine.CheckResult{
			FrameID:  "git-wrap/override",
			Category: frames.CategoryGitSafety,
			Outcome:  engine.OutcomeInfo,
			Reason:   "--force-yes: " + opts.reason,
			Override: true,
		})
		// Explicit + loud confirmation. The bypass is recorded, pattern-
		// matched, and surfaces in `nimblegate audit analyze` - the message
		// says so to discourage agents from looping --force-yes silently.
		reasonShown := opts.reason
		if reasonShown == "" {
			reasonShown = "(no reason given, analyzer will flag this as suspect)"
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "⚠  --force-yes BYPASS RECORDED")
		fmt.Fprintf(os.Stderr, "    Command:  %s\n", cmdStr)
		fmt.Fprintf(os.Stderr, "    Reason:   %s\n", reasonShown)
		fmt.Fprintf(os.Stderr, "    Logged:   %s\n", filepath.Join(root, ".appframes", "audit.parts/audit.<pid>.log"))
		fmt.Fprintln(os.Stderr, "    This bypass will surface in `nimblegate audit analyze` reports.")
		fmt.Fprintln(os.Stderr, "    Vague reason text + repeated --force-yes on the same gate flag")
		fmt.Fprintln(os.Stderr, "    as suspect. If this bypass is legitimate, the reason should be.")
		fmt.Fprintln(os.Stderr, "")
		maybePromptCaptureIncident(root, opts, cmdStr)
		return execCommand(opts)
	}

	results := engine.Run(e.Registry, ctx)
	for _, r := range results {
		_ = e.Audit.Write(ctx, r)
	}

	wl, err := whitelist.LoadFromProject(root, knownIDsWithLinters(e), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", opts.label, err)
		return 2
	}
	filtered, suppressed := engine.ApplyWhitelist(results, wl, root)
	for _, s := range suppressed {
		_ = e.Audit.WriteSuppression(ctx, s)
	}

	engine.FormatLoadWarnings(os.Stdout, loadErrs)
	exit := engine.FormatResults(os.Stdout, filtered)
	if exit != 0 {
		return exit
	}

	// Production-boundary gate: `git push` is the publish boundary. Commit
	// freely, but a fresh full-tree check here blocks the push if any open
	// (non-deferred) BLOCK findings remain - so dangerous code can't ship
	// silently. --force-yes already returned above, so it bypasses this too.
	if isGitPush(opts) {
		if code := prodBoundaryGate(e, root, cwd); code != 0 {
			return code
		}
	}
	return execCommand(opts)
}

// execCommand resolves the binary, hooks up stdio, runs it, and propagates
// its exit code.
func execCommand(opts interceptOptions) int {
	bin, err := opts.resolveBinary(opts.cmdName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", opts.label, err)
		return 2
	}
	cmd := exec.Command(bin, opts.cmdArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "%s: exec %s: %v\n", opts.label, bin, err)
		return 2
	}
	return 0
}

// resolveGitBinary prefers known system paths for git, then falls back to PATH.
func resolveGitBinary(cmdName string) (string, error) {
	for _, candidate := range []string{"/usr/bin/git", "/usr/local/bin/git", "/opt/homebrew/bin/git"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	path, err := exec.LookPath(cmdName)
	if err != nil {
		return "", fmt.Errorf("no %s binary found on PATH", cmdName)
	}
	return path, nil
}

// resolveBinaryFromPATH is the generic resolver - just looks up the command name.
func resolveBinaryFromPATH(cmdName string) (string, error) {
	path, err := exec.LookPath(cmdName)
	if err != nil {
		return "", fmt.Errorf("no %s binary found on PATH", cmdName)
	}
	return path, nil
}

// stdinIsTTY reports whether stdin is connected to a terminal (vs piped /
// redirected). Used to gate the post-bypass incident-capture prompt so it
// never fires from CI or scripted invocations.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// maybePromptCaptureIncident asks "capture this bypass as an incident?" at
// the end of a --force-yes invocation. Silent (no prompt) when:
//   - stdin is not a TTY (CI safety),
//   - APPFRAMES_NO_INCIDENT_PROMPT is set (escape hatch for users who never
//     want the prompt),
//   - bare-default suppression: APPFRAMES_INCIDENT_PROMPT=off.
//
// On yes, scaffolds an incident with source=bypass and the reason/frame/
// command pre-populated, then prints the path.
func maybePromptCaptureIncident(projectRoot string, opts interceptOptions, cmdStr string) {
	if os.Getenv("APPFRAMES_NO_INCIDENT_PROMPT") != "" {
		return
	}
	if strings.EqualFold(os.Getenv("APPFRAMES_INCIDENT_PROMPT"), "off") {
		return
	}
	if !stdinIsTTY() {
		return
	}
	fmt.Fprint(os.Stderr, "nimblegate: capture this bypass as an incident? [y/N] ")
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return
	}
	answer := strings.ToLower(strings.TrimSpace(sc.Text()))
	if answer != "y" && answer != "yes" {
		return
	}

	// Build a useful default title from the command's first verb + the
	// frame ID isn't known here (override was recorded for the wrap, not the
	// specific blocked frame). Best-effort: pull the leading verb (e.g.
	// "push --force" → "push") and combine with the wrap context.
	title := strings.TrimSpace(cmdStr)
	if title == "" {
		title = "bypass capture"
	}
	if len(title) > 80 {
		title = title[:77] + "..."
	}

	now := time.Now().UTC()
	draft := incident.NewDraft(incident.NewDraftOptions{
		Title:         title,
		Date:          now,
		Source:        incident.SourceBypass,
		SourceFrame:   opts.cmdName + "-wrap/override",
		SourceReason:  opts.reason,
		SourceCommand: cmdStr,
	})
	incDir := filepath.Join(paths.AppframesDir(projectRoot), incident.IncidentsDirName)
	if err := os.MkdirAll(incDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate: capture incident: %v\n", err)
		return
	}
	dst := filepath.Join(incDir, incident.Filename(now, incident.Slugify(title)))
	// If a same-day same-title slug already exists, append a counter; the
	// failure mode here is benign so we fall back gracefully rather than
	// dropping the capture.
	for i := 2; i < 100; i++ {
		if _, err := os.Stat(dst); err != nil {
			break
		}
		dst = filepath.Join(incDir, incident.Filename(now, fmt.Sprintf("%s-%d", incident.Slugify(title), i)))
	}
	draft.SourcePath = dst
	if err := draft.Write(); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate: capture incident: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "nimblegate: captured at %s\n", dst)
	fmt.Fprintf(os.Stderr, "  next: edit it, then `nimblegate incident promote %s --category <cat> --name <name> --tier <N> --severity <BLOCK|WARN|INFO> --triggers <comma>`\n", draft.Slug())
}
