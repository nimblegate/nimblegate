// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"nimblegate/internal/stdlib"
)

// patternListEntry is one row of `nimblegate patterns list` output.
// Fields are intentionally flat + JSON-stable for future UI / scripting.
type patternListEntry struct {
	ID                  string   `json:"id"`
	Description         string   `json:"description"`
	AnticipatedSiblings []string `json:"anticipated_siblings,omitempty"`
	InstanceFrames      []string `json:"instance_frames,omitempty"`
	Source              string   `json:"source"`
}

// Patterns implements `nimblegate patterns` (subcommand: list | view).
//
// Patterns are the canonical/parent frames added 2026-05-20 with the
// Phase 1 architecture. Each frame declares a parent pattern via its
// `pattern:` frontmatter field; patterns themselves don't fire as
// gates, they document the structural shape that instance frames detect.
func Patterns(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate patterns: subcommand required (list | view <id>)")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return patternsList(rest)
	case "view":
		return patternsView(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate patterns: patterns are canonical/parent frames; instance frames are children")
		fmt.Println()
		fmt.Println("Usage: nimblegate patterns <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  list [--json]      List all patterns + instance frame counts")
		fmt.Println("  view <id>          Show one pattern (body + bound instance frames)")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate patterns: unknown subcommand %q (use list | view; --help for usage)\n", sub)
		return 2
	}
}

func patternsList(args []string) int {
	fs := flag.NewFlagSet("patterns list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate patterns list: %v\n", err)
		return 2
	}
	stdFrames, err := stdlib.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate patterns list: %v\n", err)
		return 2
	}

	// Build pattern -> [frame ID...] index so each pattern row lists
	// the instance frames currently bound to it. Helps the user see at
	// a glance which patterns are well-populated and which are stubs.
	instanceIndex := map[string][]string{}
	for _, f := range stdFrames {
		if f.Frontmatter.Pattern != "" {
			instanceIndex[f.Frontmatter.Pattern] = append(instanceIndex[f.Frontmatter.Pattern], f.ID())
		}
	}

	entries := make([]patternListEntry, 0, len(patterns))
	for _, p := range patterns {
		insts := append([]string{}, instanceIndex[p.ID()]...)
		sort.Strings(insts)
		entries = append(entries, patternListEntry{
			ID:                  p.ID(),
			Description:         p.Frontmatter.Description,
			AnticipatedSiblings: append([]string{}, p.Frontmatter.AnticipatedSiblings...),
			InstanceFrames:      insts,
			Source:              "stdlib",
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ID < entries[j].ID
	})

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entries); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate patterns list: json encode: %v\n", err)
			return 2
		}
		return 0
	}

	fmt.Printf("%-44s  %-3s  %s\n", "Pattern", "N", "Description")
	for _, e := range entries {
		desc := e.Description
		if len(desc) > 78 {
			desc = desc[:75] + "..."
		}
		fmt.Printf("%-44s  %-3d  %s\n", e.ID, len(e.InstanceFrames), desc)
	}
	fmt.Printf("\n%d pattern(s) shown (N = instance frame count)\n", len(entries))
	return 0
}

func patternsView(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate patterns view: pattern ID required")
		return 2
	}
	target := args[0]

	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate patterns view: %v\n", err)
		return 2
	}
	stdFrames, _ := stdlib.Load()

	for _, p := range patterns {
		if p.ID() == target {
			fmt.Printf("Pattern: %s\n", p.ID())
			fmt.Printf("Description: %s\n", p.Frontmatter.Description)
			if len(p.Frontmatter.AnticipatedSiblings) > 0 {
				fmt.Println("Anticipated siblings:")
				for _, s := range p.Frontmatter.AnticipatedSiblings {
					fmt.Printf("  - %s\n", s)
				}
			}
			var insts []string
			for _, f := range stdFrames {
				if f.Frontmatter.Pattern == target {
					insts = append(insts, f.ID())
				}
			}
			sort.Strings(insts)
			if len(insts) > 0 {
				fmt.Println("Instance frames:")
				for _, id := range insts {
					fmt.Printf("  - %s\n", id)
				}
			} else {
				fmt.Println("Instance frames: (none, pattern has no instances yet)")
			}
			if p.Body != "" {
				fmt.Println()
				fmt.Println(p.Body)
			}
			return 0
		}
	}
	fmt.Fprintf(os.Stderr, "nimblegate patterns view: pattern %q not found\n", target)
	return 1
}
