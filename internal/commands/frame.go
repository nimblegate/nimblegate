// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/selection"
	"nimblegate/internal/stdlib"
)

// Frame implements `nimblegate frame` (subcommands: test, archive, revive).
// Added 2026-05-20 with Phase 1 Slice 2; archive/revive added with Slice 3.
func Frame(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate frame: subcommand required (test | archive <id> | revive <id>)")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "test":
		return frameTest(rest)
	case "archive":
		return frameArchive(rest)
	case "revive":
		return frameRevive(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate frame: frame management (negative-selection runner + lifecycle transitions)")
		fmt.Println()
		fmt.Println("Usage: nimblegate frame <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  test <id> [--write-grade] [--json]      Run frame's positive/negative testdata corpus")
		fmt.Println("  archive <id> [--reason \"...\"] [--move]  Mark project frame archived (frontmatter + optional file move)")
		fmt.Println("  revive <id> [--move]                    Reverse archive, mark active again")
		fmt.Println()
		fmt.Println("Note: archive / revive operate on project-local frames only. Stdlib frames are read-only embeds.")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate frame: unknown subcommand %q (use test | archive | revive; --help for usage)\n", sub)
		return 2
	}
}

// frameTest runs the named frame against its testdata corpus and
// reports per-case results + computed selection grade.
//
// Default: report-only. Pass --write-grade to update the frame's
// frontmatter with the computed grade + stats. Opt-in by design so CI
// runs don't accidentally rewrite source files.
func frameTest(args []string) int {
	fs := flag.NewFlagSet("frame test", flag.ExitOnError)
	writeGrade := fs.Bool("write-grade", false, "update frame frontmatter with computed grade + stats")
	asJSON := fs.Bool("json", false, "emit JSON output")
	// Separate the positional frame ID from flags so flags can appear in any
	// order around it. Go's default flag.Parse stops at the first non-flag
	// argument, which surprises users who write `frame test <id> --write-grade`.
	flagArgs, positional := splitFlagsAndPositional(args)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate frame test: frame ID required")
		return 2
	}
	target := positional[0]

	stdFrames, err := stdlib.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: stdlib load: %v\n", err)
		return 2
	}

	var targetFrame *frames.Frame
	for i := range stdFrames {
		if stdFrames[i].ID() == target {
			targetFrame = &stdFrames[i]
			break
		}
	}
	if targetFrame == nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: frame %q not found in stdlib\n", target)
		return 1
	}

	checkFns := BuiltinCheckFuncs()
	fn, ok := checkFns[target]
	if !ok {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: frame %q has no check function bound\n", target)
		return 1
	}

	testdataFS, hasTestdata := stdlib.TestdataFS(target)
	if !hasTestdata {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: no testdata for %q (frame stays at %q grade)\n",
			target, targetFrame.Frontmatter.SelectionGrade)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(selection.RunResult{FrameID: target, Grade: "pending"})
		}
		return 1
	}

	// Wrap embedded fs.FS into the runner's expected interface.
	runFS, ok := testdataFS.(selection.FS)
	if !ok {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: testdata fs does not implement required interface (internal error)\n")
		return 2
	}

	result, err := selection.Run(target, fn, runFS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame test: run failed: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate frame test: json encode: %v\n", err)
			return 2
		}
	} else {
		renderRunResult(os.Stdout, result)
	}

	if *writeGrade {
		if err := writeGradeBack(targetFrame, result); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate frame test: write-grade failed: %v\n", err)
			return 2
		}
		if !*asJSON {
			fmt.Printf("\nWrote grade %q to frame frontmatter.\n", result.Grade)
		}
	}

	if result.Grade == "failing" {
		return 1
	}
	return 0
}

