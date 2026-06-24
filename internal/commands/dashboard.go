// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/banner"
	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/linters"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/tasks"
)

// Dashboard serves a local web UI over the findings task-list - the
// "whole-picture" view (production-ready verdict + open findings by frame +
// deferred/resolved). Local-only, foreground (Ctrl-C to stop). Reads the
// ledger (reflects the last `nimblegate check`); the page auto-refreshes.
// `/frames` is a read-only inspection surface over the frame catalogue (what
// each frame catches), cross-linked from the findings. `/api/tasks` returns
// the open tasks as JSON (PR / agent / SaaS seam).
func Dashboard(args []string) int {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	_ = fs.Bool("serve", false, "run the local web UI (the only mode today)")
	port := fs.Int("port", 7878, "port to serve on (localhost only)")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate dashboard: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate dashboard: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		ledger, _ := tasks.Load(root)
		if err := dashboardTmpl.Execute(w, buildDashData(root, ledger)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/frames", func(w http.ResponseWriter, r *http.Request) {
		stdlibFrames, projectFrames, expanded, cfg := loadFrameContext(root)
		if id := r.URL.Query().Get("id"); id != "" {
			// Real frames resolve first; a synthetic linter ID (app-correctness/<name>)
			// that isn't a frame falls through to the linter inspection page.
			if d, ok := buildFrameDetail(id, stdlibFrames, projectFrames, expanded, cfg); ok {
				if err := frameDetailTmpl.Execute(w, d); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			if li, ok := linters.ByID(id, cfg.Linters); ok {
				if err := linterDetailTmpl.Execute(w, li); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
				}
				return
			}
			http.Error(w, "no such frame or linter: "+id, http.StatusNotFound)
			return
		}
		rows := buildFramesList(stdlibFrames, projectFrames, expanded, nil, cfg.FrameOverrides, linters.DescribeEnabled(cfg.Linters), nil)
		data := framesPage{Project: banner.DefaultProjectName(root), Count: len(rows), Rows: rows}
		if err := framesListTmpl.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		ledger, _ := tasks.Load(root)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ledger.OpenTasks())
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	fmt.Printf("nimblegate dashboard: http://localhost:%d  (Ctrl-C to stop)\n", *port)
	fmt.Println("  reflects the last `nimblegate check`; re-run it to update the data.")
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate dashboard: %v\n", err)
		return 2
	}
	return 0
}

// dashData is the rendered view model.
type dashData struct {
	Project   string
	Generated string
	Ready     bool
	Dangerous int
	Advisory  int
	Deferred  int
	Resolved  int
	Groups    []dashGroup
}

type dashGroup struct {
	Frame    string
	Severity string
	Tasks    []*tasks.Task
}

func buildDashData(root string, ledger *tasks.Ledger) dashData {
	open := ledger.OpenTasks()
	dangerous, advisory := splitOpen(open)
	d := dashData{
		Project:   banner.DefaultProjectName(root),
		Generated: time.Now().Format("2006-01-02 15:04:05"),
		Ready:     len(dangerous) == 0,
		Dangerous: len(dangerous),
		Advisory:  len(advisory),
		Deferred:  len(ledger.DeferredTasks()),
		Resolved:  len(ledger.ResolvedTasks()),
		Groups:    groupOpenByFrame(open),
	}
	return d
}

// splitOpen partitions open tasks into dangerous (BLOCK) and advisory (WARN/INFO).
func splitOpen(open []*tasks.Task) (dangerous, advisory []*tasks.Task) {
	for _, t := range open {
		if t.Severity == "BLOCK" {
			dangerous = append(dangerous, t)
		} else {
			advisory = append(advisory, t)
		}
	}
	return dangerous, advisory
}

