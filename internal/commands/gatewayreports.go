// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"nimblegate/internal/gateway/agentapi"
)

// reportWindows are the day-based windows offered on the Reports page. The
// dropdown value IS the day count (the agent API is day-windowed; no all-time).
var reportWindows = []windowOpt{
	{"7", "Last 7 days"},
	{"30", "Last 30 days"},
	{"90", "Last 90 days"},
	{"365", "Last year"},
}

// reportButtons are the one-click reports, in display order.
var reportButtons = []struct{ Key, Label string }{
	{"what-changed", "What changed"},
	{"gate-stats", "Gate stats"},
	{"bounce-rate", "Bounce rate"},
	{"top-rules", "Top rules"},
	{"time-saved", "Time saved"},
	{"recurring", "Recurring findings"},
	{"decisions", "Decisions"},
}

type reportsPageData struct {
	Repos   []string
	Repo    string
	Windows []windowOpt
	Window  string
	RowOpts []int
	Rows    string
	Buttons []struct{ Key, Label string }
}

// reportRowOpts are the row-count choices on the Reports page. The dashboard
// raises the agent API's default 50 ceiling to reportMaxRows via Params.MaxLimit
// (the operator surface is in-process, not a small-model payload), so a wide
// window can actually show its full count instead of the newest 50. Aggregate
// reports use the count as the top-N cap.
var reportRowOpts = []int{25, 50, 100, 250, reportMaxRows}

// reportMaxRows is the dashboard's row ceiling - the absolute backstop the
// service enforces (clamp() caps MaxLimit here too).
const reportMaxRows = 500

var reportsTmpl = template.Must(template.New("reports").Parse(`<style>
.report-controls{display:flex;gap:18px;margin:14px 0}
.report-controls select{margin-left:6px}
.report-buttons{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0}
.report-buttons button{cursor:pointer}
.report-search{display:flex;align-items:center;gap:10px;margin:12px 0 2px;flex-wrap:wrap}
.report-search .sub{font-size:12px}
.report-row[hidden],.report-row.hide{display:none}
.report-title{margin:14px 0 6px}
.report-pre{white-space:pre;background:var(--gw-bg-alt,#11151c);padding:10px 12px;border-radius:6px;overflow-x:auto;font-size:13px;line-height:1.4;margin:0 0 8px}
.report-rows{background:var(--gw-bg-alt,#11151c);border-radius:6px;overflow-x:auto;margin:0 0 8px}
.report-row{display:flex;gap:10px;align-items:baseline;padding:3px 10px;border-bottom:1px solid var(--gw-border-row,#1c222b);font-size:13px;line-height:1.35;width:max-content;min-width:100%;box-sizing:border-box}
.report-row:last-child{border-bottom:0}
.report-row time{flex:0 0 auto;white-space:nowrap;font-variant-numeric:tabular-nums;font-size:12px}
.report-row time.report-date{color:var(--gw-text-muted,#8a93a0)}
.report-rb{flex:1 1 auto;white-space:pre}
.report-head{color:var(--gw-text-muted,#8a93a0);font-size:12px;margin:10px 0 4px}
.report-note{color:var(--gw-text-muted,#8a93a0);font-style:italic;background:rgba(202,162,74,.08);border-left:3px solid #caa24a;padding:4px 10px;margin:4px 0;font-size:12px;border-radius:0 4px 4px 0}
.rb-ok{color:#3ec46d}
.rb-rej{color:#e6675e;font-weight:600}
.sev-block{color:#e6675e;font-weight:600}
.sev-error{color:#e08a3c}
.sev-warn{color:#caa24a}
.sev-info{color:var(--gw-text-muted,#8a93a0)}
.report-err{color:var(--gw-danger,#e66);margin:14px 0}
.htmx-indicator{display:none;opacity:.7;margin:8px 0}
.htmx-request #report-spin{display:block}
</style>
<section class="frame">
  <h2 class="gw-section-head">Reports</h2>
  <p class="sub">One-click reports from the gate's own data: no agent or token needed. Pick a repo and window, then run a report.</p>
  <div class="report-controls">
    <label>Repo
      <select name="repo">
        <option value="">all repos</option>
        {{$r := .Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $r}} selected{{end}}>{{.}}</option>{{end}}
      </select>
    </label>
    <label>Window
      <select name="window">
        {{$w := .Window}}{{range .Windows}}<option value="{{.Value}}"{{if eq .Value $w}} selected{{end}}>{{.Label}}</option>{{end}}
      </select>
    </label>
    <label>Rows
      <select name="rows">
        {{$rw := .Rows}}{{range .RowOpts}}<option value="{{.}}"{{if eq (printf "%d" .) $rw}} selected{{end}}>{{.}}</option>{{end}}
      </select>
    </label>
  </div>
  <div class="report-buttons">
    {{range .Buttons}}<button type="button" hx-get="/reports/run?report={{.Key}}" hx-include="[name='repo'],[name='window'],[name='rows']" hx-target="#report-out" hx-indicator="#report-spin">{{.Label}}</button>{{end}}
  </div>
  <div class="report-search">
    <input type="search" id="report-filter" class="gw-searchbox" placeholder="filter rows…" aria-label="filter report rows" disabled>
    <span id="report-filter-count" class="sub"></span>
    <span id="report-filter-hint" class="sub">run a report above first, then filter its rows here</span>
  </div>
  <div id="report-spin" class="htmx-indicator">running…</div>
  <div id="report-out"></div>
</section>`))

