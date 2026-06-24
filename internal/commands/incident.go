// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/frames"
	"nimblegate/internal/incident"
	"nimblegate/internal/paths"
)

// Incident routes `nimblegate incident <subcommand>`.
// Subcommands: new, list, promote.
// `from-bypass` is exposed as a flag on `new` rather than a peer subcommand -
// post-bypass capture is just "new + extra context."
func Incident(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate incident: subcommand required")
		fmt.Fprintln(os.Stderr, "usage: nimblegate incident <new|list|promote> [args...]")
		return 2
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "new":
		return incidentNew(rest)
	case "list":
		return incidentList(rest)
	case "promote":
		return incidentPromote(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate incident: capture footguns as drafts, promote them into new frames")
		fmt.Println()
		fmt.Println("Usage: nimblegate incident <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  new --title \"...\" [--source ...]   Scaffold a draft incident under .appframes/_incidents/")
		fmt.Println("  list [--status promoted|draft]      Browse drafts (default: drafts only)")
		fmt.Println("  promote <slug> --category ... --name ... --tier N --severity ... --triggers ...")
		fmt.Println("                                      Turn a draft into a frame stub at the right path")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate incident: unknown subcommand %q (use new | list | promote; --help for usage)\n", sub)
		return 2
	}
}

// incidentNew scaffolds a fresh draft incident file. Title is mandatory.
// All other flags are optional; the body is the embedded template the user
// then fills in by hand.
func incidentNew(args []string) int {
	fs := flag.NewFlagSet("incident new", flag.ExitOnError)
	title := fs.String("title", "", "human-readable title (required)")
	hours := fs.Float64("time-cost-hours", 0, "estimated debug time cost")
	tagCSV := fs.String("tags", "", "comma-separated tags")
	srcFrame := fs.String("from-frame", "", "frame ID this incident came from (sets source=bypass)")
	srcReason := fs.String("from-reason", "", "the --force-yes reason that triggered capture")
	srcCommand := fs.String("from-command", "", "the command that was bypassed")
	asJSON := fs.Bool("json", false, "emit JSON describing the created file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if strings.TrimSpace(*title) == "" {
		fmt.Fprintln(os.Stderr, "nimblegate incident new: --title is required")
		return 2
	}

	root, err := projectRootFor("incident new")
	if err != nil {
		return 2
	}

	source := incident.SourceManual
	if *srcFrame != "" || *srcReason != "" || *srcCommand != "" {
		source = incident.SourceBypass
	}

	var tags []string
	if *tagCSV != "" {
		for _, t := range strings.Split(*tagCSV, ",") {
			if s := strings.TrimSpace(t); s != "" {
				tags = append(tags, s)
			}
		}
	}

	now := time.Now().UTC()
	draft := incident.NewDraft(incident.NewDraftOptions{
		Title:         *title,
		Date:          now,
		TimeCostHours: *hours,
		Tags:          tags,
		Source:        source,
		SourceFrame:   *srcFrame,
		SourceReason:  *srcReason,
		SourceCommand: *srcCommand,
	})

	dir := filepath.Join(paths.AppframesDir(root), incident.IncidentsDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident new: mkdir %s: %v\n", dir, err)
		return 2
	}
	fn := incident.Filename(now, incident.Slugify(*title))
	dst := filepath.Join(dir, fn)
	if _, err := os.Stat(dst); err == nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident new: %s already exists (rename via --title)\n", dst)
		return 1
	}
	draft.SourcePath = dst
	if err := draft.Write(); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident new: write: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"path":   dst,
			"slug":   draft.Slug(),
			"title":  *title,
			"source": string(source),
		})
		return 0
	}
	fmt.Printf("Created %s\n", dst)
	fmt.Printf("  slug: %s\n", draft.Slug())
	fmt.Println("  next: edit the file to fill in Incident / Detection signal / Frame proposal sections.")
	fmt.Printf("        when ready: nimblegate incident promote %s --category <cat> --name <name> --tier <N> --severity <BLOCK|WARN|INFO> --triggers <comma>\n", draft.Slug())
	return 0
}

