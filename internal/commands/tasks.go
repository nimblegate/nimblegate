// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"nimblegate/internal/paths"
	"nimblegate/internal/tasks"
)

// Tasks renders and mutates the findings-ledger task-list. With no subcommand
// it lists tasks (open by default; --resolved / --deferred / --json). The
// defer / undefer / link / unlink subcommands mutate a task by ID (or an
// unambiguous prefix). The ledger itself is maintained by `nimblegate check`.
func Tasks(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "defer", "undefer", "link", "unlink":
			return tasksMutate(args[0], args[1:])
		}
	}
	return tasksList(args)
}

func loadLedger() (string, *tasks.Ledger, int) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate tasks: getwd: %v\n", err)
		return "", nil, 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate tasks: %v\nHint: run `nimblegate init` here.\n", err)
		return "", nil, 2
	}
	ledger, err := tasks.Load(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate tasks: %v\n", err)
		return "", nil, 2
	}
	return root, ledger, 0
}

func tasksMutate(sub string, rest []string) int {
	root, ledger, code := loadLedger()
	if code != 0 {
		return code
	}
	if len(rest) == 0 {
		fmt.Fprintf(os.Stderr, "nimblegate tasks %s: need a task id (see `nimblegate tasks --json`)\n", sub)
		return 2
	}
	id := rest[0]

	var (
		t   *tasks.Task
		err error
	)
	switch sub {
	case "defer":
		fs := flag.NewFlagSet("defer", flag.ExitOnError)
		reason := fs.String("reason", "", "why this is deferred (known, will fix)")
		until := fs.String("until", "", "resurface after this date (YYYY-MM-DD)")
		_ = fs.Parse(rest[1:])
		var untilT *time.Time
		if *until != "" {
			parsed, perr := time.Parse("2006-01-02", *until)
			if perr != nil {
				fmt.Fprintf(os.Stderr, "nimblegate tasks defer: --until must be YYYY-MM-DD: %v\n", perr)
				return 2
			}
			untilT = &parsed
		}
		t, err = ledger.Defer(id, *reason, untilT, time.Now().UTC())
	case "undefer":
		t, err = ledger.Undefer(id)
	case "link":
		if len(rest) < 2 {
			fmt.Fprintln(os.Stderr, "nimblegate tasks link: usage: nimblegate tasks link <id> <pr-ref>")
			return 2
		}
		t, err = ledger.Link(id, rest[1])
	case "unlink":
		t, err = ledger.Unlink(id)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate tasks %s: %v\n", sub, err)
		return 2
	}
	if err := ledger.Save(root); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate tasks %s: save: %v\n", sub, err)
		return 2
	}
	fmt.Printf("✓ %s %s: %s (%s)\n", sub, t.ID, t.FrameID, t.Status)
	if sub == "defer" && t.Severity == "BLOCK" {
		fmt.Fprintln(os.Stderr, "⚠  this is a BLOCK (dangerous) finding: deferring it lets it pass the push/prod gate. Recorded in the ledger.")
	}
	return 0
}

