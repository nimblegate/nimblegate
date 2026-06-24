// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/linters"
	"nimblegate/internal/paths"
	"nimblegate/internal/scanignore"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/whitelist"
)

// frameStatus is what `nimblegate lint` reports for each loaded frame.
type frameStatus struct {
	id         string
	severity   string // post-override
	enabled    bool   // matches an entry in cfg.Frames.Enabled (or list empty = all enabled)
	overridden bool   // project frame replaces a stdlib frame of the same ID
	hasCheckFn bool   // a Go check function is bound for this id (= will run, not ERROR)
	downgraded bool   // override reduces severity vs stdlib
}

// Lint validates every frame (stdlib + project), reports parse problems,
// and shows enabled/disabled status alongside catalog mismatches so the
// user can audit exactly what their project enforces. Exits 1 if any
// frame is broken, 0 if all clean.
//
// Unlike `check`, lint does NOT run any check functions - it only loads,
// parses, and reports.
func Lint(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate lint: cannot get cwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate lint: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}
	return lintAt(root, os.Stderr)
}

// lintAt runs the lint report for the given project root, writing any
// errors and warnings to stderr. Extracted so tests can capture output
// without a subprocess.
func lintAt(root string, stderr io.Writer) int {
	cfg, _ := config.LoadProject(paths.ConfigPath(root))

	// Reject removed config syntax: @-prefixes and wildcards.
	for _, e := range cfg.Frames.Enabled {
		if strings.HasPrefix(e, "@") {
			fmt.Fprint(stderr, removedGroupSyntaxMsg())
			return 1
		}
		if strings.Contains(e, "*") {
			fmt.Fprint(stderr, removedGroupSyntaxMsg())
			return 1
		}
	}
	// enabled is the flat source of truth; no expansion.
	enabled := cfg.Frames.Enabled

	stdlibFrames, stdlibErr := stdlib.Load()
	if stdlibErr != nil {
		fmt.Fprintf(stderr, "nimblegate lint: stdlib load error: %v\n", stdlibErr)
		// Don't return - keep going to lint project frames.
	}

	projectFrames, projectErrs := frames.LoadFromDir(paths.AppframesDir(root))

	idSources := map[string][]string{}
	stdlibSeverity := map[string]string{}
	projectSeverity := map[string]string{}
	for _, f := range stdlibFrames {
		idSources[f.ID()] = append(idSources[f.ID()], "stdlib:"+f.SourcePath)
		stdlibSeverity[f.ID()] = string(f.Frontmatter.Severity)
	}
	for _, f := range projectFrames {
		idSources[f.ID()] = append(idSources[f.ID()], "project:"+f.SourcePath)
		projectSeverity[f.ID()] = string(f.Frontmatter.Severity)
	}

	// Effective severity = project's if present, else stdlib's. Then apply
	// any [frames.<id>] table override from appframes.toml.
	effectiveSeverity := func(id string) string {
		s := stdlibSeverity[id]
		if proj, ok := projectSeverity[id]; ok {
			s = proj
		}
		if ov, ok := cfg.FrameOverrides[id]; ok && ov.Severity != "" {
			s = ov.Severity
		}
		return s
	}

	bound := BuiltinCheckFuncs()

	// Build the per-frame status table over the deduped ID set.
	statuses := make([]frameStatus, 0, len(idSources))
	for id := range idSources {
		_, isOverride := projectSeverity[id]
		// Mark "overridden" only when BOTH stdlib + project have this ID.
		_, hasStdlib := stdlibSeverity[id]
		st := frameStatus{
			id:         id,
			severity:   effectiveSeverity(id),
			enabled:    isFrameEnabledByPatterns(id, enabled),
			overridden: isOverride && hasStdlib,
			hasCheckFn: bound[id] != nil,
		}
		if st.overridden && severityRank(projectSeverity[id]) < severityRank(stdlibSeverity[id]) {
			st.downgraded = true
		}
		statuses = append(statuses, st)
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].id < statuses[j].id })

	// Detect enabled-list entries that match no loaded frame ID (typos / deleted frames).
	var mismatches []string
	loadedIDs := make(map[string]bool, len(idSources))
	for id := range idSources {
		loadedIDs[id] = true
	}
	for _, pat := range enabled {
		if !loadedIDs[pat] {
			mismatches = append(mismatches, pat)
		}
	}

	// ---------------- Output ----------------

	fmt.Printf("Lint report for %s\n\n", root)
	fmt.Printf("stdlib frames loaded: %d\n", len(stdlibFrames))
	fmt.Printf("project frames loaded: %d\n", len(projectFrames))
	fmt.Println()

	// Parse problems first - block of warnings before the table.
	if len(projectErrs) > 0 || stdlibErr != nil {
		fmt.Printf("Problems (%d):\n", len(projectErrs)+errCount(stdlibErr))
		if stdlibErr != nil {
			fmt.Printf("  - stdlib: %v\n", stdlibErr)
		}
		for _, e := range projectErrs {
			fmt.Printf("  - %v\n", e)
		}
		fmt.Println()
	} else {
		fmt.Println("✓ All frames parsed cleanly.")
		fmt.Println()
	}

	// Per-frame status table. Skip if no frames loaded (rare; new project).
	if len(statuses) > 0 {
		fmt.Println("Frame status:")
		fmt.Println("  ✓ = enabled (will run)   ⊘ = loaded but disabled   💥 = enabled but no check bound")
		for _, st := range statuses {
			icon := "⊘"
			if st.enabled {
				icon = "✓"
				if !st.hasCheckFn {
					icon = "💥"
				}
			}
			notes := ""
			if st.overridden {
				notes = "  ← project override"
				if st.downgraded {
					notes += " ⚠ severity downgraded"
				}
			}
			fmt.Printf("  %s %-44s %-6s%s\n", icon, st.id, st.severity, notes)
		}
		fmt.Println()
	}

	// Mismatches - enabled-list typos / dead entries.
	if len(mismatches) > 0 {
		fmt.Printf("⚠️  appframes.toml enables %d frame(s) that don't exist:\n", len(mismatches))
		for _, m := range mismatches {
			fmt.Printf("   - %q\n", m)
		}
		fmt.Println("   (typo? frame deleted? remove from [frames].enabled or fix the spelling)")
		fmt.Println()
	}
	// Whitelist surfacing - load with the same fail-closed semantics
	// `nimblegate check` uses, so a typo here is caught at lint time. The
	// "unused" count isn't shown because lint never runs check funcs;
	// users wanting that signal use `nimblegate whitelist list --unused`
	// after a real `nimblegate check` run.
	known := loadedIDs
	for _, id := range linters.EnabledIDs(cfg.Linters) {
		known[id] = true // enabled linters' synthetic IDs are valid whitelist targets
	}
	wl, wlErr := whitelist.LoadFromProject(root, known, time.Now().UTC())
	if wlErr != nil {
		fmt.Printf("⚠️  Whitelist failed to load: %v\n", wlErr)
		fmt.Println("   (fail-closed: gates fire normally until the whitelist parses cleanly)")
		fmt.Println()
		// Don't fail lint - keep going so the rest of the report is useful.
	} else if wl != nil && wl.Count() > 0 {
		entries := wl.Entries()
		expired := 0
		expiredEntries := []whitelist.EntryView{}
		for _, e := range entries {
			if e.Expired {
				expired++
				expiredEntries = append(expiredEntries, e)
			}
		}
		fmt.Printf("Whitelist (%s):\n", wl.Source())
		fmt.Printf("  %d total: %d active, %d expired\n", wl.Count(), wl.Count()-expired, expired)
		for _, e := range expiredEntries {
			fmt.Printf("  ⚠ expired: frame=%s path=%s (expires=%s): %s\n",
				e.Frame, e.Path, e.Expires, e.Reason)
		}
		fmt.Println("  (run `nimblegate whitelist list --unused` after `nimblegate check` for usage hygiene)")
		fmt.Println()
	}

	// Scan-ignore surfacing: surface any malformed exclude-paths globs +
	// .appframes-ignore patterns as warnings so the user can fix typos.
	excludeNames := cfg.Scan.Exclude
	if len(excludeNames) == 0 {
		excludeNames = []string{".git", "node_modules", "dist", "build", ".appframes"}
	}
	matcher, _ := scanignore.New(root, excludeNames, cfg.Scan.ExcludePaths)
	if matcher != nil {
		warnings := matcher.LoadWarnings()
		if len(warnings) > 0 {
			fmt.Printf("⚠️  Scan-ignore warnings (%d):\n", len(warnings))
			for _, w := range warnings {
				fmt.Printf("   - %s\n", w)
			}
			fmt.Println("   (fix or remove the malformed patterns; other patterns still apply)")
			fmt.Println()
		}
	}

	// Project-override expansion (existing detail; richer than the table).
	var overrides []string
	for id, sources := range idSources {
		if len(sources) > 1 {
			overrides = append(overrides, id)
		}
	}
	if len(overrides) > 0 {
		sort.Strings(overrides)
		fmt.Println("Project overrides of stdlib frames:")
		for _, id := range overrides {
			downgradeMark := ""
			if severityRank(projectSeverity[id]) < severityRank(stdlibSeverity[id]) {
				downgradeMark = "  ⚠ SEVERITY DOWNGRADED"
			}
			fmt.Printf("  - %s%s\n", id, downgradeMark)
			if stdlibSeverity[id] != "" || projectSeverity[id] != "" {
				fmt.Printf("      stdlib severity: %s → project severity: %s\n",
					stdlibSeverity[id], projectSeverity[id])
			}
			for _, src := range idSources[id] {
				fmt.Printf("      ← %s\n", src)
			}
		}
		fmt.Println()
	}

	// Severity-downgrade summary (existing).
	var downgrades []string
	for id := range projectSeverity {
		s, hasStdlib := stdlibSeverity[id]
		if !hasStdlib {
			continue
		}
		if severityRank(projectSeverity[id]) < severityRank(s) {
			downgrades = append(downgrades, id)
		}
	}
	if len(downgrades) > 0 {
		fmt.Printf("⚠️  %d frame(s) weaken stdlib severity. Review intentional? %v\n\n",
			len(downgrades), downgrades)
	}

	if len(projectErrs) > 0 || stdlibErr != nil {
		return 1
	}
	if len(mismatches) > 0 {
		// Mismatches are a real config bug - exit non-zero so CI catches them.
		return 1
	}
	return 0
}

