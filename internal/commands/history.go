// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// historyEntry is one row of `nimblegate history list` output.
type historyEntry struct {
	ID            string `json:"id"`
	Category      string `json:"category"`
	Pattern       string `json:"pattern,omitempty"`
	Lifecycle     string `json:"lifecycle"`
	ArchivedAt    string `json:"archived_at,omitempty"`
	ArchiveReason string `json:"archive_reason,omitempty"`
	Source        string `json:"source"`
}

// historyCheckEntry is one row of `nimblegate history check` - an
// archived frame's relevance signal against the current project.
type historyCheckEntry struct {
	ID         string `json:"id"`
	Lifecycle  string `json:"lifecycle"`
	ArchivedAt string `json:"archived_at,omitempty"`
	WouldFire  bool   `json:"would_fire"`
	Outcome    string `json:"outcome"`
	HitCount   int    `json:"hit_count"`
	Reason     string `json:"reason,omitempty"`
}

// History implements `nimblegate history` (subcommands: list, view, search, check).
//
// The history pool is the SET of frames whose lifecycle is `archived` or
// `deprecated` - historical record, not currently gating. Stdlib frames
// can reach this state via their source frontmatter; project frames via
// `nimblegate frame archive`.
//
// Added 2026-05-20 with Phase 1 Slice 3.
func History(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate history: subcommand required (list | view <id> | search <query> | check)")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return historyList(rest)
	case "view":
		return historyView(rest)
	case "search":
		return historySearch(rest)
	case "check":
		return historyCheck(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate history: query the historical (archived/deprecated) frame pool")
		fmt.Println()
		fmt.Println("Usage: nimblegate history <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  list [--json] [--state X]       List archived + deprecated frames (filter by lifecycle)")
		fmt.Println("  view <id>                       Show one archived frame's body + metadata")
		fmt.Println("  search \"<query>\"                Substring search across body / pattern / archive-reason")
		fmt.Println("  check [--json] [--threshold N]  Relevance signal: dry-run archived frames against current")
		fmt.Println("                                  project; suggests revival via `nimblegate frame revive <id>`")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate history: unknown subcommand %q (use list | view | search | check; --help for usage)\n", sub)
		return 2
	}
}

// loadHistoricalFrames returns all frames in archived or deprecated
// lifecycle across stdlib + project sources.
func loadHistoricalFrames() ([]frames.Frame, error) {
	stdFrames, err := stdlib.Load()
	if err != nil {
		return nil, fmt.Errorf("stdlib load: %w", err)
	}

	var all []frames.Frame
	for _, f := range stdFrames {
		all = append(all, f)
	}

	// Project frames are best-effort: missing project is fine here, the
	// stdlib historical set is still useful.
	cwd, _ := os.Getwd()
	if root, err := paths.FindProjectRoot(cwd); err == nil {
		projFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
		all = append(all, projFrames...)
	}

	var hist []frames.Frame
	for _, f := range all {
		lc := f.Frontmatter.EffectiveLifecycle()
		if lc == frames.LifecycleArchived || lc == frames.LifecycleDeprecated {
			hist = append(hist, f)
		}
	}
	return hist, nil
}

