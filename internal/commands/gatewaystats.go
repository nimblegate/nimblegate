// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"html/template"
	"net/http"
	"net/url"
	"time"

	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/gwicons"
)

type windowOpt struct{ Value, Label string }

var statsWindows = []windowOpt{
	{"", "All time"},
	{"24h", "Last 24h"},
	{"7d", "Last 7 days"},
	{"30d", "Last 30 days"},
}

// windowSince maps a window dropdown value to a lower time bound; unknown/"" → zero (no bound).
func windowSince(window string) time.Time {
	switch window {
	case "24h":
		return time.Now().Add(-24 * time.Hour)
	case "7d":
		return time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		return time.Now().Add(-30 * 24 * time.Hour)
	default:
		return time.Time{}
	}
}

type statsPageData struct {
	Repo         string
	Window       string
	ActiveTab    string // "time-saved" (default) | "recurring"
	Repos        []string
	Windows      []windowOpt
	Notice       string
	Err          string
	Warn         string
	Blocks       []repoBlock
	TotDecisions int
	TotAccepts   int
	TotRejects   int
	TotActualStr string
	TotModelStr  string
	AllowEdits   bool
	CSRFToken    string
}

// statsTabStrip renders the Time saved / Recurring findings tabs. Tab links
// preserve the current repo + window filter so changing tab doesn't reset the
// scope the operator picked.
func statsTabStrip(activeTab, repo, window string) template.HTML {
	cls := func(t string) string {
		if t == activeTab {
			return "autopr-tab active"
		}
		return "autopr-tab"
	}
	link := func(t string) string {
		v := url.Values{}
		v.Set("tab", t)
		if repo != "" {
			v.Set("repo", repo)
		}
		if window != "" {
			v.Set("window", window)
		}
		return "/stats?" + v.Encode()
	}
	return template.HTML(`<style>
.autopr-tabs{display:flex;gap:2px;margin:18px 0;border-bottom:1px solid var(--gw-border);padding:0}
.autopr-tab{display:inline-block;padding:10px 18px;color:var(--gw-text-muted);text-decoration:none;font-size:13px;font-weight:500;border-bottom:2px solid transparent;margin-bottom:-1px}
.autopr-tab:hover{color:var(--gw-text)}
.autopr-tab.active{color:var(--gw-accent);border-bottom-color:var(--gw-accent);font-weight:600}
</style>
<nav class="autopr-tabs">` +
		`<a href="` + link("time-saved") + `" class="` + cls("time-saved") + `">Time saved</a>` +
		`<a href="` + link("recurring") + `" class="` + cls("recurring") + `">Recurring findings</a>` +
		`</nav>`)
}

func serveStats(w http.ResponseWriter, r *http.Request, policyRoot string, allowEdits bool, csrfToken string) {
	repo := r.URL.Query().Get("repo")
	if repo != "" && !validRepoName(repo) {
		repo = ""
	}
	window := r.URL.Query().Get("window")
	tab := r.URL.Query().Get("tab")
	if tab != "recurring" {
		tab = "time-saved"
	}

	data := statsPageData{
		Repo:      repo,
		Window:    window,
		ActiveTab: tab,
		Repos:     listGatewayRepos(policyRoot),
		Windows:   statsWindows,
		Notice:    policyRootNotice(policyRoot),
	}
	data.AllowEdits = allowEdits
	data.CSRFToken = csrfToken

	if data.Notice == "" {
		if db, err := analytics.Open(analyticsDBPath(policyRoot)); err != nil {
			data.Err = "open analytics db: " + err.Error()
		} else {
			defer db.Close()
			if _, err := analytics.Ingest(db, policyRoot); err != nil {
				data.Warn = "couldn't refresh (showing existing data): " + err.Error()
			}
			scope := data.Repos
			if repo != "" {
				scope = []string{repo}
			}
			since := windowSince(window)
			var actual, modeled float64
			for _, rp := range scope {
				b := buildRepoBlock(db, policyRoot, rp, since)
				b.AllowEdits = allowEdits
				data.Blocks = append(data.Blocks, b)
				data.TotDecisions += b.Decisions
				data.TotAccepts += b.Accepts
				data.TotRejects += b.Rejects
				actual += b.ActualHours
				modeled += b.ModeledHours
			}
			data.TotActualStr = formatHours(actual)
			data.TotModelStr = formatHours(modeled)
		}
	}
	renderStatsPage(w, data, buildChrome("stats", repo, policyRoot))
}

