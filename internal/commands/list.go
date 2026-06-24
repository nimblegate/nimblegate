// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/kits"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// listEntry is one row of `nimblegate list` output. Fields are intentionally
// flat + JSON-stable so a future UI / scripting layer can rely on the
// shape. Anything new goes at the end so older consumers don't break.
type listEntry struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Tier      int      `json:"tier"`
	Severity  string   `json:"severity"`
	Triggers  []string `json:"triggers"`
	Tags      []string `json:"tags"`
	Enabled   bool     `json:"enabled"`
	HasCheck  bool     `json:"has_check"`
	Source    string   `json:"source"` // "stdlib" or "project"
	Lifecycle string   `json:"lifecycle,omitempty"`
	Pattern   string   `json:"pattern,omitempty"`
}

// listOutput is the top-level shape of `nimblegate list --json`. Includes
// metadata so a UI can show "showing 7 of 12" with confidence.
type listOutput struct {
	Frames   []listEntry `json:"frames"`
	Total    int         `json:"total"`    // pre-filter count
	Returned int         `json:"returned"` // post-filter count
	KitFlag  string      `json:"kit,omitempty"`
	TagFlag  string      `json:"tag,omitempty"`
}

// List implements `nimblegate list`. Browses all loaded frames with
// optional --kit / --tag filters and a --json output mode. Always
// reflects the current project's enable-status when run inside a
// project root; outside a project, all frames render as disabled with
// a hint banner.
func List(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	kitFlag := fs.String("kit", "", "filter by kit membership (kit name, e.g. core, web-app)")
	tagFlag := fs.String("tag", "", "filter by tag (frontmatter tags field)")
	asJSON := fs.Bool("json", false, "emit JSON for scripting / future UI")
	enabledOnly := fs.Bool("enabled", false, "show only enabled frames")
	disabledOnly := fs.Bool("disabled", false, "show only disabled frames")
	includeArchived := fs.Bool("include-archived", false, "include lifecycle: archived frames")
	includeDeprecated := fs.Bool("include-deprecated", false, "include lifecycle: deprecated frames")
	includeProposed := fs.Bool("include-proposed", false, "include lifecycle: proposed frames")
	includeAll := fs.Bool("all", false, "include every lifecycle (archived + deprecated + proposed); overrides individual --include-* flags")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *enabledOnly && *disabledOnly {
		fmt.Fprintln(os.Stderr, "nimblegate list: --enabled and --disabled are mutually exclusive")
		return 2
	}
	if *includeAll {
		*includeArchived = true
		*includeDeprecated = true
		*includeProposed = true
	}

	// Project context is best-effort: outside a project we still want to
	// list stdlib frames (useful for "what does nimblegate ship?"). The
	// enabled column then reflects "would be enabled with default
	// config" - every stdlib frame.
	cwd, _ := os.Getwd()
	root, projectErr := paths.FindProjectRoot(cwd)

	stdlibFrames, stdlibErr := stdlib.Load()
	if stdlibErr != nil {
		fmt.Fprintf(os.Stderr, "nimblegate list: stdlib load: %v\n", stdlibErr)
		return 2
	}

	var projectFrames []frames.Frame
	var cfg config.ProjectConfig
	expanded := []string{}
	if projectErr == nil {
		projectFrames, _ = frames.LoadFromDir(paths.AppframesDir(root))
		cfg, _ = config.LoadProject(paths.ConfigPath(root))
		expanded = cfg.Frames.Enabled
	}

	// Filter by kit: precompute the flat member set of the named kit and
	// use it to narrow the list.
	var kitMembers map[string]bool
	if *kitFlag != "" {
		ks, err := kits.LoadStdlib()
		if err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate list: load kits: %v\n", err)
			return 2
		}
		kit, ok := ks.Get(*kitFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "nimblegate list: unknown kit %q (available: %s)\n",
				*kitFlag, strings.Join(ks.Names(), ", "))
			return 2
		}
		kitMembers = map[string]bool{}
		for _, id := range kit.Frames {
			kitMembers[id] = true
		}
	}

	bound := BuiltinCheckFuncs()

	// De-dup by ID (project frames shadow stdlib).
	byID := map[string]listEntry{}
	for _, f := range stdlibFrames {
		byID[f.ID()] = listEntry{
			ID:        f.ID(),
			Category:  string(f.Frontmatter.Category),
			Tier:      f.Frontmatter.EffectiveTier(),
			Severity:  string(f.Frontmatter.Severity),
			Triggers:  append([]string{}, f.Frontmatter.Triggers...),
			Tags:      append([]string{}, f.Frontmatter.Tags...),
			Enabled:   frameEnabledInList(f.ID(), expanded, projectErr),
			HasCheck:  bound[f.ID()] != nil,
			Source:    "stdlib",
			Lifecycle: string(f.Frontmatter.EffectiveLifecycle()),
			Pattern:   f.Frontmatter.Pattern,
		}
	}
	for _, f := range projectFrames {
		// Project severity may override stdlib via [frames.<id>] config;
		// reflect that here.
		sev := string(f.Frontmatter.Severity)
		if ov, ok := cfg.FrameOverrides[f.ID()]; ok && ov.Severity != "" {
			sev = ov.Severity
		}
		byID[f.ID()] = listEntry{
			ID:        f.ID(),
			Category:  string(f.Frontmatter.Category),
			Tier:      f.Frontmatter.EffectiveTier(),
			Severity:  sev,
			Triggers:  append([]string{}, f.Frontmatter.Triggers...),
			Tags:      append([]string{}, f.Frontmatter.Tags...),
			Enabled:   frameEnabledInList(f.ID(), expanded, projectErr),
			HasCheck:  bound[f.ID()] != nil,
			Source:    "project",
			Lifecycle: string(f.Frontmatter.EffectiveLifecycle()),
			Pattern:   f.Frontmatter.Pattern,
		}
	}

	// Materialize + apply filters.
	all := make([]listEntry, 0, len(byID))
	for _, e := range byID {
		all = append(all, e)
	}
	total := len(all)
	filtered := all[:0]
	for _, e := range all {
		if kitMembers != nil && !kitMembers[e.ID] {
			continue
		}
		if *tagFlag != "" && !containsString(e.Tags, *tagFlag) {
			continue
		}
		if *enabledOnly && !e.Enabled {
			continue
		}
		if *disabledOnly && e.Enabled {
			continue
		}
		// Phase 1 Slice 4: hide non-gated lifecycles by default. Users
		// opt in via --include-* / --all to see archived / deprecated /
		// proposed frames in the list output.
		switch e.Lifecycle {
		case string(frames.LifecycleArchived):
			if !*includeArchived {
				continue
			}
		case string(frames.LifecycleDeprecated):
			if !*includeDeprecated {
				continue
			}
		case string(frames.LifecycleProposed):
			if !*includeProposed {
				continue
			}
		}
		filtered = append(filtered, e)
	}
	sort.Slice(filtered, func(i, j int) bool {
		// Lower tier first (1 = most catastrophic); tied tier → ID.
		if filtered[i].Tier != filtered[j].Tier {
			return filtered[i].Tier < filtered[j].Tier
		}
		return filtered[i].ID < filtered[j].ID
	})

	if *asJSON {
		out := listOutput{
			Frames:   filtered,
			Total:    total,
			Returned: len(filtered),
			KitFlag:  *kitFlag,
			TagFlag:  *tagFlag,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate list: json encode: %v\n", err)
			return 2
		}
		return 0
	}

	// Human-readable table. Banner first if we're outside a project.
	if projectErr != nil {
		fmt.Println("(not inside an nimblegate project, showing stdlib catalogue; `enabled` column reflects defaults)")
		fmt.Println()
	}

	// Show the Lifecycle column only when the result set contains any
	// non-default lifecycle. Keeps the default `nimblegate list` output
	// unchanged for users who never archive / deprecate frames.
	showLifecycle := false
	for _, e := range filtered {
		if e.Lifecycle != string(frames.LifecycleActive) && e.Lifecycle != "" {
			showLifecycle = true
			break
		}
	}
	if showLifecycle {
		fmt.Printf("%-44s  %-5s  %-6s  %-7s  %-11s  %s\n", "Frame", "Tier", "Sev", "Enabled", "Lifecycle", "Triggers")
	} else {
		fmt.Printf("%-44s  %-5s  %-6s  %-7s  %s\n", "Frame", "Tier", "Sev", "Enabled", "Triggers")
	}
	for _, e := range filtered {
		enabled := "no"
		if e.Enabled {
			enabled = "yes"
		}
		if showLifecycle {
			fmt.Printf("%-44s  T%-4d  %-6s  %-7s  %-11s  %s\n",
				e.ID, e.Tier, e.Severity, enabled, e.Lifecycle,
				strings.Join(e.Triggers, ","),
			)
		} else {
			fmt.Printf("%-44s  T%-4d  %-6s  %-7s  %s\n",
				e.ID, e.Tier, e.Severity, enabled,
				strings.Join(e.Triggers, ","),
			)
		}
	}
	fmt.Printf("\n%d frame(s) shown", len(filtered))
	if len(filtered) != total {
		fmt.Printf(" (of %d total)", total)
	}
	fmt.Println()
	return 0
}

// frameEnabledInList resolves an ID against the post-expansion enabled
// list, with a safe default for "outside a project" (treat as enabled
// so the catalogue listing isn't all "no").
func frameEnabledInList(id string, expanded []string, projectErr error) bool {
	if projectErr != nil {
		return true // no project context; treat all as enabled in catalogue
	}
	if len(expanded) == 0 {
		return true
	}
	for _, pat := range expanded {
		if pat == id {
			return true
		}
		if strings.HasSuffix(pat, "/*") &&
			strings.HasPrefix(id, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}