func tasksList(args []string) int {
	fs := flag.NewFlagSet("tasks", flag.ExitOnError)
	showResolved := fs.Bool("resolved", false, "show recently-resolved tasks")
	showDeferred := fs.Bool("deferred", false, "show deferred (known, will-fix) tasks")
	asJSON := fs.Bool("json", false, "emit the task list as JSON (stable IDs + file:line + pr_ref; for PR/agent workflows)")
	_ = fs.Parse(args)

	_, ledger, code := loadLedger()
	if code != 0 {
		return code
	}

	var list []*tasks.Task
	switch {
	case *showResolved:
		list = ledger.ResolvedTasks()
	case *showDeferred:
		list = ledger.DeferredTasks()
	default:
		list = ledger.OpenTasks()
	}

	if *asJSON {
		out, _ := json.MarshalIndent(list, "", "  ")
		fmt.Println(string(out))
		return 0
	}

	if len(list) == 0 {
		switch {
		case *showResolved:
			fmt.Println("No resolved tasks recorded yet.")
		case *showDeferred:
			fmt.Println("No deferred tasks.")
		default:
			fmt.Println("✓ No open tasks: nothing tracked needs fixing.")
			if d := len(ledger.DeferredTasks()); d > 0 {
				fmt.Printf("  (%d deferred, `nimblegate tasks --deferred`)\n", d)
			}
		}
		return 0
	}

	if *showResolved {
		fmt.Printf("Resolved tasks: %d (most recent first)\n\n", len(list))
		for _, t := range list {
			when := ""
			if t.ResolvedAt != nil {
				when = t.ResolvedAt.Format("2006-01-02")
			}
			fmt.Printf("  ✓ [%s] %s: %s%s  (resolved %s)\n", t.Severity, t.FrameID, loc(t), prSuffix(t), when)
		}
		return 0
	}

	if *showDeferred {
		fmt.Printf("Deferred tasks: %d\n\n", len(list))
		for _, t := range list {
			fmt.Printf("  ⏸ [%s] %s: %s%s\n", t.Severity, t.FrameID, loc(t), prSuffix(t))
			detail := t.DeferReason
			if t.DeferUntil != nil {
				if detail != "" {
					detail += "; "
				}
				detail += "until " + t.DeferUntil.Format("2006-01-02")
			}
			if detail != "" {
				fmt.Printf("      (%s · id %s)\n", detail, t.ID)
			} else {
				fmt.Printf("      (id %s)\n", t.ID)
			}
		}
		return 0
	}

	// Default: open tasks grouped by frame for an at-a-glance list.
	type group struct {
		frame    string
		severity string
		oldest   time.Time
		runs     int
		tasks    []*tasks.Task
	}
	byFrame := map[string]*group{}
	for _, t := range list {
		g := byFrame[t.FrameID]
		if g == nil {
			g = &group{frame: t.FrameID, severity: t.Severity, oldest: t.FirstSeen, runs: t.RunsSeen}
			byFrame[t.FrameID] = g
		}
		if severityRankCmd(t.Severity) > severityRankCmd(g.severity) {
			g.severity = t.Severity
		}
		if t.FirstSeen.Before(g.oldest) {
			g.oldest = t.FirstSeen
		}
		if t.RunsSeen > g.runs {
			g.runs = t.RunsSeen
		}
		g.tasks = append(g.tasks, t)
	}
	groups := make([]*group, 0, len(byFrame))
	for _, g := range byFrame {
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		si, sj := severityRankCmd(groups[i].severity), severityRankCmd(groups[j].severity)
		if si != sj {
			return si > sj
		}
		return groups[i].oldest.Before(groups[j].oldest)
	})

	fmt.Printf("Open tasks: %d across %d frame(s)   (severity → age; fix and re-run `nimblegate check` to clear)\n\n", len(list), len(groups))
	for _, g := range groups {
		fmt.Printf("[%s] %s: %d open · since %s (%s)\n",
			g.severity, g.frame, len(g.tasks), g.oldest.Format("2006-01-02"), runsLabel(g.runs))
		for _, t := range g.tasks {
			fmt.Printf("    %s: %s%s   ·id %s\n", loc(t), t.Label, prSuffix(t), t.ID)
		}
	}
	if d := len(ledger.DeferredTasks()); d > 0 {
		fmt.Printf("\n(%d deferred, `nimblegate tasks --deferred`; defer with `nimblegate tasks defer <id> --reason …`)\n", d)
	}
	return 0
}

func loc(t *tasks.Task) string {
	if t.File == "" {
		return "(project-level)"
	}
	if t.Line > 0 {
		return fmt.Sprintf("%s:%d", t.File, t.Line)
	}
	return t.File
}

func prSuffix(t *tasks.Task) string {
	if t.PRRef == "" {
		return ""
	}
	return " → " + t.PRRef
}

func severityRankCmd(s string) int {
	switch s {
	case "BLOCK":
		return 3
	case "WARN":
		return 2
	case "INFO":
		return 1
	}
	return 0
}

func runsLabel(n int) string {
	if n == 1 {
		return "1 run"
	}
	return fmt.Sprintf("%d runs", n)
}
