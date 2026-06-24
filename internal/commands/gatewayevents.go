// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"
)

// eventGroup buckets an event.Event string into one of the four UI chips.
// Unknown events fall into "other" so they remain searchable but the chips
// don't claim them.
func eventGroup(name string) string {
	switch name {
	case "add", "archive", "restore", "migrate-layout":
		return "lifecycle"
	case "scan-first-push", "scan-apply", "scan-dismiss", "scan-rescan":
		return "scan"
	case "frame-severity", "repo-toggle",
		"linter-add", "linter-delete", "linter-severity", "linter-enabled":
		return "tuning"
	case "whitelist-add", "whitelist-remove":
		return "whitelist"
	}
	return "other"
}

type eventRow struct {
	Event    gateway.Event
	Group    string
	Hour     int
	TimeAttr string
	TimeStr  string
	Summary  string // formatted one-liner from formatEventPayload
	RawJSON  string // raw JSON; shown on row expand
}

type eventsPageData struct {
	Notice    string
	Events    []eventRow
	Repos     []string
	Repo      string // selected repo (preserves across limit changes)
	Limit     int    // currently-applied limit (0 = unlimited)
	LimitOpts []int  // dropdown choices
	Total     int    // count before tail
	Truncated bool   // Total > len(Events)
}

// eventLimitOpts is the allowlist surfaced in the dropdown. 0 = "all" (no
// truncation). Any other ?limit= value falls back to defaultEventsLimit so
// a stray query string can't blow up memory.
var eventLimitOpts = []int{50, 100, 500, 1000, 0}

const defaultEventsLimit = 100

func parseEventsLimit(s string) int {
	if s == "" {
		return defaultEventsLimit
	}
	for _, n := range eventLimitOpts {
		if strconv.Itoa(n) == s {
			return n
		}
	}
	return defaultEventsLimit
}

func buildEventRow(e gateway.Event) eventRow {
	t := e.Timestamp.UTC()
	var raw string
	if len(e.Payload) > 0 {
		if b, err := json.Marshal(e.Payload); err == nil {
			raw = string(b)
		}
	}
	return eventRow{
		Event:    e,
		Group:    eventGroup(e.Event),
		Hour:     t.Hour(),
		TimeAttr: t.Format("2006-01-02T15:04:05Z"),
		TimeStr:  t.Format("2006-01-02 15:04:05") + "Z",
		Summary:  formatEventPayload(e.Event, e.Payload),
		RawJSON:  raw,
	}
}