func groupOpenByFrame(open []*tasks.Task) []dashGroup {
	byFrame := map[string]*dashGroup{}
	for _, t := range open {
		g := byFrame[t.FrameID]
		if g == nil {
			g = &dashGroup{Frame: t.FrameID, Severity: t.Severity}
			byFrame[t.FrameID] = g
		}
		if severityRankCmd(t.Severity) > severityRankCmd(g.Severity) {
			g.Severity = t.Severity
		}
		g.Tasks = append(g.Tasks, t)
	}
	out := make([]dashGroup, 0, len(byFrame))
	for _, g := range byFrame {
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := severityRankCmd(out[i].Severity), severityRankCmd(out[j].Severity)
		if si != sj {
			return si > sj
		}
		return out[i].Frame < out[j].Frame
	})
	return out
}

// --- Frames inspection surface (read-only) -------------------------------

// frameRow is one entry on the /frames list page.
type frameRow struct {
	ID       string
	Category string
	Severity string
	Enabled  bool
	Source   string   // "stdlib" or "project"
	Summary  string   // first line of the frame body
	Tier     int      // 0 when unknown
	Groups   []string // @-prefixed stdlib groups this frame belongs to (sorted)
}

// frameDetail is the /frames?id=<id> view model.
type frameDetail struct {
	ID             string
	Category       string
	Severity       string
	OverriddenFrom string // non-empty when [frames.<id>] severity differs from frontmatter
	Tier           int
	Triggers       string
	Tags           string
	Lifecycle      string
	Enabled        bool
	HasCheck       bool
	Source         string
	SourcePath     string
	Body           string
}

type framesPage struct {
	Project string
	Count   int
	Rows    []frameRow
}

// loadFrameContext loads the stdlib + project frames and the enabled set the
// same way `nimblegate list`/`info` do. Called per request (cheap; reads the
// embedded stdlib FS + the project dir). root is always a project root here.
func loadFrameContext(root string) (stdlibFrames, projectFrames []frames.Frame, expanded []string, cfg config.ProjectConfig) {
	stdlibFrames, _ = stdlib.Load()
	projectFrames, _ = frames.LoadFromDir(paths.AppframesDir(root))
	cfg, _ = config.LoadProject(paths.ConfigPath(root))
	expanded = cfg.Frames.Enabled
	return stdlibFrames, projectFrames, expanded, cfg
}

