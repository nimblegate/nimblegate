// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/whitelist"
)

// whitelistEntryOut is the JSON shape for one whitelist entry. Status
// is computed at output time: "expired" > "unused" > "stale" > "active-matched".
// MatchedTotal is the cumulative count across all runs (Phase 1 Slice 9);
// MatchedCount is the per-run figure kept for backward compat.
type whitelistEntryOut struct {
	Frame        string `json:"frame"`
	Path         string `json:"path"`
	Pattern      string `json:"pattern,omitempty"`
	Reason       string `json:"reason"`
	Expires      string `json:"expires,omitempty"`
	Status       string `json:"status"`                 // "active-matched" | "expired" | "unused" | "stale"
	MatchedCount int    `json:"matched_count"`          // current-run; legacy
	MatchedTotal int    `json:"matched_total"`          // cumulative across runs
	LastMatched  string `json:"last_matched,omitempty"` // ISO-8601 or ""
}

// whitelistListOutput is the top-level JSON shape for `nimblegate
// whitelist list`. Source is the path the file was loaded from (or
// empty if no whitelist exists yet).
type whitelistListOutput struct {
	Source   string              `json:"source"`
	Total    int                 `json:"total"`
	Active   int                 `json:"active"`
	Expired  int                 `json:"expired"`
	Unused   int                 `json:"unused"`
	Entries  []whitelistEntryOut `json:"entries"`
	HasError bool                `json:"has_error,omitempty"`
	Error    string              `json:"error,omitempty"`
}

// Whitelist routes the `nimblegate whitelist <subcommand>` family.
// Currently supports `list`. Future subcommands (add/remove) will be
// added here; doing it via dispatch (vs separate top-level commands)
// keeps the user-facing surface tidy: one verb for one resource.
func Whitelist(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate whitelist: subcommand required")
		fmt.Fprintln(os.Stderr, "usage: nimblegate whitelist list [--expired] [--unused] [--json]")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list":
		return whitelistList(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate whitelist: manage the project allow-list with hygiene tracking")
		fmt.Println()
		fmt.Println("Usage: nimblegate whitelist <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  list [--expired] [--unused] [--json]   Show whitelist entries + cumulative match counts +")
		fmt.Println("                                         last-matched age + stale status")
		fmt.Println()
		fmt.Println("Whitelist file:  .appframes/_canonical/whitelist.toml")
		fmt.Println("Hygiene stats:   .appframes/_canonical/whitelist-stats.json (persists across runs)")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate whitelist: unknown subcommand %q (use list; --help for usage)\n", sub)
		return 2
	}
}

func whitelistList(args []string) int {
	fs := flag.NewFlagSet("whitelist list", flag.ExitOnError)
	expiredOnly := fs.Bool("expired", false, "show only expired entries")
	unusedOnly := fs.Bool("unused", false, "show only entries that haven't matched yet this run")
	asJSON := fs.Bool("json", false, "emit JSON for scripting / future UI")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate whitelist: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate whitelist: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	// Need known frame IDs so the loader can validate `frame:` entries
	// the same way `nimblegate check` does - consistent error semantics.
	stdlibFrames, _ := stdlib.Load()
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	e, err := engine.New(engine.Options{
		ProjectRoot:   root,
		StdlibFrames:  stdlibFrames,
		ProjectFrames: projectFrames,
		CheckFuncs:    BuiltinCheckFuncs(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate whitelist: engine init: %v\n", err)
		return 2
	}
	defer e.Close()
	known := knownIDsWithLinters(e)

	now := time.Now().UTC()
	wl, loadErr := whitelist.LoadFromProject(root, known, now)

	// Phase 1 Slice 9: load persisted hygiene counters (best-effort).
	// A missing or corrupted stats file is fine - fall back to per-run
	// counters only.
	stats, _ := whitelist.LoadStats(root)

	// Compose output even on load error so JSON consumers get a
	// machine-parseable error shape, not just a stderr line.
	out := whitelistListOutput{}
	if wl != nil {
		out.Source = wl.Source()
	}
	if loadErr != nil {
		out.HasError = true
		out.Error = loadErr.Error()
	} else if wl != nil {
		entries := wl.Entries()
		for _, ev := range entries {
			var matchedTotal int
			var lastMatchedISO string
			isStale := false
			if es := stats.Lookup(ev.Frame, ev.Path, ev.Pattern); es != nil {
				matchedTotal = es.MatchedTotal
				if !es.LastMatched.IsZero() {
					lastMatchedISO = es.LastMatched.Format(time.RFC3339)
				}
				isStale = es.IsStale(now, whitelist.DefaultStaleDays)
			}

			status := "active-matched"
			switch {
			case ev.Expired:
				status = "expired"
				out.Expired++
			case matchedTotal == 0:
				status = "unused"
				out.Unused++
				out.Active++
			case isStale:
				status = "stale"
				out.Active++
			default:
				out.Active++
			}
			if *expiredOnly && status != "expired" {
				continue
			}
			if *unusedOnly && status != "unused" {
				continue
			}
			out.Entries = append(out.Entries, whitelistEntryOut{
				Frame:        ev.Frame,
				Path:         ev.Path,
				Pattern:      ev.Pattern,
				Reason:       ev.Reason,
				Expires:      ev.Expires,
				Status:       status,
				MatchedCount: ev.MatchedCount,
				MatchedTotal: matchedTotal,
				LastMatched:  lastMatchedISO,
			})
		}
		out.Total = len(entries)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate whitelist: json encode: %v\n", err)
			return 2
		}
		if out.HasError {
			return 2
		}
		return 0
	}

	// Human-readable.
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "nimblegate whitelist: %v\n", loadErr)
		return 2
	}
	if wl == nil || out.Total == 0 {
		fmt.Println("No whitelist entries.")
		fmt.Printf("(checked %s)\n", root+"/.appframes/_canonical/whitelist.toml")
		return 0
	}

	fmt.Printf("Whitelist: %s\n", out.Source)
	fmt.Printf("  %d total: %d active, %d expired, %d never-matched\n",
		out.Total, out.Active, out.Expired, out.Unused)
	fmt.Println("  (Matched column shows cumulative counts across all `nimblegate check` runs;")
	fmt.Println("   stale = last match older than 90 days, may be removable.)")
	fmt.Println()
	fmt.Printf("%-14s  %-8s  %-11s  %-44s  %s\n", "Status", "Matched", "Last", "Frame", "Path / Reason")
	for _, e := range out.Entries {
		lastDisplay := "-"
		if e.LastMatched != "" {
			if t, err := time.Parse(time.RFC3339, e.LastMatched); err == nil {
				lastDisplay = relativeTime(t, time.Now().UTC())
			}
		}
		fmt.Printf("%-14s  %-8d  %-11s  %-44s  %s\n", e.Status, e.MatchedTotal, lastDisplay, e.Frame, e.Path)
		fmt.Printf("%-14s  %-8s  %-11s  %-44s    └─ %s\n", "", "", "", "", e.Reason)
		if e.Expires != "" {
			fmt.Printf("%-14s  %-8s  %-11s  %-44s    └─ expires: %s\n", "", "", "", "", e.Expires)
		}
	}
	return 0
}

// relativeTime renders a past time as a short human label
// ("3d ago", "2h ago", "just now"). Used by the whitelist list display
// for the Last-Matched column.
func relativeTime(then, now time.Time) string {
	if then.IsZero() {
		return "-"
	}
	d := now.Sub(then)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