func renderStatsPage(w http.ResponseWriter, data statsPageData, chrome chromeData) {
	if data.ActiveTab == "" {
		data.ActiveTab = "time-saved"
	}
	// Wrap data with the pre-rendered tab strip so the content template can
	// emit it as raw HTML (without re-escaping).
	tmplData := struct {
		statsPageData
		TabStrip template.HTML
	}{data, statsTabStrip(data.ActiveTab, data.Repo, data.Window)}
	var buf bytes.Buffer
	if err := statsTmpl.ExecuteTemplate(&buf, "content", tmplData); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{Title: "stats : gateway", CSRFToken: data.CSRFToken, Chrome: chrome, Content: template.HTML(buf.String())})
}

var tsFuncs = template.FuncMap{
	"tsAttr": func(unix int64) string { return time.Unix(unix, 0).UTC().Format("2006-01-02T15:04:05Z") },
	"tsText": func(unix int64) string { return time.Unix(unix, 0).UTC().Format("2006-01-02 15:04:05") + "Z" },
	"tsHour": func(unix int64) int { return time.Unix(unix, 0).UTC().Hour() },
	"icon":   gwicons.HTML,
}

var statsTmpl = func() *template.Template {
	t := template.New("stats").Funcs(tsFuncs)
	template.Must(t.New("block-header").Parse(`<h2 class="gw-stats-repo">{{.Repo}}</h2>{{if .HasData}}<div class="sub">decisions: {{.Decisions}} · accepts: {{.Accepts}} · rejects: {{.Rejects}}</div>{{else}}<div class="sub">No decisions recorded yet.</div>{{end}}`))
	template.Must(t.New("block-timesaved").Parse(`{{if .HasData}}<h3 class="gw-section-head">Estimated debugging time saved</h3><div class="sub">Actually prevented: <b>{{.ActualHoursStr}}</b> · Modeled / would-have: <b>{{.ModeledHoursStr}}</b></div><div class="sub">Hours of debugging, code review, or rollback work the gateway prevented by catching these issues at push time. <b>Actually prevented</b> counts pushes that were blocked AND subsequently fixed (audit-log verified). <b>Modeled</b> applies conservative per-tier defaults (or this repo's <code>[time-estimates]</code> overrides) to every distinct block, an upper-bound estimate, not a measured figure.</div>{{if .TimeRows}}<details><summary>Per-frame breakdown</summary><table class="fr"><tr><td class="k">frame</td><td class="k">rejected</td><td class="k">observed</td><td class="k">hrs/hit</td><td class="k">source</td><td class="k">actual h</td><td class="k">modeled h</td></tr>{{range .TimeRows}}<tr><td><a href="/frames?id={{.FrameID}}">{{.FrameID}}</a></td><td>{{.Rejected}}</td><td>{{.Observed}}</td><td>{{printf "%.2f" .HoursPerHit}}</td><td><span class="fnd INFO">{{.Source}}</span></td><td>{{printf "%.1f" .ActualSub}}</td><td>{{printf "%.1f" .ModeledSub}}</td></tr>{{end}}</table></details>{{end}}{{end}}`))
	template.Must(t.New("block-recurring").Parse(`{{if and .HasData .Recurring}}<h3 class="gw-section-head">Recurring findings ({{len .Recurring}})</h3><table class="fr"><tr><td class="k">severity</td><td class="k">frame</td><td class="k">location</td><td class="k">seen</td><td class="k">last seen</td>{{if $.AllowEdits}}<td class="k"></td>{{end}}</tr>{{range .Recurring}}<tr><td><span class="fnd {{.Severity}}">{{.Severity}}</span></td><td><a href="/frames?id={{.FrameID}}">{{.FrameID}}</a></td><td>{{.Message}}</td><td>{{.Seen}}×</td><td><time class="gw-ts gw-tc-{{tsHour .LastSeen}}" datetime="{{tsAttr .LastSeen}}">{{tsText .LastSeen}}</time></td>{{if $.AllowEdits}}<td><details class="wl"><summary>whitelist</summary><form><input type="hidden" name="repo" value="{{$.Repo}}"><input type="hidden" name="frame" value="{{.FrameID}}"><label>Path<input type="text" name="path" value="{{.Path}}" oninput="wlScope(this)"></label><span class="wlscope">matches: this one file</span><p class="hint">Exact path = that one file only. <code>*</code> or <code>**</code> = a pattern matching many files.</p><label>Reason<input type="text" name="reason" placeholder="Why isn't this a real finding?"></label><button type="button" hx-post="/policy/whitelist/add" hx-include="closest form" hx-target="next .wlout" hx-swap="innerHTML">Review</button></form><div class="wlout"></div></details></td>{{end}}</tr>{{end}}</table>{{end}}{{if .Whitelisted}}<h3 class="gw-section-head">Whitelist ({{len .Whitelisted}})</h3><table class="fr"><tr><td class="k">frame</td><td class="k">path</td><td class="k">reason</td>{{if $.AllowEdits}}<td class="k"></td>{{end}}</tr>{{range .Whitelisted}}<tr><td>{{.Frame}}</td><td>{{.Path}}</td><td>{{.Reason}}</td>{{if $.AllowEdits}}<td><form style="display:inline"><input type="hidden" name="repo" value="{{$.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.Path}}"><button type="button" hx-post="/policy/whitelist/remove" hx-include="closest form" hx-target="next .wlrm-out" hx-swap="innerHTML">Remove</button></form><div class="wlrm-out"></div></td>{{end}}</tr>{{end}}</table>{{end}}`))
	template.Must(t.New("results").Parse(`{{if .Notice}}<div class="warn">{{icon "warn"}} {{.Notice}}</div>{{else if .Err}}<div class="warn">stats error: {{.Err}}</div>{{else}}{{if .Warn}}<div class="warn">{{icon "warn"}} {{.Warn}}</div>{{end}}<div class="sub">decisions: {{.TotDecisions}} · accepts: {{.TotAccepts}} · rejects: {{.TotRejects}} · prevented: {{.TotActualStr}} · modeled: {{.TotModelStr}}</div>{{$tab := .ActiveTab}}{{range .Blocks}}<div class="frame">{{template "block-header" .}}{{if eq $tab "recurring"}}{{template "block-recurring" .}}{{else}}{{template "block-timesaved" .}}{{end}}</div>{{end}}{{end}}`))
	template.Must(t.New("content").Parse(`<section>
<h2 class="gw-pagehead">Stats</h2>
<p class="gw-pagedesc">For each repo: decision counts, <b>estimated debugging time saved</b> by catching issues at push time (actual = blocked-and-fixed; modeled = conservative per-tier hours), <b>recurring findings</b> seen multiple times, and the per-repo whitelist.</p>
{{.TabStrip}}
<form>
  <input type="hidden" name="tab" value="{{.ActiveTab}}">
  <select name="repo" hx-get="/stats" hx-target="#stats-results" hx-select="#stats-results" hx-include="[name]">
    <option value="">all repos</option>
    {{$cur := .Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}
  </select>
  <select name="window" hx-get="/stats" hx-target="#stats-results" hx-select="#stats-results" hx-include="[name]">
    {{$w := .Window}}{{range .Windows}}<option value="{{.Value}}"{{if eq .Value $w}} selected{{end}}>{{.Label}}</option>{{end}}
  </select>
</form>
<div id="stats-results">{{template "results" .}}</div>
</section>
<script>function wlScope(i){var s=i.closest('form').querySelector('.wlscope');var v=i.value;var p=/[*?\[]/.test(v)||v.endsWith('/');s.textContent=p?'⚠ PATTERN: matches multiple files':'matches: this one file';s.className='wlscope'+(p?' warn':'');}</script>`))
	return t
}()