// serveReports renders the Reports page (controls + buttons + empty result
// pane). Session-authed by the dashboard middleware; no token.
func serveReports(w http.ResponseWriter, r *http.Request, policyRoot string) {
	repo := r.URL.Query().Get("repo")
	if repo != "" && !validRepoName(repo) {
		repo = ""
	}
	data := reportsPageData{
		Repos:   listGatewayRepos(policyRoot),
		Repo:    repo,
		Windows: reportWindows,
		Window:  "30",
		RowOpts: reportRowOpts,
		Rows:    "50",
		Buttons: reportButtons,
	}
	var buf bytes.Buffer
	if err := reportsTmpl.Execute(&buf, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{
		Title:   "reports : gateway",
		Chrome:  buildChrome("reports", repo, policyRoot),
		Content: template.HTML(buf.String()),
	})
}

// serveReportRun runs one report in-process and returns an HTML fragment that
// htmx swaps into the result pane. Direct service calls - no bearer auth, no
// rate guard (those live in the HTTP/MCP handlers, not the methods).
func serveReportRun(w http.ResponseWriter, r *http.Request, svc *agentapi.Service) {
	report := r.URL.Query().Get("report")
	repo := r.URL.Query().Get("repo")
	if repo != "" && !validRepoName(repo) {
		repo = ""
	}
	days := 30
	switch r.URL.Query().Get("window") {
	case "7":
		days = 7
	case "90":
		days = 90
	case "365":
		days = 365
	}
	p := agentapi.Params{Repo: repo, Days: days, MaxLimit: reportMaxRows}
	if n, err := strconv.Atoi(r.URL.Query().Get("rows")); err == nil && n > 0 {
		p.Limit = n // service clamps to [1, reportMaxRows]
	}

	var res agentapi.Result
	var err error
	switch report {
	case "what-changed":
		res, err = svc.WhatChanged(p)
	case "gate-stats":
		res, err = svc.GateStats(p)
	case "bounce-rate":
		res, err = svc.BounceRate(p)
	case "top-rules":
		res, err = svc.TopRules(p)
	case "time-saved":
		res, err = svc.TimeSaved(p)
	case "recurring":
		res, err = svc.Recurring(p)
	case "decisions":
		res, err = svc.Decisions(p)
	default:
		reportFragment(w, "unknown report", "")
		return
	}
	if err != nil {
		reportFragment(w, "report failed: "+err.Error(), "")
		return
	}
	title := reportLabel(report)
	if repo != "" {
		title += " - " + repo
	}
	reportFragment(w, title, res.Text)
}

// reportFragment writes the htmx result fragment. Empty body → an inline
// error message; otherwise a title + the formatted report.
func reportFragment(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if body == "" {
		fmt.Fprintf(w, `<div class="report-err">%s</div>`, html.EscapeString(title))
		return
	}
	fmt.Fprintf(w, `<h3 class="report-title">%s</h3>%s`,
		html.EscapeString(title), formatReport(body))
}

// sevColorizer wraps verdict markers and severity tokens in colored spans.
// It runs on ALREADY-ESCAPED text and inserts only fixed, safe HTML - the
// escape-first-then-colorize order is what keeps it XSS-safe.
var sevColorizer = strings.NewReplacer(
	"✓ accepted", `<span class="rb-ok">✓ accepted</span>`,
	"✗ rejected", `<span class="rb-rej">✗ rejected</span>`,
	"ACCEPTED", `<span class="rb-ok">ACCEPTED</span>`,
	"REJECTED", `<span class="rb-rej">REJECTED</span>`,
	"(BLOCK)", `<span class="sev-block">(BLOCK)</span>`,
	"(ERROR)", `<span class="sev-error">(ERROR)</span>`,
	"(WARN)", `<span class="sev-warn">(WARN)</span>`,
	"(INFO)", `<span class="sev-info">(INFO)</span>`,
)

