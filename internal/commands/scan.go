// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/kits"
	"nimblegate/internal/stdlib"
)

// ScanRecommendation is the machine-readable shape of a scan result. The
// gateway post-receive hook shells out to `nimblegate scan <tmp>
// --recommend-json`, captures stdout, and persists this JSON to
// `<policy-root>/<repo>/scan-recommendation.json`. The dashboard reads that
// file to render the recommendation banner. Stable interface - schema is
// pinned in docs/superpowers/specs/2026-05-30-gateway-add-remove-repo-design.md.
type ScanRecommendation struct {
	ScannedAt          string             `json:"scanned_at"`
	TreeRef            string             `json:"tree_ref"`
	RecommendedGroups  []RecommendedGroup `json:"recommended_groups"`
	RecommendedLinters []string           `json:"recommended_linters,omitempty"`
	Dismissed          bool               `json:"dismissed"`
}

// RecommendedGroup is one frame-group entry inside a ScanRecommendation.
// Always is true for the core kit (catastrophic-prevention) and false
// for stack-specific kits. WouldFlag is the raw count before whitelisting.
type RecommendedGroup struct {
	Name      string `json:"name"`
	Always    bool   `json:"always"`
	WouldFlag int    `json:"would_flag"`
}

// Scan walks an existing project, detects its stack from file-presence
// heuristics, and recommends which frame groups + linters to enable - the
// brownfield-onboarding entry point. It changes nothing; it prints the
// `nimblegate enable …` line and points at `nimblegate check` to preview what
// the recommended frames would flag on the existing code. (Level A: detection
// + recommendation; the per-group "would flag N" preview is a parked v2.)
func Scan(args []string) int {
	fset := flag.NewFlagSet("scan", flag.ExitOnError)
	quick := fset.Bool("quick", false, "detection + recommendation only; skip running the recommended frames for would-flag counts")
	recommendJSON := fset.Bool("recommend-json", false, "print recommendation struct as JSON to stdout (no human output), used by the gateway post-receive hook")
	// Reorder so flags can appear after a positional path (e.g. `scan <tmp>
	// --recommend-json` - the shape the gateway post-receive hook uses).
	_ = fset.Parse(reorderFlagsFirst(args))

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate scan: getwd: %v\n", err)
		return 2
	}
	root := cwd
	if fset.Arg(0) != "" {
		root = fset.Arg(0)
	}

	sig := detectSignals(root)
	groupRecs, linters, notes := recommend(sig)

	if *recommendJSON {
		// Compute would-flag counts so the JSON carries the same per-group
		// signal the human-readable path shows. tree_ref is "HEAD" - the
		// gateway extracts HEAD into <tmp> before shelling out, so the
		// scanned tree IS HEAD from the bare repo's perspective.
		counts, _ := previewCounts(root, groupRecs)
		rec := ScanRecommendation{
			ScannedAt:          time.Now().UTC().Format(time.RFC3339),
			TreeRef:            "HEAD",
			RecommendedGroups:  buildRecommendedGroups(groupRecs, counts),
			RecommendedLinters: linters,
			Dismissed:          false,
		}
		if err := json.NewEncoder(os.Stdout).Encode(rec); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate scan: encode: %v\n", err)
			return 2
		}
		return 0
	}

	fmt.Printf("nimblegate scan: %s\n\n", root)
	detected := describeSignals(sig)
	if len(detected) == 0 {
		fmt.Println("No recognizable stack signals found (no HTML/Svelte/Astro, wrangler.toml, go.mod, migrations, …).")
		fmt.Println("Catastrophic-prevention frames still apply to any project:")
	} else {
		fmt.Println("Detected:")
		for _, d := range detected {
			fmt.Printf("  ✓ %s\n", d)
		}
		fmt.Println()
	}

	if *quick {
		fmt.Println("Recommended kits:")
		for _, k := range groupRecs {
			fmt.Printf("  nimblegate kits apply %s\n", k)
		}
	} else {
		// Level B: run the recommended frames against the existing code and
		// show per-kit would-flag counts, so the adoption decision is informed.
		fmt.Println("Recommended kits (run against your existing code):")
		counts, frameCounts := previewCounts(root, groupRecs)
		for _, g := range groupRecs {
			fmt.Printf("  %-20s %2d frames · would flag %d\n", g, frameCounts[g], counts[g])
		}
		fmt.Println("  (raw counts, before any whitelist; enabling + a whitelist suppresses known-false-positives)")
		fmt.Println()
		for _, k := range groupRecs {
			fmt.Printf("  nimblegate kits apply %s\n", k)
		}
	}
	if len(linters) > 0 {
		fmt.Println("\nRecommended linters (in appframes.toml):")
		for _, l := range linters {
			fmt.Printf("  [linters.%s]\n  enabled = true\n", l)
		}
	}
	for _, n := range notes {
		fmt.Printf("\nNote: %s\n", n)
	}

	fmt.Println("\nNext:")
	fmt.Printf("  1. nimblegate kits apply %s\n", strings.Join(groupRecs, " "))
	fmt.Println("  2. nimblegate check     # see the findings in detail")
	fmt.Println("  3. nimblegate review    # whole-project production-ready verdict")
	return 0
}