func historyList(args []string) int {
	fs := flag.NewFlagSet("history list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	stateFlag := fs.String("state", "", "filter by lifecycle (archived | deprecated); default: both")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	hist, err := loadHistoricalFrames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history list: %v\n", err)
		return 2
	}

	wantState := strings.TrimSpace(*stateFlag)
	entries := make([]historyEntry, 0, len(hist))
	for _, f := range hist {
		lc := string(f.Frontmatter.EffectiveLifecycle())
		if wantState != "" && lc != wantState {
			continue
		}
		source := "stdlib"
		if !strings.HasPrefix(f.SourcePath, "stdlib:") {
			source = "project"
		}
		entries = append(entries, historyEntry{
			ID:            f.ID(),
			Category:      string(f.Frontmatter.Category),
			Pattern:       f.Frontmatter.Pattern,
			Lifecycle:     lc,
			ArchivedAt:    f.Frontmatter.ArchivedAt,
			ArchiveReason: f.Frontmatter.ArchiveReason,
			Source:        source,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(entries)
		return 0
	}

	if len(entries) == 0 {
		if wantState != "" {
			fmt.Printf("No frames in lifecycle %q.\n", wantState)
		} else {
			fmt.Println("No archived or deprecated frames. The history pool is empty.")
		}
		return 0
	}

	fmt.Printf("%-44s  %-10s  %-21s  %s\n", "Frame", "Lifecycle", "Archived at", "Reason")
	for _, e := range entries {
		reason := e.ArchiveReason
		if len(reason) > 40 {
			reason = reason[:37] + "..."
		}
		fmt.Printf("%-44s  %-10s  %-21s  %s\n", e.ID, e.Lifecycle, e.ArchivedAt, reason)
	}
	fmt.Printf("\n%d historical frame(s) shown\n", len(entries))
	return 0
}

func historyView(args []string) int {
	flagArgs, positional := splitFlagsAndPositional(args)
	fs := flag.NewFlagSet("history view", flag.ExitOnError)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate history view: frame ID required")
		return 2
	}
	target := positional[0]

	hist, err := loadHistoricalFrames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history view: %v\n", err)
		return 2
	}
	for _, f := range hist {
		if f.ID() == target {
			fmt.Printf("Frame: %s\n", f.ID())
			fmt.Printf("Category: %s\n", f.Frontmatter.Category)
			fmt.Printf("Severity: %s\n", f.Frontmatter.Severity)
			fmt.Printf("Tier: %d\n", f.Frontmatter.EffectiveTier())
			fmt.Printf("Lifecycle: %s\n", f.Frontmatter.EffectiveLifecycle())
			if f.Frontmatter.Pattern != "" {
				fmt.Printf("Pattern: %s\n", f.Frontmatter.Pattern)
			}
			if f.Frontmatter.ArchivedAt != "" {
				fmt.Printf("Archived at: %s\n", f.Frontmatter.ArchivedAt)
			}
			if f.Frontmatter.ArchiveReason != "" {
				fmt.Printf("Archive reason: %s\n", f.Frontmatter.ArchiveReason)
			}
			fmt.Printf("Source: %s\n", f.SourcePath)
			fmt.Println()
			fmt.Println("⚠ This frame is in the history pool, not actively gating.")
			fmt.Println("  Run `nimblegate history check` to see whether it would fire on current code.")
			fmt.Println("  For project frames, `nimblegate frame revive <id>` re-enables it.")
			if f.Body != "" {
				fmt.Println()
				fmt.Println(f.Body)
			}
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "nimblegate history view: frame %q not found in history pool\n", target)
	return 1
}

func historySearch(args []string) int {
	flagArgs, positional := splitFlagsAndPositional(args)
	fs := flag.NewFlagSet("history search", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}
	if len(positional) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate history search: query required")
		return 2
	}
	query := strings.ToLower(strings.Join(positional, " "))

	hist, err := loadHistoricalFrames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history search: %v\n", err)
		return 2
	}

	type hit struct {
		ID        string `json:"id"`
		Lifecycle string `json:"lifecycle"`
		Snippet   string `json:"snippet"`
	}
	var hits []hit
	for _, f := range hist {
		haystack := strings.ToLower(f.ID() + " " +
			f.Frontmatter.Pattern + " " +
			f.Frontmatter.ArchiveReason + " " +
			string(f.Frontmatter.Category) + " " +
			f.Body)
		if !strings.Contains(haystack, query) {
			continue
		}
		// Build a small snippet around the match in the body when present.
		snippet := f.Frontmatter.ArchiveReason
		idx := strings.Index(strings.ToLower(f.Body), query)
		if idx >= 0 {
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(query) + 60
			if end > len(f.Body) {
				end = len(f.Body)
			}
			snippet = "..." + strings.ReplaceAll(f.Body[start:end], "\n", " ") + "..."
		}
		hits = append(hits, hit{
			ID:        f.ID(),
			Lifecycle: string(f.Frontmatter.EffectiveLifecycle()),
			Snippet:   snippet,
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].ID < hits[j].ID })

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(hits)
		return 0
	}

	if len(hits) == 0 {
		fmt.Printf("No historical frames match %q.\n", query)
		return 0
	}
	fmt.Printf("History search for %q: %d match(es):\n\n", query, len(hits))
	for _, h := range hits {
		fmt.Printf("  %s  (%s)\n", h.ID, h.Lifecycle)
		if h.Snippet != "" {
			fmt.Printf("    %s\n", h.Snippet)
		}
	}
	return 0
}

