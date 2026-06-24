// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package incident

import (
	_ "embed"
	"strings"
	"time"
)

//go:embed template.md
var bodyTemplate string

// NewDraftOptions describes a freshly-scaffolded incident.
type NewDraftOptions struct {
	Title         string
	Date          time.Time
	TimeCostHours float64
	Tags          []string

	// Source defaults to SourceManual. When set to SourceBypass, the next
	// three fields are persisted to frontmatter so `from-bypass` runs are
	// reconstructable.
	Source        Source
	SourceFrame   string
	SourceReason  string
	SourceCommand string
}

// NewDraft assembles a fresh draft incident in memory. SourcePath is left
// blank; the caller picks the destination dir and filename and sets it
// before Write.
func NewDraft(opts NewDraftOptions) Incident {
	if opts.Date.IsZero() {
		opts.Date = time.Now().UTC()
	}
	src := opts.Source
	if src == "" {
		src = SourceManual
	}
	body := strings.ReplaceAll(bodyTemplate, "{{title}}", opts.Title)
	if opts.Source == SourceBypass {
		body = prependBypassContext(body, opts)
	}
	return Incident{
		Frontmatter: Frontmatter{
			Title:         opts.Title,
			Date:          opts.Date.Format("2006-01-02"),
			TimeCostHours: opts.TimeCostHours,
			Status:        StatusDraft,
			Source:        src,
			SourceFrame:   opts.SourceFrame,
			SourceReason:  opts.SourceReason,
			SourceCommand: opts.SourceCommand,
			Tags:          opts.Tags,
		},
		Body: body,
	}
}

func prependBypassContext(body string, opts NewDraftOptions) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(opts.Title)
	b.WriteString("\n\n")
	b.WriteString("> Captured from `--force-yes` bypass on ")
	b.WriteString(opts.Date.Format("2006-01-02"))
	b.WriteString(".\n>\n")
	if opts.SourceFrame != "" {
		b.WriteString("> Bypassed frame: `")
		b.WriteString(opts.SourceFrame)
		b.WriteString("`\n")
	}
	if opts.SourceCommand != "" {
		b.WriteString("> Command: `")
		b.WriteString(opts.SourceCommand)
		b.WriteString("`\n")
	}
	if opts.SourceReason != "" {
		b.WriteString("> Reason given: ")
		b.WriteString(opts.SourceReason)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	// Strip the leading H1 from the template body since we wrote a richer one
	// above.
	stripped := body
	if idx := strings.Index(stripped, "\n\n"); idx > 0 && strings.HasPrefix(stripped, "# ") {
		stripped = stripped[idx+2:]
	}
	b.WriteString(stripped)
	return b.String()
}