// previewCounts runs the frames in each recommended group against the project
// (no project config needed - brownfield) and returns per-group finding counts
// + the number of stdlib frames in each group. Frames fire on their CLI
// trigger; only file-scanning frames contribute (command/git-wrap frames don't
// apply to a static scan).
func previewCounts(root string, recGroups []string) (counts, frameCounts map[string]int) {
	counts = map[string]int{}
	frameCounts = map[string]int{}
	for _, g := range recGroups {
		counts[g] = 0
		frameCounts[g] = 0
	}

	stdlibFrames, err := stdlib.Load()
	if err != nil {
		return counts, frameCounts
	}
	ks, err := kits.LoadStdlib()
	if err != nil {
		return counts, frameCounts
	}
	frameByID := map[string]frames.Frame{}
	for _, f := range stdlibFrames {
		frameByID[f.ID()] = f
	}

	groupSets := map[string]map[string]bool{}
	for _, g := range recGroups {
		kit, ok := ks.Get(g)
		set := map[string]bool{}
		if ok {
			for _, id := range kit.Frames {
				if _, exists := frameByID[id]; exists {
					set[id] = true
				}
			}
		}
		groupSets[g] = set
		frameCounts[g] = len(set)
	}

	reg := engine.NewRegistry()
	checks := BuiltinCheckFuncs()
	added := map[string]bool{}
	for _, set := range groupSets {
		for id := range set {
			if added[id] {
				continue
			}
			if check := checks[id]; check != nil {
				_ = reg.Add(frameByID[id], check)
				added[id] = true
			}
		}
	}

	ctx := engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		WorkingDir:   root,
		ExcludedDirs: scanExcludeList(),
	}
	results := engine.Run(reg, ctx)
	return countByGroup(results, groupSets), frameCounts
}

// countByGroup attributes each run's findings to every recommended group whose
// frame produced it. A fired result counts its Hits (or 1 if hitless);
// PASS/SKIP/ERROR count 0.
func countByGroup(results []engine.CheckResult, groupSets map[string]map[string]bool) map[string]int {
	out := map[string]int{}
	for g := range groupSets {
		out[g] = 0
	}
	for _, r := range results {
		n := findingsIn(r)
		if n == 0 {
			continue
		}
		for g, set := range groupSets {
			if set[r.FrameID] {
				out[g] += n
			}
		}
	}
	return out
}

func findingsIn(r engine.CheckResult) int {
	switch r.Outcome {
	case engine.OutcomeBlock, engine.OutcomeWarn, engine.OutcomeInfo:
		if len(r.Hits) > 0 {
			return len(r.Hits)
		}
		return 1
	}
	return 0
}

func scanExcludeList() []string {
	out := make([]string, 0, len(scanExcludeDirs))
	for d := range scanExcludeDirs {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// scanSignals is the evidence collected by walking the project.
type scanSignals struct {
	WranglerToml     bool
	SvelteConfig     bool
	AstroConfig      bool
	GoMod            bool
	PackageJSON      bool
	HeadersFile      bool
	HTMLCount        int
	SvelteCount      int
	AstroCount       int
	SQLMigrations    int
	MigrationScripts bool
}

var scanExcludeDirs = map[string]bool{
	".git": true, "node_modules": true, "dist": true, "build": true,
	".appframes": true, ".svelte-kit": true, "vendor": true, "_archive": true,
	"tmp": true, ".playwright-cache": true, ".playwright-install": true, ".superpowers": true,
}

func detectSignals(root string) scanSignals {
	var s scanSignals
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && scanExcludeDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		lower := strings.ToLower(name)
		switch {
		case name == "wrangler.toml":
			s.WranglerToml = true
		case name == "go.mod":
			s.GoMod = true
		case name == "package.json":
			s.PackageJSON = true
		case name == "_headers":
			s.HeadersFile = true
		case strings.HasPrefix(name, "svelte.config."):
			s.SvelteConfig = true
		case strings.HasPrefix(name, "astro.config."):
			s.AstroConfig = true
		}
		switch {
		case strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm"):
			s.HTMLCount++
		case strings.HasSuffix(lower, ".svelte"):
			s.SvelteCount++
		case strings.HasSuffix(lower, ".astro"):
			s.AstroCount++
		case strings.HasSuffix(lower, ".sql") && strings.Contains(strings.ToLower(path), "migration"):
			s.SQLMigrations++
		case strings.HasPrefix(lower, "apply-") && strings.Contains(lower, "migrat"):
			s.MigrationScripts = true
		}
		return nil
	})
	return s
}

