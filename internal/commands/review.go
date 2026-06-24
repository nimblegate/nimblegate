// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"sort"
	"time"

	"nimblegate/internal/banner"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/tasks"
	"nimblegate/internal/whitelist"
)

// Review is the final full-project review: a fresh whole-tree check rendered as
// one consolidated report - dangerous (open BLOCK), advisory (WARN/INFO),
// deferred, and resolved - with a production-ready verdict. Exits 1 when any
// open dangerous finding remains (CI-usable), 0 otherwise. It reads the ledger
// for defer/resolved status but does not mutate it (that's `nimblegate check`).
func Review(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate review: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate review: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	stdlibFrames, _ := stdlib.Load()
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	e, err := engine.New(engine.Options{
		ProjectRoot:   root,
		StdlibFrames:  stdlibFrames,
		ProjectFrames: projectFrames,
		CheckFuncs:    BuiltinCheckFuncs(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate review: engine init: %v\n", err)
		return 2
	}
	defer e.Close()

	ctx := engine.CheckContext{
		Trigger:       engine.TriggerCLI,
		ProjectRoot:   root,
		WorkingDir:    cwd,
		ExcludedDirs:  e.ExcludedDirs(),
		IgnorePath:    e.IgnorePathFunc(),
		CurrentBranch: currentGitBranch(root),
	}
	results := engine.Run(e.Registry, ctx)
	wl, err := whitelist.LoadFromProject(root, knownIDsWithLinters(e), time.Now().UTC())
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate review: %v\n", err)
		return 2
	}
	filtered, _ := engine.ApplyWhitelist(results, wl, root)

	ledger, _ := tasks.Load(root)
	dangerous, advisory, deferred := reviewBuckets(tasks.KeysFromResults(filtered, root), ledger)
	resolved := ledger.ResolvedTasks()

	name := banner.DefaultProjectName(root)
	fmt.Printf("nimblegate review: %s (%s)\n\n", name, time.Now().UTC().Format("2006-01-02"))

	fmt.Printf("DANGEROUS (open BLOCK): must fix before shipping: %d\n", len(dangerous))
	for _, f := range dangerous {
		fmt.Printf("   ❌ %s: %s: %s\n", f.Key.FrameID, locFinding(f), f.Key.Label)
	}
	fmt.Printf("\nAdvisory (WARN/INFO): %d\n", len(advisory))
	for _, line := range byFrameLines(advisory) {
		fmt.Printf("   %s\n", line)
	}
	fmt.Printf("\nDeferred (knowingly carried): %d", len(deferred))
	if len(deferred) > 0 {
		fmt.Print("   (`nimblegate tasks --deferred`)")
	}
	fmt.Printf("\nResolved (tracked, now fixed): %d\n", len(resolved))

	fmt.Println()
	if len(dangerous) > 0 {
		fmt.Printf("Verdict: ⛔ NOT production-ready: %d dangerous finding(s) open. Fix, defer (with reason), or they block `git push`.\n", len(dangerous))
		return 1
	}
	if len(advisory) > 0 {
		fmt.Printf("Verdict: ✓ production-ready: no dangerous findings. %d advisory item(s) remain (see `nimblegate tasks`).\n", len(advisory))
		return 0
	}
	fmt.Println("Verdict: ✓ production-ready: clean. No open findings.")
	return 0
}

// reviewBuckets splits fresh findings into dangerous (open BLOCK), advisory
// (open WARN/INFO), and deferred (any severity whose ledger task is deferred).
func reviewBuckets(findings []tasks.Finding, ledger *tasks.Ledger) (dangerous, advisory, deferred []tasks.Finding) {
	for _, f := range findings {
		if t := ledger.Tasks[f.Key.ID()]; t != nil && t.Status == tasks.StatusDeferred {
			deferred = append(deferred, f)
			continue
		}
		if f.Severity == "BLOCK" {
			dangerous = append(dangerous, f)
		} else {
			advisory = append(advisory, f)
		}
	}
	return dangerous, advisory, deferred
}

func locFinding(f tasks.Finding) string {
	if f.Key.File == "" {
		return "(project-level)"
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.Key.File, f.Line)
	}
	return f.Key.File
}

func byFrameCount(findings []tasks.Finding) map[string]int {
	m := map[string]int{}
	for _, f := range findings {
		m[f.Key.FrameID]++
	}
	return m
}

func byFrameLines(findings []tasks.Finding) []string {
	counts := byFrameCount(findings)
	frameIDs := make([]string, 0, len(counts))
	for id := range counts {
		frameIDs = append(frameIDs, id)
	}
	sort.SliceStable(frameIDs, func(i, j int) bool { return counts[frameIDs[i]] > counts[frameIDs[j]] })
	out := make([]string, 0, len(frameIDs))
	for _, id := range frameIDs {
		out = append(out, fmt.Sprintf("%s: %d", id, counts[id]))
	}
	return out
}
