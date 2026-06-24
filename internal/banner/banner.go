// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package banner renders the nimblegate context messages shown when a
// gated command runs: a short header on every invocation, and a rich
// first-time-per-user-per-project intro that explains what nimblegate is
// and - explicitly - how NOT to bypass it.
//
// The first-time state is tracked via a marker file at
// ~/.appframes/seen/<sha-of-project-path>. Each user-project pair sees
// the intro exactly once across the lifetime of that marker.
package banner

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// QuietEnv is the env var that suppresses the per-invocation header line.
// When set (any non-empty value), only the rich first-time intro and the
// frame results are printed; the always-on header is omitted.
//
// The first-time intro is NOT suppressed by APPFRAMES_QUIET - it shows
// once per user-per-project regardless. The point of the intro is that
// every contributor sees it; silencing that would defeat the design.
const QuietEnv = "APPFRAMES_QUIET"

// SeenDir is the per-user directory where marker files live. Returned as
// an absolute path. Created lazily by MarkSeen.
func SeenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("banner: resolve home: %w", err)
	}
	return filepath.Join(home, ".appframes", "seen"), nil
}

// markerPath returns the absolute path of the seen-marker for the given
// project. The marker name is a sha256 of the project's absolute path -
// distinct per project, stable across renames of the user's home dir,
// short enough to be a clean filename.
func markerPath(projectRoot string) (string, error) {
	abs, err := filepath.Abs(projectRoot)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(abs))
	dir, err := SeenDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".seen"), nil
}

