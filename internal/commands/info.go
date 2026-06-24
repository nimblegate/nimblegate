// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// infoOutput is the JSON shape for `nimblegate info <id>`. The same data
// renders as a human-readable detail block in the default mode.
type infoOutput struct {
	ID          string          `json:"id"`
	Frontmatter infoFrontmatter `json:"frontmatter"`
	Body        string          `json:"body"`
	SourcePath  string          `json:"source_path"`
	Source      string          `json:"source"` // "stdlib" or "project"
	Enabled     bool            `json:"enabled"`
	HasCheck    bool            `json:"has_check"`
}

// infoFrontmatter mirrors frames.Frontmatter but with the field
// ordering / serialization tailored for JSON output. Keeping it
// distinct insulates JSON consumers from internal struct churn.
type infoFrontmatter struct {
	Name              string   `json:"name"`
	Category          string   `json:"category"`
	Severity          string   `json:"severity"`
	Tier              int      `json:"tier"`
	Triggers          []string `json:"triggers"`
	Tags              []string `json:"tags,omitempty"`
	DedupKey          string   `json:"dedup_key,omitempty"`
	RunsAfter         []string `json:"runs_after,omitempty"`
	AppliesToFiles    []string `json:"applies_to_files,omitempty"`
	AppliesToCommands []string `json:"applies_to_commands,omitempty"`
	CanonicalRefs     []string `json:"canonical_refs,omitempty"`
	Pattern           string   `json:"pattern,omitempty"`
	Lifecycle         string   `json:"lifecycle,omitempty"`
	SelectionGrade    string   `json:"selection_grade,omitempty"`
	ArchivedAt        string   `json:"archived_at,omitempty"`
	ArchiveReason     string   `json:"archive_reason,omitempty"`
}

// Info implements `nimblegate info <id>`. Prints the full frame
// definition: frontmatter (effective values, including overrides) +
// body, plus current enabled status.
func Info(args []string) int {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON for scripting / future UI")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "nimblegate info: exactly one frame ID required")
		fmt.Fprintln(os.Stderr, "usage: nimblegate info <category/name>")
		return 2
	}
	wantID := fs.Arg(0)

	stdlibFrames, err := stdlib.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate info: stdlib load: %v\n", err)
		return 2
	}

	var found frames.Frame
	var source string
	for _, f := range stdlibFrames {
		if f.ID() == wantID {
			found = f
			source = "stdlib"
			break
		}
	}

	cwd, _ := os.Getwd()
	root, projectErr := paths.FindProjectRoot(cwd)
	var cfg config.ProjectConfig
	expanded := []string{}
	if projectErr == nil {
		projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
		// Project frame shadows stdlib at the same ID.
		for _, f := range projectFrames {
			if f.ID() == wantID {
				found = f
				source = "project"
				break
			}
		}
		cfg, _ = config.LoadProject(paths.ConfigPath(root))
		expanded = cfg.Frames.Enabled
	}

	if source == "" {
		fmt.Fprintf(os.Stderr, "nimblegate info: no frame %q (try `nimblegate list`)\n", wantID)
		return 1
	}

	// Apply severity override from [frames.<id>] for the displayed value.
	effectiveSeverity := string(found.Frontmatter.Severity)
	if ov, ok := cfg.FrameOverrides[wantID]; ok && ov.Severity != "" {
		effectiveSeverity = ov.Severity
	}

	bound := BuiltinCheckFuncs()
	enabled := frameEnabledInList(wantID, expanded, projectErr)
	hasCheck := bound[wantID] != nil

	if *asJSON {
		out := infoOutput{
			ID: wantID,
			Frontmatter: infoFrontmatter{
				Name:              found.Frontmatter.Name,
				Category:          string(found.Frontmatter.Category),
				Severity:          effectiveSeverity,
				Tier:              found.Frontmatter.EffectiveTier(),
				Triggers:          found.Frontmatter.Triggers,
				Tags:              found.Frontmatter.Tags,
				DedupKey:          found.Frontmatter.DedupKey,
				RunsAfter:         found.Frontmatter.RunsAfter,
				AppliesToFiles:    found.Frontmatter.AppliesTo.Files,
				AppliesToCommands: found.Frontmatter.AppliesTo.Commands,
				CanonicalRefs:     found.Frontmatter.CanonicalRefs,
				Pattern:           found.Frontmatter.Pattern,
				Lifecycle:         string(found.Frontmatter.EffectiveLifecycle()),
				SelectionGrade:    found.Frontmatter.SelectionGrade,
				ArchivedAt:        found.Frontmatter.ArchivedAt,
				ArchiveReason:     found.Frontmatter.ArchiveReason,
			},
			Body:       found.Body,
			SourcePath: found.SourcePath,
			Source:     source,
			Enabled:    enabled,
			HasCheck:   hasCheck,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate info: json encode: %v\n", err)
			return 2
		}
		return 0
	}

	// Human-readable view: detail block then full body.
	fmt.Printf("Frame:    %s\n", wantID)
	fmt.Printf("Category: %s\n", found.Frontmatter.Category)
	fmt.Printf("Tier:     %d\n", found.Frontmatter.EffectiveTier())
	fmt.Printf("Severity: %s", effectiveSeverity)
	if effectiveSeverity != string(found.Frontmatter.Severity) {
		fmt.Printf("  (overridden from %s)", found.Frontmatter.Severity)
	}
	fmt.Println()
	fmt.Printf("Triggers: %s\n", strings.Join(found.Frontmatter.Triggers, ", "))
	if len(found.Frontmatter.Tags) > 0 {
		fmt.Printf("Tags:     %s\n", strings.Join(found.Frontmatter.Tags, ", "))
	}
	if found.Frontmatter.DedupKey != "" {
		fmt.Printf("Dedup:    %s\n", found.Frontmatter.DedupKey)
	}
	fmt.Printf("Source:   %s (%s)\n", source, found.SourcePath)
	if found.Frontmatter.Pattern != "" {
		fmt.Printf("Pattern:  %s\n", found.Frontmatter.Pattern)
	}
	lc := string(found.Frontmatter.EffectiveLifecycle())
	fmt.Printf("Lifecycle: %s", lc)
	if !frames.IsGated(found.Frontmatter.EffectiveLifecycle()) {
		fmt.Printf("  ⚠ not actively gating")
	}
	fmt.Println()
	if found.Frontmatter.SelectionGrade != "" {
		fmt.Printf("Selection grade: %s\n", found.Frontmatter.SelectionGrade)
	}
	if found.Frontmatter.ArchivedAt != "" {
		fmt.Printf("Archived at: %s\n", found.Frontmatter.ArchivedAt)
	}
	if found.Frontmatter.ArchiveReason != "" {
		fmt.Printf("Archive reason: %s\n", found.Frontmatter.ArchiveReason)
	}
	fmt.Printf("Enabled:  %v", enabled)
	if !hasCheck {
		fmt.Printf("  ⚠ no check function bound, will report ERROR if it runs")
	}
	fmt.Println()
	fmt.Println()
	fmt.Println(found.Body)
	return 0
}