func renderRunResult(w io.Writer, r selection.RunResult) {
	fmt.Fprintf(w, "Frame: %s\n", r.FrameID)
	fmt.Fprintf(w, "Grade: %s\n", r.Grade)
	if r.PositivesTotal > 0 || r.NegativesTotal > 0 {
		fmt.Fprintf(w, "Positives: %d/%d  Negatives: %d/%d\n",
			r.PositivesPassed, r.PositivesTotal,
			r.NegativesPassed, r.NegativesTotal)
	}
	if len(r.Cases) > 0 {
		fmt.Fprintln(w)
		for _, c := range r.Cases {
			status := "OK"
			if !c.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(w, "  [%-4s] %-9s %-12s  %s\n", status, c.Kind, c.Outcome, c.Filename)
			if !c.Passed && c.Reason != "" {
				fmt.Fprintf(w, "         reason: %s\n", c.Reason)
			}
		}
	}
}

// writeGradeBack updates the frame's source file frontmatter with the
// computed grade + stats. The source path is derived from the embed
// source - for stdlib frames this lives under internal/stdlib/frames/...
//
// The function locates the source file by mapping "stdlib:..." paths
// back to disk paths relative to the current working directory. Only
// works when run from the repo root (i.e. dev mode), which is the
// supported context for --write-grade.
func writeGradeBack(f *frames.Frame, result selection.RunResult) error {
	srcPath := strings.TrimPrefix(f.SourcePath, "stdlib:")
	// Stdlib frame paths embed-relative to "frames/" - prepend "frames" to
	// reach the on-disk source under internal/stdlib/.
	diskPath := filepath.Join("internal", "stdlib", "frames", srcPath)
	data, err := os.ReadFile(diskPath)
	if err != nil {
		return fmt.Errorf("read %s: %w (run --write-grade from repo root)", diskPath, err)
	}

	stats := frames.SelectionStats{
		Positives: fmt.Sprintf("%d/%d", result.PositivesPassed, result.PositivesTotal),
		Negatives: fmt.Sprintf("%d/%d", result.NegativesPassed, result.NegativesTotal),
		LastRun:   result.LastRun.UTC().Format(time.RFC3339),
	}

	updated, err := rewriteFrontmatter(string(data), result.Grade, stats)
	if err != nil {
		return err
	}
	if err := os.WriteFile(diskPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", diskPath, err)
	}
	return nil
}

// frameArchive flips a project-local frame to lifecycle: archived. Updates
// frontmatter (archived-at, archive-reason), optionally moves the file
// into .appframes/history/<basename>. Stdlib frames are read-only embeds
// and cannot be archived via CLI - their lifecycle is set in source.
func frameArchive(args []string) int {
	flagArgs, positional := splitFlagsAndPositional(args)
	fs := flag.NewFlagSet("frame archive", flag.ExitOnError)
	reason := fs.String("reason", "", "free-text reason for archiving (referenced incident, supersession, etc.)")
	move := fs.Bool("move", false, "physically move the file to .appframes/history/<basename>")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate frame archive: frame ID required")
		return 2
	}
	target := positional[0]

	root, err := projectRootFromCwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame archive: %v\n", err)
		return 2
	}

	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	var found *frames.Frame
	for i := range projectFrames {
		if projectFrames[i].ID() == target {
			found = &projectFrames[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame archive: %q is not a project frame (stdlib frames are read-only embeds; archive their source instead)\n", target)
		return 1
	}

	srcPath := found.SourcePath
	data, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame archive: read %s: %v\n", srcPath, err)
		return 2
	}

	updated, err := rewriteForArchive(string(data), *reason, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame archive: %v\n", err)
		return 2
	}

	destPath := srcPath
	if *move {
		historyDir := filepath.Join(paths.AppframesDir(root), "history")
		if err := os.MkdirAll(historyDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate frame archive: mkdir %s: %v\n", historyDir, err)
			return 2
		}
		destPath = filepath.Join(historyDir, filepath.Base(srcPath))
	}

	if err := os.WriteFile(destPath, []byte(updated), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame archive: write %s: %v\n", destPath, err)
		return 2
	}
	if *move && destPath != srcPath {
		if err := os.Remove(srcPath); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate frame archive: remove original %s: %v\n", srcPath, err)
			return 2
		}
	}
	if *move {
		fmt.Printf("Archived %s → %s\n", target, destPath)
	} else {
		fmt.Printf("Archived %s (frontmatter only; file remains at %s)\n", target, srcPath)
	}
	return 0
}