// reportDateTime matches a leading "YYYY-MM-DD HH:MM " (decisions); reportDate
// matches a leading "YYYY-MM-DD " with no time (what_changed commit lines).
var (
	reportDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) (\d{2}):(\d{2}) (.*)$`)
	reportDate     = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}) (.+)$`)
)

// reportTimeRow renders a timestamped report line as a compact feed-style row:
// an hour-colored time chip (left) + the colorized remainder. The chip reuses
// the dashboard's shared gw-ts/gw-tc-<hour> classes, so it honors the operator's
// time-color toggle exactly like the /feed table (data-tc="off" drops the hour
// colors). Returns ok=false when the line doesn't begin with a date.
func reportTimeRow(line string) (string, bool) {
	if m := reportDateTime.FindStringSubmatch(line); m != nil {
		date, hh, mm, rest := m[1], m[2], m[3], m[4]
		hour, _ := strconv.Atoi(hh)
		// Full UTC datetime (the report API formats timestamps in UTC) with a Z,
		// so the shared gwApplyTz converts the chip to the viewer's local zone
		// exactly like the /feed table - same datetime contract.
		return reportRowHTML(fmt.Sprintf("gw-ts gw-tc-%d", hour), date+"T"+hh+":"+mm+":00Z", date[5:]+" "+hh+":"+mm, rest), true
	}
	if m := reportDate.FindStringSubmatch(line); m != nil {
		date, rest := m[1], m[2]
		// Date-only (commit date) is a calendar date, not a tz-sensitive instant.
		// Use a NON-gw-ts class: the shared gwApplyTz rewrites every time.gw-ts
		// from its datetime, and `new Date(null)` (a missing attr) is the EPOCH,
		// not invalid - so a gw-ts date-only chip would render as 1970. A plain
		// report-date chip is left verbatim. No hour color applies to a date.
		return reportRowHTML("report-date", "", date, rest), true
	}
	return "", false
}

// reportRowHTML builds one feed-style row. cls/dt/chip are trusted-shape values
// derived from a matched timestamp; rest is escaped then colorized (escape-first
// keeps it XSS-safe, same as the pre path). An empty dt omits the datetime attr.
func reportRowHTML(cls, dt, chip, rest string) string {
	dtAttr := ""
	if dt != "" {
		dtAttr = ` datetime="` + html.EscapeString(dt) + `"`
	}
	return fmt.Sprintf(
		`<div class="report-row"><time class="%s"%s>%s</time><span class="report-rb">%s</span></div>`,
		cls, dtAttr, html.EscapeString(chip), sevColorizer.Replace(html.EscapeString(rest)))
}

// reportPlainRow renders a non-timestamped body line (the summary / stat lines
// from gate_stats, top_rules, bounce_rate, …) as a filterable row with no time
// chip, so the client-side search box works on every report - not just the two
// with timestamps. Leading whitespace is preserved (CSS white-space:pre) so the
// indentation of sub-lines survives.
func reportPlainRow(line string) string {
	return `<div class="report-row"><span class="report-rb">` + sevColorizer.Replace(html.EscapeString(line)) + `</span></div>`
}

// formatReport turns a report's plain text into readable HTML: the header line
// becomes a caption, "note:" lines become callouts, and every other line becomes
// a filterable .report-row inside a horizontally-scrollable .report-rows card -
// timestamped lines (decisions / what_changed) carry a time chip, the rest are
// plain content rows. Every line is HTML-escaped before any markup is added.
func formatReport(text string) template.HTML {
	var b, rows strings.Builder
	flushRows := func() {
		if rows.Len() > 0 {
			b.WriteString(`<div class="report-rows">` + rows.String() + `</div>`)
			rows.Reset()
		}
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "("):
			flushRows()
			b.WriteString(`<div class="report-head">` + html.EscapeString(line) + `</div>`)
		case strings.HasPrefix(line, "note:"):
			flushRows()
			b.WriteString(`<div class="report-note">` + html.EscapeString(line) + `</div>`)
		case strings.TrimSpace(line) == "":
			// drop blank separator lines - an empty bordered row reads as noise.
		default:
			if row, ok := reportTimeRow(line); ok {
				rows.WriteString(row)
			} else {
				rows.WriteString(reportPlainRow(line))
			}
		}
	}
	flushRows()
	return template.HTML(b.String())
}

// reportLabel maps a report key to its button label (or the key if unknown).
func reportLabel(key string) string {
	for _, b := range reportButtons {
		if b.Key == key {
			return b.Label
		}
	}
	return key
}