// recommend maps detected signals to kits + linters. The core kit
// (catastrophic prevention) is recommended for every project; stack-specific
// kits are added per signal. Pure - the testable core.
func recommend(s scanSignals) (kitNames, linters, notes []string) {
	kitNames = []string{"core"}
	if s.HTMLCount > 0 || s.SvelteCount > 0 || s.AstroCount > 0 {
		kitNames = append(kitNames, "web-app")
	}
	// wrangler.toml is used for BOTH Cloudflare Workers AND Cloudflare Pages.
	// The pre-fix heuristic blindly recommended cf-workers-project, which
	// mis-classified static-site CF Pages projects (myapp-shape) as
	// Workers and forced operators to manually correct. The multi-kit
	// comparison validation file (myapp-history-multi-kit-comparison.md
	// → Finding 2 + parked scan-logic fix entry in .appframes/_future.md)
	// surfaced this empirically.
	//
	// Heuristic per fix:
	// - wrangler.toml + (HTML files OR Svelte/Astro present) → Pages
	// - wrangler.toml WITHOUT shipping HTML signal → Workers (backend-only)
	// - SvelteKit/Astro configs without wrangler still imply Pages (these
	//   frameworks commonly deploy to CF Pages by convention)
	hasShippingHTML := s.HTMLCount > 0 || s.SvelteCount > 0 || s.AstroCount > 0
	switch {
	case s.WranglerToml && hasShippingHTML:
		kitNames = append(kitNames, "cf-pages-project")
	case s.WranglerToml && !hasShippingHTML:
		kitNames = append(kitNames, "cf-workers-project")
	case s.SvelteConfig || s.AstroConfig:
		kitNames = append(kitNames, "cf-pages-project")
	}
	if s.SQLMigrations > 0 || s.MigrationScripts {
		notes = append(notes, "database migrations detected: enable the Database > Migrations category in the dashboard")
	}
	if s.GoMod {
		linters = append(linters, "go-vet")
	}
	notes = append(notes, "also consider: nimblegate kits apply security-strict for stricter security gating")
	return kitNames, linters, notes
}

// buildRecommendedGroups converts the recommend() kit slice + per-kit
// would-flag counts into the JSON-bound RecommendedGroup slice. The core kit
// is the catastrophic-prevention always-on kit; everything else is stack-specific.
func buildRecommendedGroups(groupRecs []string, counts map[string]int) []RecommendedGroup {
	out := make([]RecommendedGroup, 0, len(groupRecs))
	for _, g := range groupRecs {
		out = append(out, RecommendedGroup{
			Name:      g,
			Always:    g == "core",
			WouldFlag: counts[g],
		})
	}
	return out
}

// reorderFlagsFirst moves any --flag / -flag entries before positional
// arguments so the stdlib `flag` package (which stops at the first
// non-flag token) parses them. Lets `scan <path> --recommend-json` work
// the same as `scan --recommend-json <path>`.
func reorderFlagsFirst(args []string) []string {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for _, a := range args {
		if strings.HasPrefix(a, "-") && a != "-" && a != "--" {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}
	return append(flags, positional...)
}

func describeSignals(s scanSignals) []string {
	var out []string
	if s.WranglerToml {
		out = append(out, "wrangler.toml (Cloudflare)")
	}
	if s.SvelteConfig {
		out = append(out, "svelte.config.* (SvelteKit)")
	}
	if s.AstroConfig {
		out = append(out, "astro.config.* (Astro)")
	}
	if s.GoMod {
		out = append(out, "go.mod (Go module)")
	}
	if s.PackageJSON {
		out = append(out, "package.json (Node/JS)")
	}
	if s.HeadersFile {
		out = append(out, "_headers (CF Pages headers)")
	}
	if s.HTMLCount > 0 {
		out = append(out, fmt.Sprintf("%d HTML file(s)", s.HTMLCount))
	}
	if s.SvelteCount > 0 {
		out = append(out, fmt.Sprintf("%d .svelte file(s)", s.SvelteCount))
	}
	if s.AstroCount > 0 {
		out = append(out, fmt.Sprintf("%d .astro file(s)", s.AstroCount))
	}
	if s.SQLMigrations > 0 {
		out = append(out, fmt.Sprintf("%d *.sql under migrations/", s.SQLMigrations))
	}
	if s.MigrationScripts {
		out = append(out, "apply-*-migration* script(s)")
	}
	return out
}