// frameRevive flips a project-local frame back to lifecycle: active.
// Clears archived-at and archive-reason. Stdlib frames cannot be revived
// via CLI.
func frameRevive(args []string) int {
	flagArgs, positional := splitFlagsAndPositional(args)
	fs := flag.NewFlagSet("frame revive", flag.ExitOnError)
	move := fs.Bool("move", false, "if file is in .appframes/history/, move it back to .appframes/frames/")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate frame revive: frame ID required")
		return 2
	}
	target := positional[0]

	root, err := projectRootFromCwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame revive: %v\n", err)
		return 2
	}

	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	var found *frames.Frame
	for i := range projectFrames {
		if projectFrames[i].ID() == target {
			found = &projectFrames[i]
			break
		}
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame revive: project frame %q not found\n", target)
		return 1
	}

	srcPath := found.SourcePath
	data, err := os.ReadFile(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame revive: read %s: %v\n", srcPath, err)
		return 2
	}

	updated, err := rewriteForRevive(string(data))
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame revive: %v\n", err)
		return 2
	}

	destPath := srcPath
	if *move {
		historyDir := filepath.Join(paths.AppframesDir(root), "history")
		// Only move if currently under history dir.
		rel, _ := filepath.Rel(historyDir, srcPath)
		if !strings.HasPrefix(rel, "..") && rel != "" {
			framesDir := filepath.Join(paths.AppframesDir(root), "frames")
			if err := os.MkdirAll(framesDir, 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate frame revive: mkdir %s: %v\n", framesDir, err)
				return 2
			}
			destPath = filepath.Join(framesDir, filepath.Base(srcPath))
		}
	}

	if err := os.WriteFile(destPath, []byte(updated), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate frame revive: write %s: %v\n", destPath, err)
		return 2
	}
	if destPath != srcPath {
		if err := os.Remove(srcPath); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate frame revive: remove original %s: %v\n", srcPath, err)
			return 2
		}
	}
	if destPath != srcPath {
		fmt.Printf("Revived %s → %s\n", target, destPath)
	} else {
		fmt.Printf("Revived %s (frontmatter only; file remains at %s)\n", target, srcPath)
	}
	return 0
}

func projectRootFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		return "", fmt.Errorf("%w (hint: run `nimblegate init` here)", err)
	}
	return root, nil
}

// rewriteForArchive sets lifecycle: archived + archived-at + optional
// archive-reason in the frontmatter. Replaces existing values; preserves
// all other fields.
func rewriteForArchive(content, reason, when string) (string, error) {
	return rewriteFrontmatterFields(content, map[string]string{
		"lifecycle":      "archived",
		"archived-at":    when,
		"archive-reason": reason,
	}, nil)
}

// rewriteForRevive sets lifecycle: active and clears archived-at +
// archive-reason. Other fields preserved.
func rewriteForRevive(content string) (string, error) {
	return rewriteFrontmatterFields(content, map[string]string{
		"lifecycle": "active",
	}, []string{"archived-at", "archive-reason"})
}