// buildFramesList builds the sorted catalogue view. Project frames shadow
// stdlib at the same ID; non-gating lifecycles (archived/deprecated/proposed)
// are hidden to match the default `nimblegate list`. Pure (no I/O) so it's
// unit-testable. projectErr feeds frameEnabledInList's outside-a-project
// fallback (nil from the dashboard, which is always in-project).
func buildFramesList(stdlibFrames, projectFrames []frames.Frame, expanded []string, projectErr error, overrides map[string]config.FrameOverride, linterInfos []linters.LinterInfo, groupIndex map[string][]string) []frameRow {
	type entry struct {
		f      frames.Frame
		source string
	}
	byID := map[string]entry{}
	for _, f := range stdlibFrames {
		byID[f.ID()] = entry{f, "stdlib"}
	}
	for _, f := range projectFrames {
		byID[f.ID()] = entry{f, "project"} // project shadows stdlib at the same ID
	}
	rows := make([]frameRow, 0, len(byID)+len(linterInfos))
	for id, e := range byID {
		if !frames.IsGated(e.f.Frontmatter.EffectiveLifecycle()) {
			continue
		}
		sev := string(e.f.Frontmatter.Severity)
		if ov, ok := overrides[id]; ok && ov.Severity != "" {
			sev = ov.Severity
		}
		rows = append(rows, frameRow{
			ID:       id,
			Category: string(e.f.Frontmatter.Category),
			Severity: sev,
			Enabled:  frameEnabledInList(id, expanded, projectErr),
			Source:   e.source,
			Summary:  firstLine(e.f.Body),
			Tier:     e.f.Frontmatter.Tier,
			Groups:   groupIndex[id],
		})
	}
	// Enabled linters carry synthetic app-correctness/<name> IDs and aren't
	// frames, but they run alongside frames and produce findings, so they
	// belong in the catalogue (and their cross-links must resolve).
	for _, li := range linterInfos {
		rows = append(rows, frameRow{
			ID:       li.ID,
			Category: string(frames.CategoryAppCorrectness),
			Severity: li.Severity,
			Enabled:  true,
			Source:   "linter",
			Summary:  linterSummary(li),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Category != rows[j].Category {
			return rows[i].Category < rows[j].Category
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}

// buildFrameDetail resolves one frame by ID (project shadows stdlib) and
// assembles the detail view, applying any [frames.<id>] severity override.
func buildFrameDetail(id string, stdlibFrames, projectFrames []frames.Frame, expanded []string, cfg config.ProjectConfig) (frameDetail, bool) {
	var found frames.Frame
	var source string
	for _, f := range stdlibFrames {
		if f.ID() == id {
			found, source = f, "stdlib"
		}
	}
	for _, f := range projectFrames {
		if f.ID() == id {
			found, source = f, "project"
		}
	}
	if source == "" {
		return frameDetail{}, false
	}
	sev := string(found.Frontmatter.Severity)
	overFrom := ""
	if ov, ok := cfg.FrameOverrides[id]; ok && ov.Severity != "" && ov.Severity != sev {
		overFrom, sev = sev, ov.Severity
	}
	bound := BuiltinCheckFuncs()
	return frameDetail{
		ID:             id,
		Category:       string(found.Frontmatter.Category),
		Severity:       sev,
		OverriddenFrom: overFrom,
		Tier:           found.Frontmatter.EffectiveTier(),
		Triggers:       strings.Join(found.Frontmatter.Triggers, ", "),
		Tags:           strings.Join(found.Frontmatter.Tags, ", "),
		Lifecycle:      string(found.Frontmatter.EffectiveLifecycle()),
		Enabled:        frameEnabledInList(id, expanded, nil),
		HasCheck:       bound[id] != nil,
		Source:         source,
		SourcePath:     found.SourcePath,
		Body:           found.Body,
	}, true
}

// firstLine returns the first non-empty line of a frame body, stripped of
// leading markdown heading markers - a one-line summary for the list page.
func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		return strings.TrimLeft(s, "# ")
	}
	return ""
}

// linterSummary is the one-line catalogue description of an enabled linter.
func linterSummary(li linters.LinterInfo) string {
	kind := "custom linter"
	if li.Builtin {
		kind = "built-in linter"
	}
	switch {
	case li.Dir != "":
		return kind + " · dir " + li.Dir
	case !li.Builtin && li.Command != "":
		return kind + ": " + li.Command
	default:
		return kind
	}
}

// --- Templates -----------------------------------------------------------

// gwRootVars is the semantic palette as CSS custom properties - the single
// source of truth for theming. It MUST be included in every standalone page
// that uses var(--gw-*) references: dashStyle below (the main dashboard pages),
// and the /login + /setup pages in gatewayauth.go (which render outside the
// gw-shell chrome and therefore don't get dashStyle). When a property is added
// here, every consumer picks it up at the next page load; when one is missing
// in a consumer, the browser falls back to its system theme and the page reads
// as the wrong color - that's the bug that hid the /login form's text on dark
// browser themes in early v0.1.0 builds. Names follow purpose, not value, so
// they survive palette tweaks.
const gwRootVars = ` :root{
   /* Accent - single source of truth for the brand-polish pass. */
   --gw-accent:#79c0ff; --gw-submit-bg:#1b6fd6; --gw-submit-bg-hover:#2580f0;
   /* Text - main → fainter */
   --gw-text:#e6e6e6; --gw-text-soft:#cdd; --gw-text-mid:#bbb; --gw-text-muted:#9aa; --gw-text-faint:#888; --gw-text-fainter:#666;
   /* Surfaces (darkest is body, lightest is control) */
   --gw-bg-page:#0f1115; --gw-bg-input:#0b0d11; --gw-bg-panel:#10151c; --gw-bg-control:#161922;
   /* Borders */
   --gw-border-subtle:#1a1d24; --gw-border-soft:#222; --gw-border:#2a2f3a; --gw-border-hover:#3a4150; --gw-border-row:#1c1f27;
   /* Severity - BLOCK / ERROR family (carries meaning, not theming) */
   --gw-block-text:#ff9b9b; --gw-block-bg:#3a1414; --gw-block-border:#7a3030;
   --gw-error-text:#f8d4d4; --gw-error-bg:#3a1d1d; --gw-error-bg-soft:#2a0d0d; --gw-error-border:#c33;
   /* Severity - WARN family */
   --gw-warn-text:#ffd479; --gw-warn-bg:#3a2e14;
   /* Severity - INFO (text reuses accent; INFO has its own bg) */
   --gw-info-bg:#14283a;
   /* Severity - accept / OK (green) */
   --gw-ok-text:#7ee2a8; --gw-ok-text-soft:#cef3da; --gw-ok-bg:#1f4d3a; --gw-ok-bg-soft:#0d2a16; --gw-ok-border:#2a6d5a; --gw-ok-border-soft:#1c5; --gw-ok-accent:#2a6d3e; --gw-ok-accent-hover:#286847; --gw-ok-bg-mid:#1d3a25;
   /* Destructive (action surfaces) */
   --gw-danger-text:#d99; --gw-danger-bg:#7a1f1f; --gw-danger-bg-hover:#9a2828; --gw-danger-border:#4a2a2a; --gw-danger-summary:#ff9999; --gw-danger-marker:#ff6666;
 }`

// dashStyle is the shared dark stylesheet for every dashboard page. The :root
// palette comes from gwRootVars above so standalone pages (/login, /setup) can
// import the same definitions without including the rest of dashStyle.
const dashStyle = `<style>` + gwRootVars + `
 body{font:15px/1.65 system-ui,sans-serif;margin:0;background:var(--gw-bg-page);color:var(--gw-text)}
 header{padding:16px 24px;border-bottom:1px solid var(--gw-border-soft)}
 h1{font-size:16px;margin:0}
 .ver{color:var(--gw-text-fainter);font-size:11px;font-weight:400}
 .sub{color:var(--gw-text-faint);font-size:12px}
 .verdict{margin:16px 24px;padding:14px 18px;border-radius:8px;font-weight:600}
 .ok{background:var(--gw-ok-bg-soft);color:var(--gw-ok-text);border:1px solid var(--gw-ok-border-soft)}
 .bad{background:var(--gw-error-bg-soft);color:var(--gw-block-text);border:1px solid var(--gw-error-border)}
 .counts{margin:0 24px;color:var(--gw-text-muted)}
 .counts b{color:var(--gw-text)}
 section{margin:18px 24px}
 .frame{margin:10px 0;padding:10px 14px;background:var(--gw-bg-control);border-radius:8px}
 .frame h2{font-size:13px;margin:0 0 6px}
 details.frame summary{font-size:13px;font-weight:600;cursor:pointer;list-style-position:inside}
 details.frame[open] summary{margin-bottom:6px}
 .tag{font-size:11px;padding:1px 7px;border-radius:10px;margin-right:8px}
 .BLOCK{background:var(--gw-block-bg);color:var(--gw-block-text)}
 .WARN{background:var(--gw-warn-bg);color:var(--gw-warn-text)}
 .INFO{background:var(--gw-info-bg);color:var(--gw-accent)}
 .hit{color:var(--gw-text-mid);font-size:12px;margin-left:14px;padding:2px 0}
 .loc{color:var(--gw-accent);white-space:nowrap}
 .acc{color:var(--gw-ok-text)}
 .dmsg{color:var(--gw-text-faint);font-size:11px}
 .rej{color:var(--gw-block-text);font-weight:600}
 .fnd{font-size:11px;padding:1px 7px;border-radius:10px;margin-right:6px;background:var(--gw-bg-control);color:var(--gw-text-mid)}
 .fnd.WARN{background:var(--gw-warn-bg);color:var(--gw-warn-text)}
 .fnd.INFO{background:var(--gw-info-bg);color:var(--gw-accent)}
 .fnd.BLOCK,.fnd.ERROR{background:var(--gw-block-bg);color:var(--gw-block-text)}
 .fnd.LOOP{background:rgba(94,155,230,0.18);color:#5e9be6}
 .gw-looprow{margin-top:4px;display:flex;align-items:center;gap:6px;flex-wrap:wrap}
 .gw-loopresetform{display:inline;margin:0}
 .gw-loopreset{font-size:11px;padding:1px 7px;border-radius:10px;border:1px solid var(--gw-border);background:var(--gw-bg-soft);color:var(--gw-text-muted);cursor:pointer;font-family:inherit;line-height:1.4}
 .gw-loopreset:hover{background:var(--gw-bg-hover);color:var(--gw-text)}
 .warn{margin:10px 0;padding:10px 14px;background:var(--gw-warn-bg);border:1px solid var(--gw-warn-text);border-radius:8px;color:var(--gw-warn-text);font-size:13px}
 a{color:var(--gw-accent)}
 table.fr{border-collapse:collapse;width:100%}
 table.fr td{padding:6px 10px;border-bottom:1px solid var(--gw-border-row);vertical-align:top}
 table.fr td.k{color:var(--gw-text-faint);white-space:nowrap;width:1%}
 table.fr tr.off{opacity:.45}
 .meta{color:var(--gw-text-faint);font-size:12px;margin:2px 0 0}
 pre.body{background:var(--gw-bg-input);border:1px solid var(--gw-border-soft);border-radius:8px;padding:14px;overflow:auto;white-space:pre-wrap;font:12px/1.55 ui-monospace,monospace;color:var(--gw-text-soft)}
</style>`

var dashboardTmpl = template.Must(template.New("dash").Parse(
	`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="10">
<title>nimblegate: {{.Project}}</title>` + dashStyle + `</head><body>
<header>
  <h1>nimblegate: {{.Project}}</h1>
  <div class="sub">{{.Generated}} · reflects the last <code>nimblegate check</code> · auto-refresh 10s · <a href="/frames">Frames</a> · <a href="/api/tasks">/api/tasks</a></div>
</header>
{{if .Ready}}
  <div class="verdict ok">✓ production-ready, no open dangerous (BLOCK) findings{{if .Advisory}} · {{.Advisory}} advisory item(s) to review{{end}}</div>
{{else}}
  <div class="verdict bad">⛔ NOT production-ready: {{.Dangerous}} open dangerous (BLOCK) finding(s) must be resolved</div>
{{end}}
<div class="counts"><b>{{.Dangerous}}</b> dangerous · <b>{{.Advisory}}</b> advisory · <b>{{.Deferred}}</b> deferred · <b>{{.Resolved}}</b> resolved</div>
<section>
{{range .Groups}}
  <details class="frame" data-frame="{{.Frame}}"{{if eq .Severity "BLOCK"}} open{{end}}>
    <summary><span class="tag {{.Severity}}">{{.Severity}}</span><a href="/frames?id={{.Frame}}">{{.Frame}}</a>: {{len .Tasks}} open</summary>
    {{range .Tasks}}<div class="hit"><span class="loc">{{.File}}{{if .Line}}:{{.Line}}{{end}}</span>: {{.Label}}{{if .PRRef}} → {{.PRRef}}{{end}}</div>
    {{end}}
  </details>
{{else}}
  <div class="frame">✓ No open tasks, nothing tracked needs fixing.</div>
{{end}}
</section>
<script>
// Remember which frame groups are expanded across the 10s auto-refresh.
(function(){
  var KEY="af-open";
  function load(){try{return JSON.parse(localStorage.getItem(KEY)||"{}")}catch(e){return {}}}
  var saved=load();
  document.querySelectorAll("details[data-frame]").forEach(function(d){
    var id=d.getAttribute("data-frame");
    if(id in saved){d.open=saved[id];}
    d.addEventListener("toggle",function(){
      var s=load(); s[id]=d.open; localStorage.setItem(KEY,JSON.stringify(s));
    });
  });
})();
</script>
</body></html>`))

var framesListTmpl = template.Must(template.New("frames").Parse(
	`<!doctype html><html><head><meta charset="utf-8">
<title>nimblegate frames: {{.Project}}</title>` + dashStyle + `</head><body>
<header>
  <h1>nimblegate: {{.Project}} · frames</h1>
  <div class="sub"><a href="/">← dashboard</a> · {{.Count}} frame(s) · inspection only (read-only)</div>
</header>
<section>
<table class="fr">
{{range .Rows}}
  <tr{{if not .Enabled}} class="off"{{end}}>
    <td class="k"><span class="tag {{.Severity}}">{{.Severity}}</span></td>
    <td><a href="/frames?id={{.ID}}">{{.ID}}</a><div class="meta">{{.Summary}}</div></td>
    <td class="k">{{.Source}}{{if not .Enabled}} · off{{end}}</td>
  </tr>
{{else}}
  <tr><td>No frames loaded.</td></tr>
{{end}}
</table>
</section>
</body></html>`))

var frameDetailTmpl = template.Must(template.New("frame").Parse(
	`<!doctype html><html><head><meta charset="utf-8">
<title>{{.ID}}: nimblegate</title>` + dashStyle + `</head><body>
<header>
  <h1><span class="tag {{.Severity}}">{{.Severity}}</span>{{.ID}}</h1>
  <div class="sub"><a href="/frames">← frames</a> · <a href="/">dashboard</a></div>
</header>
<section>
<table class="fr">
  <tr><td class="k">Category</td><td>{{.Category}}</td></tr>
  <tr><td class="k">Tier</td><td>T{{.Tier}}</td></tr>
  <tr><td class="k">Severity</td><td>{{.Severity}}{{if .OverriddenFrom}} (overridden from {{.OverriddenFrom}}){{end}}</td></tr>
  <tr><td class="k">Triggers</td><td>{{.Triggers}}</td></tr>
  {{if .Tags}}<tr><td class="k">Tags</td><td>{{.Tags}}</td></tr>{{end}}
  <tr><td class="k">Lifecycle</td><td>{{.Lifecycle}}</td></tr>
  <tr><td class="k">Enabled</td><td>{{.Enabled}}{{if not .HasCheck}} · ⚠ no check function bound{{end}}</td></tr>
  <tr><td class="k">Source</td><td>{{.Source}}: {{.SourcePath}}</td></tr>
</table>
<pre class="body">{{.Body}}</pre>
</section>
</body></html>`))

var linterDetailTmpl = template.Must(template.New("linter").Parse(
	`<!doctype html><html><head><meta charset="utf-8">
<title>{{.ID}}: nimblegate linter</title>` + dashStyle + `</head><body>
<header>
  <h1><span class="tag {{.Severity}}">{{.Severity}}</span>{{.ID}}</h1>
  <div class="sub"><a href="/frames">← frames</a> · <a href="/">dashboard</a></div>
</header>
<section>
<p class="meta">This is an external <b>linter</b> nimblegate runs, not a frame; its findings carry the synthetic frame ID above so they flow through the same audit / whitelist / gate pipeline.</p>
<table class="fr">
  <tr><td class="k">Linter</td><td>{{.Name}}{{if .Builtin}} (built-in adapter){{else}} (custom){{end}}</td></tr>
  <tr><td class="k">Severity</td><td>{{.Severity}}</td></tr>
  {{if .Dir}}<tr><td class="k">Runs in</td><td>{{.Dir}}</td></tr>{{end}}
  {{if .Patterns}}<tr><td class="k">Patterns</td><td>{{range $i, $p := .Patterns}}{{if $i}}, {{end}}{{$p}}{{end}}</td></tr>{{end}}
  {{if .Command}}<tr><td class="k">Command</td><td>{{.Command}}{{range .Args}} {{.}}{{end}}</td></tr>{{end}}
  {{if .Disable}}<tr><td class="k">Disabled rules</td><td>{{range $i, $d := .Disable}}{{if $i}}, {{end}}{{$d}}{{end}}</td></tr>{{end}}
</table>
</section>
</body></html>`))