// formatEventPayload returns a one-line operator-facing summary of an event's
// payload, picking the 1-2 fields that matter for that event type. Falls back
// to "" (empty cell) when the payload carries nothing operator-useful (the
// column header reads "details", an empty cell reads as "no details"). The raw
// JSON is still available on row-expand for debugging.
func formatEventPayload(name string, payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	get := func(k string) string {
		if v, ok := payload[k]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	getBool := func(k string) (bool, bool) {
		if v, ok := payload[k]; ok {
			if b, ok := v.(bool); ok {
				return b, true
			}
		}
		return false, false
	}
	switch name {
	case "add":
		parts := []string{}
		if u := get("upstream"); u != "" {
			parts = append(parts, "upstream="+u)
		}
		if k := get("kit"); k != "" {
			parts = append(parts, "kit="+k)
		}
		if ss, ok := getBool("security_strict"); ok && ss {
			parts = append(parts, "+security-strict")
		}
		if cs, ok := getBool("credential_set"); ok && cs {
			parts = append(parts, "credential-set")
		}
		return strings.Join(parts, " · ")
	case "archive", "restore":
		return "" // repo column already says everything
	case "migrate-layout":
		if m, ok := payload["migrated"]; ok {
			if list, ok := m.([]any); ok {
				return fmt.Sprintf("%d repo(s)", len(list))
			}
		}
		return ""
	case "scan-apply":
		if ag, ok := payload["applied_groups"]; ok {
			if list, ok := ag.([]any); ok {
				return fmt.Sprintf("%d group(s) applied", len(list))
			}
		}
		return ""
	case "scan-first-push", "scan-dismiss", "scan-rescan":
		return "" // no payload
	case "frame-severity":
		f := get("frame")
		sev := get("severity")
		if f != "" && sev != "" {
			return f + " → " + sev
		}
		return ""
	case "repo-toggle":
		if enabled, ok := getBool("enabled"); ok {
			if enabled {
				return "enabled=true"
			}
			return "enabled=false"
		}
		return ""
	case "linter-add":
		parts := []string{}
		if n := get("name"); n != "" {
			parts = append(parts, n)
		}
		if s := get("severity"); s != "" {
			parts = append(parts, s)
		}
		return strings.Join(parts, " · ")
	case "linter-delete":
		return get("name")
	case "linter-severity":
		n := get("name")
		s := get("severity")
		if n != "" && s != "" {
			return n + " → " + s
		}
		return n
	case "linter-enabled":
		n := get("name")
		if enabled, ok := getBool("enabled"); ok {
			state := "off"
			if enabled {
				state = "on"
			}
			if n != "" {
				return n + " " + state
			}
			return state
		}
		return n
	case "whitelist-add":
		parts := []string{}
		if f := get("frame"); f != "" {
			parts = append(parts, f)
		}
		if p := get("path"); p != "" {
			parts = append(parts, "path="+p)
		}
		if r := get("reason"); r != "" {
			parts = append(parts, "\""+r+"\"")
		}
		return strings.Join(parts, " · ")
	case "whitelist-remove":
		parts := []string{}
		if f := get("frame"); f != "" {
			parts = append(parts, f)
		}
		if p := get("path"); p != "" {
			parts = append(parts, "path="+p)
		}
		return strings.Join(parts, " · ")
	case "credential-update":
		return "" // intentionally no operator-useful details (security policy)
	case "set-groups":
		if g, ok := payload["groups"]; ok {
			if list, ok := g.([]any); ok {
				return fmt.Sprintf("%d group(s)", len(list))
			}
		}
		return ""
	case "ssh-key-add", "ssh-key-remove":
		if fp := get("fingerprint"); fp != "" {
			return fp
		}
		return ""
	case "time-estimates-update":
		return "" // variable structured payload; raw expand is the right view
	case "build-update":
		from := get("from")
		to := get("to")
		if from == "" || to == "" {
			return ""
		}
		s := "build " + from + " → build " + to
		if dirty, ok := getBool("dirty"); ok && dirty {
			s += " (dirty)"
		}
		return s
	}
	// Unknown event type: fall back to first one or two keys formatted compactly.
	parts := []string{}
	for k, v := range payload {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		if len(parts) >= 2 {
			break
		}
	}
	return strings.Join(parts, " · ")
}

var eventsTmpl = func() *template.Template {
	t := template.New("events").Funcs(template.FuncMap{"icon": gwicons.HTML})
	template.Must(t.New("content").Parse(`<section>
<h2 class="gw-pagehead">Events</h2>
<p class="gw-pagedesc">Structured mutation log: every dashboard write (repo registrations, policy changes, key + credential rotations) since the gateway last started fresh. {{if .Truncated}}Showing <b>{{len .Events}}</b> of {{.Total}}.{{else}}<b>{{len .Events}}</b> total.{{end}}</p>
<div class="frame">
{{if .Notice}}<div class="warn">{{icon "warn"}} {{.Notice}}</div>{{end}}
<form class="gw-filters" method="get" action="/events">
  <select name="repo" data-events-repo onchange="this.form.submit()">
    <option value="">all repos</option>
    {{$cur := .Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}
  </select>
  <label class="sub">show <select name="limit" data-events-limit onchange="this.form.submit()">{{$lcur := .Limit}}{{range .LimitOpts}}<option value="{{.}}"{{if eq . $lcur}} selected{{end}}>{{if eq . 0}}all{{else}}{{.}}{{end}}</option>{{end}}</select></label>
  <input type="search" id="events-search" class="gw-searchbox" placeholder="filter events…" aria-label="filter events">
  <span id="events-count" class="sub"></span>
  <span class="gw-statusfilter"><span class="sub">group:</span><span class="gw-sevchips">
    <button type="button" class="gw-evchip fnd INFO" data-evgroup="lifecycle" aria-pressed="true">lifecycle</button>
    <button type="button" class="gw-evchip fnd INFO" data-evgroup="scan" aria-pressed="true">scan</button>
    <button type="button" class="gw-evchip fnd INFO" data-evgroup="tuning" aria-pressed="true">tuning</button>
    <button type="button" class="gw-evchip fnd INFO" data-evgroup="whitelist" aria-pressed="true">whitelist</button>
    <button type="button" class="gw-evchip fnd" data-evgroup="other" aria-pressed="true">other</button>
  </span></span>
</form>
{{if .Events}}
<table class="fr" id="events-list">
<thead><tr><td class="k">time</td><td class="k">event</td><td class="k">repo</td><td class="k">details</td></tr></thead>
<tbody>
{{range .Events}}<tr data-evgroup="{{.Group}}" data-repo="{{.Event.Repo}}"><td class="loc"><time class="gw-ts gw-tc-{{.Hour}}" datetime="{{.TimeAttr}}">{{.TimeStr}}</time></td><td class="k">{{.Event.Event}}</td><td class="k">{{.Event.Repo}}</td><td>{{if .RawJSON}}<details class="gw-event-details"><summary>{{if .Summary}}{{.Summary}}{{else}}<span class="sub">(raw)</span>{{end}}</summary><pre class="gw-event-raw">{{.RawJSON}}</pre></details>{{end}}</td></tr>
{{end}}</tbody>
</table>
{{else}}<div class="sub">No events recorded yet.</div>{{end}}
</div>
</section>`))
	return t
}()

func renderEventsPage(w http.ResponseWriter, data eventsPageData, chrome chromeData) {
	var buf bytes.Buffer
	if err := eventsTmpl.ExecuteTemplate(&buf, "content", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderGwShell(w, gwLayout{Title: "events: gateway", Chrome: chrome, Content: template.HTML(buf.String())})
}

// serveGatewayEvents reads <policy-root>/_events.jsonl, presents newest-first.
// Read-only: no --allow-edits guard, no CSRF - operators inspect; mutation
// routes already exist where mutations belong.
func serveGatewayEvents(policyRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events" {
			http.NotFound(w, r)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			repo = ""
		}
		limit := parseEventsLimit(r.URL.Query().Get("limit"))
		events, err := gateway.ReadEvents(policyRoot, nil)
		notice := policyRootNotice(policyRoot)
		if err != nil && notice == "" {
			notice = "events read error: " + err.Error()
		}
		// Newest first - matches /feed default ordering.
		sort.Slice(events, func(i, j int) bool { return events[i].Timestamp.After(events[j].Timestamp) })
		total := len(events)
		truncated := false
		if limit > 0 && total > limit {
			events = events[:limit]
			truncated = true
		}
		rows := make([]eventRow, 0, len(events))
		for _, e := range events {
			rows = append(rows, buildEventRow(e))
		}
		repos := listGatewayRepos(policyRoot)
		renderEventsPage(w, eventsPageData{
			Notice:    notice,
			Events:    rows,
			Repos:     repos,
			Repo:      repo,
			Limit:     limit,
			LimitOpts: eventLimitOpts,
			Total:     total,
			Truncated: truncated,
		}, buildChrome("events", repo, policyRoot))
	}
}