// isFrameEnabledByPatterns mirrors engine.isFrameEnabled (duplicated here
// to avoid the commands package importing the engine package just for
// this helper, and to keep the wildcard semantics in one obvious place
// for lint's purposes).
func isFrameEnabledByPatterns(id string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pat := range patterns {
		if pat == id {
			return true
		}
		if strings.HasSuffix(pat, "/*") {
			prefix := strings.TrimSuffix(pat, "*")
			if strings.HasPrefix(id, prefix) {
				return true
			}
		}
	}
	return false
}

// severityRank maps a severity name to a numeric weight. Higher = stricter.
// Used to detect downgrades (project < stdlib).
func severityRank(s string) int {
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

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func errCount(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

// removedGroupSyntaxMsg returns the migration-table error shown when
// appframes.toml still contains pre-v0.1.0 @-prefix or wildcard syntax.
func removedGroupSyntaxMsg() string {
	return `Error: appframes.toml uses removed syntax.

The v0.1.0 frame model is flat frame IDs only. Apply a kit instead:

  @tier-1          → nimblegate kits apply core
  @tier-6          → tick Documentation category frames in dashboard
  @web             → nimblegate kits apply web-app
  @cloudflare      → nimblegate kits apply cf-workers-project
  @cf-pages        → nimblegate kits apply cf-pages-project
  @migrations      → tick Database > Migrations frames in dashboard
  @security-strict → nimblegate kits apply security-strict
  security/*       → tick frames individually in dashboard

See docs/frames.md for the full v0.1.0 model. After migration, re-run.
`
}