// HasSeen reports whether this user has already seen the intro for this
// project. Returns false on any error (a missing marker is the common
// case; we lean toward showing the intro rather than silently swallowing
// it).
func HasSeen(projectRoot string) bool {
	path, err := markerPath(projectRoot)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// MarkSeen creates the marker file so subsequent invocations skip the
// intro. Best-effort: errors are returned for the caller's awareness but
// the calling code should not abort on failure (a missing marker just
// means the intro shows again next time, no worse than before).
func MarkSeen(projectRoot string) error {
	path, err := markerPath(projectRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("banner: mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("banner: write %s: %w", path, err)
	}
	defer f.Close()
	_, _ = f.WriteString(projectRoot + "\n")
	return nil
}

// Context describes what RenderHeader and RenderIntro need to know about
// the current invocation: the project, the active groups, and the
// command being gated.
type Context struct {
	ProjectRoot   string   // absolute path to the project
	ProjectName   string   // human display name (last path segment by default)
	EnabledGroups []string // e.g. ["@tier-1", "@cf-pages"]
	FrameCount    int      // total frames currently enabled
	Command       string   // the command being gated (e.g. "git push --force")
	DesignDocPath string   // relative path to _design.md if present, else ""
	FutureDocPath string   // relative path to _future.md if present, else ""
}

// RenderHeader writes the always-on, one-line context header. Quiet when
// APPFRAMES_QUIET is set.
//
// Format: `nimblegate ▸ <project> ▸ <design-doc> ▸ checking <cmd>...`
//
// Always written to stderr (so it doesn't pollute stdout pipes).
func RenderHeader(w io.Writer, ctx Context) {
	if os.Getenv(QuietEnv) != "" {
		return
	}
	parts := []string{"nimblegate"}
	if ctx.ProjectName != "" {
		parts = append(parts, ctx.ProjectName)
	}
	if ctx.DesignDocPath != "" {
		parts = append(parts, ctx.DesignDocPath)
	}
	if ctx.Command != "" {
		parts = append(parts, "checking "+ctx.Command)
	}
	fmt.Fprintf(w, "🛡  %s\n", strings.Join(parts, " ▸ "))
}

// RenderIntro writes the first-time-per-user-per-project rich banner.
// Idempotent - caller can call this unconditionally; this function
// checks the marker and short-circuits when already seen.
//
// On show, marks-seen so subsequent invocations skip silently. Returns
// true when the intro was written (i.e. first time); false otherwise.
//
// The content emphasizes that bypassing is recorded, pattern-matched,
// and surfaces in `audit analyze` - to discourage agents from looping
// --force-yes silently.
func RenderIntro(w io.Writer, ctx Context) bool {
	if HasSeen(ctx.ProjectRoot) {
		return false
	}
	writeIntro(w, ctx)
	_ = MarkSeen(ctx.ProjectRoot)
	return true
}

// RenderIntroForced writes the rich intro regardless of marker state.
// Used by `nimblegate intro` so an agent or user can re-read the banner
// on demand (e.g. agent at session start runs `nimblegate intro` to load
// context).
func RenderIntroForced(w io.Writer, ctx Context) {
	writeIntro(w, ctx)
}

// RenderIntroAgent writes a terse agent-targeted version of the intro
// - no decorative rules, no paragraphs, just the load-bearing facts in
// ~25 lines. Used by `nimblegate intro --for-agent` for AI agents that
// need to load project context quickly at session start without burning
// tokens on banner ASCII.
//
// Added 2026-05-21 with Slice 11. Pairs with the rich human-targeted
// version (RenderIntroForced); both surface the same anti-bypass
// posture, but the agent version is structured as a comment-prefixed
// machine-readable brief.
func RenderIntroAgent(w io.Writer, ctx Context) {
	fmt.Fprintf(w, "# nimblegate - agent brief for project: %s\n", ctx.ProjectName)
	fmt.Fprintln(w, "# Gated commands route through nimblegate. Bypasses are recorded.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "active-frames: %d\n", ctx.FrameCount)
	if len(ctx.EnabledGroups) > 0 {
		fmt.Fprintf(w, "active-groups: %s\n", strings.Join(ctx.EnabledGroups, ", "))
	} else {
		fmt.Fprintln(w, "active-groups: (none - direct frame list in appframes.toml)")
	}
	if ctx.DesignDocPath != "" {
		fmt.Fprintf(w, "design-doc:    %s\n", ctx.DesignDocPath)
	} else {
		fmt.Fprintln(w, "design-doc:    (none - consider authoring .appframes/_design.md)")
	}
	if ctx.FutureDocPath != "" {
		fmt.Fprintf(w, "future-doc:    %s\n", ctx.FutureDocPath)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# Load order at session start:")
	fmt.Fprintln(w, "#   nimblegate intro --for-agent   (this output)")
	if ctx.DesignDocPath != "" {
		fmt.Fprintf(w, "#   cat %s   (architectural intent)\n", ctx.DesignDocPath)
	}
	fmt.Fprintln(w, "#   nimblegate patterns list       (patterns + instance counts)")
	fmt.Fprintln(w, "#   nimblegate list                (active frames + status)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# When a frame BLOCKs:")
	fmt.Fprintln(w, "#   1. Read the reason text - it explains the issue.")
	fmt.Fprintln(w, "#   2. Fix the underlying cause (right answer ~95% of the time).")
	fmt.Fprintln(w, "#   3. If bypass is genuinely legitimate, supply a specific reason:")
	fmt.Fprintln(w, "#        nimblegate git --force-yes --reason=\"specific-justification\" <cmd>")
	fmt.Fprintln(w, "#      Every --force-yes is audit-logged with its reason.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# DO NOT (each is logged + flagged by `nimblegate audit analyze`):")
	fmt.Fprintln(w, "#   - /usr/bin/git directly (skips the shim)")
	fmt.Fprintln(w, "#   - PATH editing to bypass the shims directory")
	fmt.Fprintln(w, "#   - git commit --no-verify (Tier-1 frame blocks; shim catches)")
	fmt.Fprintln(w, "#   - Repeated --force-yes on the same gate")
	fmt.Fprintln(w, "#   - Disabling frames in appframes.toml without recording why")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# Capture footguns this catalog missed:")
	fmt.Fprintln(w, "#   nimblegate incident new --title \"...\"")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# Health check (is nimblegate itself functional?):")
	fmt.Fprintln(w, "#   nimblegate doctor")
}

// writeIntro is the actual rendering. Separated so RenderIntro and
// RenderIntroForced share the body without duplication.
func writeIntro(w io.Writer, ctx Context) {
	const rule = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	fmt.Fprintln(w, rule)
	if ctx.ProjectName != "" {
		fmt.Fprintf(w, " nimblegate gating active for project: %s\n", ctx.ProjectName)
	} else {
		fmt.Fprintln(w, " nimblegate gating active")
	}
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This project gates destructive git / system commands AND verifies")
	fmt.Fprintln(w, "code against architectural rules. Every command that flows through")
	fmt.Fprintln(w, "nimblegate - including bypasses - is recorded in .appframes/audit.log.")
	fmt.Fprintln(w)
	if ctx.FrameCount > 0 {
		fmt.Fprintf(w, "%d frame(s) active", ctx.FrameCount)
		if len(ctx.EnabledGroups) > 0 {
			fmt.Fprintf(w, " in groups: %s", strings.Join(ctx.EnabledGroups, ", "))
		}
		fmt.Fprintln(w, ".")
		fmt.Fprintln(w)
	}
	if ctx.DesignDocPath != "" {
		fmt.Fprintf(w, "Read first:   %s   (project rules + boundaries)\n", ctx.DesignDocPath)
	}
	if ctx.FutureDocPath != "" {
		fmt.Fprintf(w, "Parked ideas: %s   (validation signals before building)\n", ctx.FutureDocPath)
	}
	fmt.Fprintln(w, "Full catalog: nimblegate list")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "⚠  DO NOT silently route around these gates.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Every command flowing through nimblegate is logged. Bypass attempts")
	fmt.Fprintln(w, "(--force-yes) are recorded with their reason. The audit analyzer")
	fmt.Fprintln(w, "clusters reason text and flags repeated bypasses - vague reasons")
	fmt.Fprintln(w, "like \"test\" or \"fix\" stand out, and looping --force-yes on the")
	fmt.Fprintln(w, "same gate is the most visible pattern of all.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "If you hit a gate:")
	fmt.Fprintln(w, "  1. Read the frame's reason text - it explains what's wrong.")
	fmt.Fprintln(w, "  2. Fix the underlying issue (right answer ~95% of the time).")
	fmt.Fprintln(w, "  3. IF the bypass is legitimate, supply a specific --reason:")
	fmt.Fprintln(w, "       nimblegate git --force-yes --reason=\"specific-justification\" ...")
	fmt.Fprintln(w, "     Use one --force-yes only when you actually mean it.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "DO NOT:")
	fmt.Fprintln(w, "  • Invoke /usr/bin/git directly to skip the shim (operator-only action)")
	fmt.Fprintln(w, "  • Edit PATH to bypass the shims directory (operator-only action)")
	fmt.Fprintln(w, "  • Silently disable frames in appframes.toml without recording why")
	fmt.Fprintln(w, "  • Repeatedly --force-yes the same gate (the analyzer flags this)")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "This message shows once per user, per project. Re-read with:")
	fmt.Fprintln(w, "    nimblegate intro")
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
}

// DefaultProjectName returns a project name suitable for display: the
// last path segment of an absolute path. Falls back to the full path if
// the segment isn't useful (empty, ".", "/").
func DefaultProjectName(projectRoot string) string {
	clean := filepath.Clean(projectRoot)
	base := filepath.Base(clean)
	switch base {
	case "", ".", "/":
		return clean
	}
	return base
}

// DetectDocPaths returns relative paths to .appframes/_design.md and
// .appframes/_future.md if either exists. Empty strings when absent.
// Caller passes the project root.
func DetectDocPaths(projectRoot string) (designRel, futureRel string) {
	for _, candidate := range []struct {
		rel string
		out *string
	}{
		{".appframes/_design.md", &designRel},
		{".appframes/_future.md", &futureRel},
	} {
		if _, err := os.Stat(filepath.Join(projectRoot, candidate.rel)); err == nil {
			*candidate.out = candidate.rel
		}
	}
	return designRel, futureRel
}