// historyCheck runs each archived/deprecated frame's CheckFunc against
// the current project (dry-run; no audit log, no gating) and reports
// would-fire counts. Surfaces frames whose underlying risk may have
// returned - candidates for revival.
//
// The user's specific ask 2026-05-20: "could pull from history existing
// back to appframe use if comes relevant and get lot of hits for same."
// This is the relevance signal that informs revive decisions.
func historyCheck(args []string) int {
	fs := flag.NewFlagSet("history check", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	threshold := fs.Int("threshold", 1, "only report frames with at least N would-fire hits")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history check: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history check: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	hist, err := loadHistoricalFrames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate history check: %v\n", err)
		return 2
	}

	checkFns := BuiltinCheckFuncs()

	// Build a CheckContext like `nimblegate check` does. We do NOT use
	// engine.Run because that requires a full engine init + audit setup
	// we don't want for this dry-run query.
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerCLI,
		ProjectRoot:   root,
		WorkingDir:    cwd,
		CurrentBranch: currentGitBranch(root),
	}

	var entries []historyCheckEntry
	for _, f := range hist {
		fn, ok := checkFns[f.ID()]
		if !ok {
			// No check function bound - can't dry-run. Skip with a note.
			continue
		}
		res := fn(ctx)
		fired := res.Outcome != engine.OutcomePass && res.Outcome != engine.OutcomeSkip && res.Outcome != engine.OutcomeError
		hits := len(res.Hits)
		// If the check didn't populate structured hits but DID fire, count as 1.
		if fired && hits == 0 {
			hits = 1
		}
		if hits < *threshold {
			continue
		}
		entries = append(entries, historyCheckEntry{
			ID:         f.ID(),
			Lifecycle:  string(f.Frontmatter.EffectiveLifecycle()),
			ArchivedAt: f.Frontmatter.ArchivedAt,
			WouldFire:  fired,
			Outcome:    string(res.Outcome),
			HitCount:   hits,
			Reason:     res.Reason,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		// Highest hit count first; ties by ID.
		if entries[i].HitCount != entries[j].HitCount {
			return entries[i].HitCount > entries[j].HitCount
		}
		return entries[i].ID < entries[j].ID
	})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(entries)
		return 0
	}

	if len(entries) == 0 {
		fmt.Println("No archived frames would fire on current project. (Threshold:", *threshold, ")")
		fmt.Println("History pool is dormant, nothing to revive.")
		return 0
	}
	fmt.Printf("Archived frames that would fire on current project (threshold %d):\n\n", *threshold)
	fmt.Printf("%-44s  %-6s  %s\n", "Frame", "Hits", "Outcome")
	for _, e := range entries {
		fmt.Printf("%-44s  %-6d  %s\n", e.ID, e.HitCount, e.Outcome)
	}
	fmt.Printf("\n%d archived frame(s) showing relevance. Revive with `nimblegate frame revive <id>`.\n", len(entries))
	return 0
}