// rewriteFrontmatterFields is a small frontmatter field editor: set the
// given key/value pairs (replacing or inserting), and delete the given
// keys. Preserves all other fields exactly. Empty values for set-keys
// are treated as deletions (cleaner than writing `field:` with no value).
func rewriteFrontmatterFields(content string, set map[string]string, deletes []string) (string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing opening frontmatter fence")
	}
	rest := content[4:]
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx < 0 {
		endIdx = strings.Index(rest, "\n---")
		if endIdx < 0 {
			return "", fmt.Errorf("missing closing frontmatter fence")
		}
	}
	fmText := rest[:endIdx]
	after := rest[endIdx:]

	deleteSet := map[string]bool{}
	for _, d := range deletes {
		deleteSet[d] = true
	}
	for k, v := range set {
		if v == "" {
			deleteSet[k] = true
			delete(set, k)
		}
	}

	lines := strings.Split(fmText, "\n")
	var out []string
	seen := map[string]bool{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		colonIdx := strings.Index(trimmed, ":")
		if colonIdx <= 0 {
			out = append(out, line)
			continue
		}
		key := trimmed[:colonIdx]
		if deleteSet[key] {
			continue
		}
		if v, ok := set[key]; ok {
			out = append(out, key+": "+v)
			seen[key] = true
			continue
		}
		out = append(out, line)
	}
	// Append any set-keys that weren't found in the existing frontmatter.
	for k, v := range set {
		if !seen[k] {
			out = append(out, k+": "+v)
		}
	}

	return "---\n" + strings.Join(out, "\n") + after, nil
}

// splitFlagsAndPositional separates flag-like args from positional args.
// Flag-like = starts with "-". Positional = everything else. Used to make
// flag order around the positional frame ID irrelevant.
//
// Caveat: this is order-agnostic but value-of-flag-aware. `--write-grade`
// is a bool flag; if a future flag takes a value (`--out=path`), the
// `=` form keeps it together. The space form (`--out path`) would still
// work since `--out` starts with `-` and `path` doesn't.
func splitFlagsAndPositional(args []string) (flags []string, positional []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}
	return flags, positional
}

// rewriteFrontmatter replaces or inserts selection-grade and
// selection-stats fields inside the YAML frontmatter of a frame markdown
// file. Preserves all other fields exactly. Deliberately string-based
// (not YAML round-trip) to avoid reformatting unrelated fields.
func rewriteFrontmatter(content, grade string, stats frames.SelectionStats) (string, error) {
	if !strings.HasPrefix(content, "---\n") {
		return "", fmt.Errorf("missing opening frontmatter fence")
	}
	// Find closing fence
	rest := content[4:]
	endIdx := strings.Index(rest, "\n---\n")
	if endIdx < 0 {
		// File may end with --- (no trailing newline)
		endIdx = strings.Index(rest, "\n---")
		if endIdx < 0 {
			return "", fmt.Errorf("missing closing frontmatter fence")
		}
	}
	fmText := rest[:endIdx]
	after := rest[endIdx:]

	// Build replacement frontmatter: walk lines, replace grade line,
	// drop any existing selection-stats block, then append new values.
	lines := strings.Split(fmText, "\n")
	var out []string
	skippingStatsBlock := false
	gradeReplaced := false

	for _, line := range lines {
		if skippingStatsBlock {
			// Lines indented as a YAML map continuation belong to the
			// stats block; skip them. The block ends when we hit a
			// non-indented line.
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				continue
			}
			skippingStatsBlock = false
		}
		if strings.HasPrefix(line, "selection-grade:") {
			out = append(out, "selection-grade: "+grade)
			gradeReplaced = true
			continue
		}
		if strings.HasPrefix(line, "selection-stats:") {
			skippingStatsBlock = true
			continue
		}
		out = append(out, line)
	}

	if !gradeReplaced {
		out = append(out, "selection-grade: "+grade)
	}
	if stats.Positives != "" || stats.Negatives != "" || stats.LastRun != "" {
		out = append(out, "selection-stats:")
		if stats.Positives != "" {
			out = append(out, "  positives: "+stats.Positives)
		}
		if stats.Negatives != "" {
			out = append(out, "  negatives: "+stats.Negatives)
		}
		if stats.LastRun != "" {
			out = append(out, "  last-run: "+stats.LastRun)
		}
	}

	return "---\n" + strings.Join(out, "\n") + after, nil
}