// incidentListItem is the JSON shape for one row of `incident list`.
type incidentListItem struct {
	Slug          string   `json:"slug"`
	Title         string   `json:"title"`
	Date          string   `json:"date"`
	Status        string   `json:"status"`
	PromotedTo    string   `json:"promoted_to,omitempty"`
	Source        string   `json:"source"`
	SourceFrame   string   `json:"source_frame,omitempty"`
	TimeCostHours float64  `json:"time_cost_hours,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Path          string   `json:"path"`
}

type incidentListOutput struct {
	Source     string             `json:"source"`
	Total      int                `json:"total"`
	Draft      int                `json:"draft"`
	Promoted   int                `json:"promoted"`
	Items      []incidentListItem `json:"items"`
	LoadErrors []string           `json:"load_errors,omitempty"`
}

func incidentList(args []string) int {
	fs := flag.NewFlagSet("incident list", flag.ExitOnError)
	statusFlag := fs.String("status", "", "filter to one status: draft | promoted")
	asJSON := fs.Bool("json", false, "emit JSON for scripting / future UI")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := projectRootFor("incident list")
	if err != nil {
		return 2
	}
	dir := filepath.Join(paths.AppframesDir(root), incident.IncidentsDirName)
	incs, loadErrs := incident.LoadFromDir(dir)

	out := incidentListOutput{Source: dir}
	for _, inc := range incs {
		st := string(inc.Frontmatter.Status)
		if st == "" {
			st = string(incident.StatusDraft)
		}
		if *statusFlag != "" && *statusFlag != st {
			continue
		}
		item := incidentListItem{
			Slug:          inc.Slug(),
			Title:         inc.Frontmatter.Title,
			Date:          inc.Frontmatter.Date,
			Status:        st,
			PromotedTo:    inc.Frontmatter.PromotedTo,
			Source:        string(inc.Frontmatter.Source),
			SourceFrame:   inc.Frontmatter.SourceFrame,
			TimeCostHours: inc.Frontmatter.TimeCostHours,
			Tags:          inc.Frontmatter.Tags,
			Path:          inc.SourcePath,
		}
		switch st {
		case "draft":
			out.Draft++
		case "promoted":
			out.Promoted++
		}
		out.Items = append(out.Items, item)
	}
	out.Total = len(out.Items)
	for _, e := range loadErrs {
		out.LoadErrors = append(out.LoadErrors, e.Error())
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		if len(loadErrs) > 0 {
			return 1
		}
		return 0
	}

	for _, e := range loadErrs {
		fmt.Fprintf(os.Stderr, "nimblegate incident list: %v\n", e)
	}
	if out.Total == 0 {
		fmt.Println("No incidents.")
		fmt.Printf("(checked %s)\n", dir)
		return 0
	}
	fmt.Printf("Incidents in %s\n", dir)
	fmt.Printf("  %d total: %d draft, %d promoted\n\n", out.Total, out.Draft, out.Promoted)
	fmt.Printf("%-12s  %-10s  %-40s  %s\n", "Date", "Status", "Slug", "Title")
	for _, it := range out.Items {
		title := it.Title
		if len(title) > 60 {
			title = title[:57] + "..."
		}
		fmt.Printf("%-12s  %-10s  %-40s  %s\n",
			frames.SanitizeForOutput(it.Date),
			frames.SanitizeForOutput(it.Status),
			frames.SanitizeForOutput(it.Slug),
			frames.SanitizeForOutput(title),
		)
		if it.PromotedTo != "" {
			fmt.Printf("%-12s  %-10s    └─ promoted to: %s\n", "", "", frames.SanitizeForOutput(it.PromotedTo))
		}
	}
	return 0
}

// validCategories is a local copy of the 11 canonical category values.
// Kept local because the parser's canonical map is an unexported inline; copy is cheap.
var validCategories = map[string]bool{
	"security":        true,
	"network":         true,
	"filesystem":      true,
	"git":             true,
	"commands":        true,
	"app-correctness": true,
	"database":        true,
	"web":             true,
	"documentation":   true,
	"platform":        true,
	"framework":       true,
}

var validSeverities = map[string]bool{
	"BLOCK": true, "WARN": true, "INFO": true,
}

var validTriggers = map[string]bool{
	"cli": true, "pre-commit": true, "git-wrap": true, "watcher": true, "server": true,
}

// frameNameRegex matches the same kebab-name constraint the frame parser
// enforces; reproduced here to give a clear error before writing the file.
func validFrameName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func incidentPromote(args []string) int {
	// Extract the slug positional ourselves so the user can write the natural
	// `promote <slug> --category ...` ordering. Go's flag package stops at
	// the first non-flag arg, which would otherwise force all flags before
	// the slug.
	slug, rest, perr := extractPositional(args)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: %v\n", perr)
		fmt.Fprintln(os.Stderr, "usage: nimblegate incident promote <slug> --category <cat> --name <name> --tier <N> --severity <BLOCK|WARN|INFO> --triggers <comma>")
		return 2
	}

	fs := flag.NewFlagSet("incident promote", flag.ExitOnError)
	category := fs.String("category", "", "frame category (required)")
	name := fs.String("name", "", "frame kebab-name (required)")
	tier := fs.Int("tier", 0, "frame tier 1-6 (required)")
	severity := fs.String("severity", "", "BLOCK / WARN / INFO (required)")
	triggerCSV := fs.String("triggers", "", "comma-separated trigger list (required)")
	asJSON := fs.Bool("json", false, "emit JSON describing the created frame stub")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: unexpected extra positional arg %q (only the slug is positional)\n", fs.Arg(0))
		return 2
	}

	if !validCategories[*category] {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: invalid --category %q (must be one of: security, network, filesystem, git, commands, app-correctness, database, web, documentation, platform, framework)\n", *category)
		return 2
	}
	if !validFrameName(*name) {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: invalid --name %q (must match [a-zA-Z0-9][a-zA-Z0-9_-]*)\n", *name)
		return 2
	}
	if *tier < 1 || *tier > 6 {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: --tier must be 1-6 (got %d)\n", *tier)
		return 2
	}
	if !validSeverities[*severity] {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: invalid --severity %q (must be BLOCK / WARN / INFO)\n", *severity)
		return 2
	}
	var triggers []string
	for _, t := range strings.Split(*triggerCSV, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if !validTriggers[t] {
			fmt.Fprintf(os.Stderr, "nimblegate incident promote: invalid trigger %q (allowed: cli, pre-commit, git-wrap, watcher, server)\n", t)
			return 2
		}
		triggers = append(triggers, t)
	}
	if len(triggers) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate incident promote: --triggers must list at least one trigger")
		return 2
	}

	root, err := projectRootFor("incident promote")
	if err != nil {
		return 2
	}
	incDir := filepath.Join(paths.AppframesDir(root), incident.IncidentsDirName)
	inc, err := incident.FindBySlug(incDir, slug)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: %v\n", err)
		return 1
	}
	frameID := *category + "/" + *name

	frameDst := filepath.Join(paths.AppframesDir(root), *category, *name+".md")
	if _, err := os.Stat(frameDst); err == nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: %s already exists\n", frameDst)
		return 1
	}

	frameMarkdown := renderFrameStub(frameStubInput{
		FrameID:                frameID,
		Name:                   *name,
		Category:               *category,
		Severity:               *severity,
		Tier:                   *tier,
		Triggers:               triggers,
		IncidentPath:           rel(root, inc.SourcePath),
		IncidentTitle:          inc.Frontmatter.Title,
		TimeCostHoursPrevented: inc.Frontmatter.TimeCostHours,
	})

	if err := os.MkdirAll(filepath.Dir(frameDst), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: mkdir: %v\n", err)
		return 2
	}
	if err := os.WriteFile(frameDst, []byte(frameMarkdown), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: write frame: %v\n", err)
		return 2
	}

	inc.Frontmatter.Status = incident.StatusPromoted
	inc.Frontmatter.PromotedTo = frameID
	if err := inc.Write(); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate incident promote: update incident: %v\n", err)
		return 2
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{
			"frame_id":      frameID,
			"frame_path":    frameDst,
			"incident_path": inc.SourcePath,
			"incident_slug": slug,
		})
		return 0
	}
	fmt.Printf("Promoted incident %q → frame %s\n", slug, frameID)
	fmt.Printf("  frame stub:    %s\n", frameDst)
	fmt.Printf("  incident file: %s (marked promoted)\n", inc.SourcePath)
	fmt.Println()
	fmt.Println("Next:")
	fmt.Printf("  1. Implement the check function for %s in internal/checks/ (or .appframes/ for a project-local frame)\n", frameID)
	fmt.Printf("  2. Bind it in internal/commands/builtin.go (or load it via project frames)\n")
	fmt.Printf("  3. `nimblegate enable %s`\n", frameID)
	fmt.Printf("  4. `nimblegate lint` to validate the new frame\n")
	return 0
}

// extractPositional pulls exactly one positional argument out of args
// regardless of its position relative to flags. Returns the positional and
// the remaining args (in original order, with the positional removed) so
// the caller can parse the rest as flags.
//
// Recognized as a flag value (and thus NOT a positional): the argument
// immediately following any --foo (with no '=') that isn't itself a flag.
// Boolean flags break this - but promote takes no value-less flags other
// than --json, which is handled by the explicit allowlist below.
func extractPositional(args []string) (string, []string, error) {
	boolFlags := map[string]bool{"--json": true}
	var positional string
	var rest []string
	skipNext := false
	for i, a := range args {
		if skipNext {
			rest = append(rest, a)
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			if !boolFlags[a] && !strings.Contains(a, "=") {
				// Next arg (if any) is this flag's value.
				if i+1 < len(args) {
					skipNext = true
				}
			}
			continue
		}
		if positional != "" {
			rest = append(rest, a)
			continue
		}
		positional = a
	}
	if positional == "" {
		return "", nil, fmt.Errorf("exactly one slug argument required")
	}
	return positional, rest, nil
}

// projectRootFor wraps the FindProjectRoot/Getwd pair with a consistent
// error message for all incident subcommands.
func projectRootFor(label string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: getwd: %v\n", label, err)
		return "", err
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: %v\nHint: run `nimblegate init` here.\n", label, err)
		return "", err
	}
	return root, nil
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}

type frameStubInput struct {
	FrameID                string
	Name                   string
	Category               string
	Severity               string
	Tier                   int
	Triggers               []string
	IncidentPath           string
	IncidentTitle          string
	TimeCostHoursPrevented float64 // copied from the incident's time-cost-hours when > 0
}

// renderFrameStub returns the markdown for a fresh frame file derived from an
// incident. Frontmatter is fully formed (passes the frame parser). The body
// is a TODO checklist + a reference back to the incident, NOT a placeholder
// "fill me in" - the user has already filled in the incident; this body is
// the migration shell.
func renderFrameStub(in frameStubInput) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + in.Name + "\n")
	b.WriteString("category: " + in.Category + "\n")
	b.WriteString("severity: " + in.Severity + "\n")
	fmt.Fprintf(&b, "tier: %d\n", in.Tier)
	b.WriteString("triggers: [" + strings.Join(in.Triggers, ", ") + "]\n")
	if in.TimeCostHoursPrevented > 0 {
		fmt.Fprintf(&b, "time-cost-hours-prevented: %g\n", in.TimeCostHoursPrevented)
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + in.FrameID + "\n\n")
	b.WriteString("Promoted from incident: [`" + in.IncidentPath + "`](../" + in.IncidentPath + ")\n\n")
	if in.IncidentTitle != "" {
		b.WriteString("> " + in.IncidentTitle + "\n\n")
	}
	b.WriteString("## What this frame catches\n\n")
	b.WriteString("Fill in: one sentence stating the precondition this frame detects, in terms\nthat make the violation testable from the trigger surface.\n\n")
	b.WriteString("## How the check works\n\n")
	b.WriteString("Fill in: the mechanical procedure. File globs, command parsing, state queries.\nNo judgment calls, this is what the check function will implement.\n\n")
	b.WriteString("## Fix\n\n")
	b.WriteString("Fill in: what the user should do when this frame blocks. Concrete command,\nedit, or config change. Linked to the incident's \"Where the check belongs.\"\n\n")
	b.WriteString("## TODO before this frame is real\n\n")
	b.WriteString("- [ ] Implement the check function (see `internal/checks/` for examples)\n")
	b.WriteString("- [ ] Bind in `internal/commands/builtin.go` (or load as a project frame)\n")
	b.WriteString("- [ ] Add unit tests covering the failure case + at least one passing case\n")
	b.WriteString("- [ ] `nimblegate enable " + in.FrameID + "`\n")
	b.WriteString("- [ ] `nimblegate lint` clean\n")
	return b.String()
}
