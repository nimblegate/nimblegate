// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"time"

	"nimblegate/internal/paths"
	"nimblegate/internal/tasks"
)

// Slice manages review slices: declare a unit of work, then at "done" review
// what that slice introduced (via the ledger's FirstSeen). It's a checkpoint -
// it reports + exits non-zero on slice-introduced dangerous findings - not a
// hard gate (the prod-boundary gate on `git push` does the real blocking).
//
// v1 reads the ledger (reflecting the last `nimblegate check`); it does not run
// its own check. Run `nimblegate check` during the slice as usual.
func Slice(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate slice: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate slice: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate slice <start <name> | status | done>")
		return 2
	}
	switch args[0] {
	case "start":
		return sliceStart(root, args[1:])
	case "status":
		return sliceStatus(root)
	case "done":
		return sliceDone(root)
	case "summary":
		return sliceSummary(root)
	default:
		fmt.Fprintf(os.Stderr, "nimblegate slice: unknown subcommand %q (start | status | done | summary)\n", args[0])
		return 2
	}
}

func sliceStart(root string, rest []string) int {
	if len(rest) == 0 || rest[0] == "" {
		fmt.Fprintln(os.Stderr, "usage: nimblegate slice start <name>")
		return 2
	}
	cur, _ := tasks.LoadSlice(root)
	if cur.Active() {
		fmt.Fprintf(os.Stderr, "nimblegate slice: slice %q is already active: run `nimblegate slice done` first\n", cur.Name)
		return 2
	}
	st := &tasks.SliceState{Name: rest[0], StartedAt: time.Now().UTC()}
	if err := st.SaveSlice(root); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate slice: %v\n", err)
		return 2
	}
	fmt.Printf("✓ slice started: %s\n", st.Name)
	return 0
}

func sliceStatus(root string) int {
	st, _ := tasks.LoadSlice(root)
	if !st.Active() {
		fmt.Println("No active slice. Start one with `nimblegate slice start <name>`.")
		return 0
	}
	ledger, _ := tasks.Load(root)
	introduced := ledger.OpenSince(st.StartedAt)
	fmt.Printf("Slice: %s   (started %s, %s ago)\n", st.Name, st.StartedAt.Format("2006-01-02 15:04"), shortDur(time.Since(st.StartedAt)))
	fmt.Printf("  %d open task(s) introduced so far (reflects last `nimblegate check`).\n", len(introduced))
	return 0
}

func sliceDone(root string) int {
	st, _ := tasks.LoadSlice(root)
	if !st.Active() {
		fmt.Fprintln(os.Stderr, "nimblegate slice: no active slice to close (`nimblegate slice start <name>` first)")
		return 2
	}
	ledger, _ := tasks.Load(root)
	introduced := ledger.OpenSince(st.StartedAt)
	var dangerous, advisory []*tasks.Task
	for _, t := range introduced {
		if t.Severity == "BLOCK" {
			dangerous = append(dangerous, t)
		} else {
			advisory = append(advisory, t)
		}
	}

	fmt.Printf("Slice done: %s   (started %s)\n", st.Name, st.StartedAt.Format("2006-01-02 15:04"))
	fmt.Printf("This slice introduced %d open finding(s): review/fix before moving on:\n", len(introduced))
	if len(dangerous) > 0 {
		fmt.Printf("  DANGEROUS (BLOCK): %d\n", len(dangerous))
		for _, t := range dangerous {
			fmt.Printf("     ❌ %s: %s\n", t.FrameID, sliceLoc(t))
		}
	}
	if len(advisory) > 0 {
		fmt.Printf("  Advisory (WARN/INFO): %d\n", len(advisory))
		for _, t := range advisory {
			fmt.Printf("     ⚠  %s: %s\n", t.FrameID, sliceLoc(t))
		}
	}
	fmt.Println("  (reflects the last `nimblegate check`, run it first for current state)")

	// Record the completed slice so `nimblegate slice summary` can surface
	// per-slice finding density + anomalies. Best-effort.
	hist, _ := tasks.LoadHistory(root)
	hist.Append(tasks.CompletedSlice{
		Name:      st.Name,
		StartedAt: st.StartedAt,
		EndedAt:   time.Now().UTC(),
		Total:     len(introduced),
		Dangerous: len(dangerous),
		Advisory:  len(advisory),
	})
	_ = hist.Save(root)
	_ = tasks.ClearSlice(root)

	fmt.Println()
	if len(dangerous) > 0 {
		fmt.Printf("⛔ this slice introduced %d dangerous finding(s): examine before continuing.\n", len(dangerous))
		return 1
	}
	fmt.Printf("✓ slice closed: no dangerous findings introduced (%d advisory).\n", len(advisory))
	return 0
}

func sliceSummary(root string) int {
	hist, _ := tasks.LoadHistory(root)
	if len(hist.Slices) == 0 {
		fmt.Println("No completed slices yet. Close one with `nimblegate slice done`.")
		return 0
	}
	mean := hist.Mean()
	flags := hist.Anomalies()
	fmt.Printf("nimblegate slice summary: %d completed slice(s)   (avg %.1f findings/slice)\n\n", len(hist.Slices), mean)
	fmt.Printf("  %-22s %9s %10s %9s\n", "slice", "findings", "dangerous", "advisory")
	for i, s := range hist.Slices {
		note := ""
		if flags[i] && mean > 0 {
			note = fmt.Sprintf("   ⚠ %.1f× avg: examine more carefully", float64(s.Total)/mean)
		}
		fmt.Printf("  %-22s %9d %10d %9d%s\n", s.Name, s.Total, s.Dangerous, s.Advisory, note)
	}
	if len(hist.Slices) < 3 {
		fmt.Printf("\n  (need ≥3 completed slices to flag anomalies; have %d)\n", len(hist.Slices))
	}
	return 0
}

func sliceLoc(t *tasks.Task) string {
	if t.File == "" {
		return "(project-level)"
	}
	if t.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", t.File, t.Line, t.Label)
	}
	return t.File + ": " + t.Label
}

func shortDur(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
