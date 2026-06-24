// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"flag"
	"fmt"
	"html"
	"html/template"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nimblegate/internal/auth"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/agentapi"
	"nimblegate/internal/gateway/help"
	"nimblegate/internal/gateway/maintenance"
	"nimblegate/internal/gwicons"
)

// feedSev classifies a feed row into one bucket by its highest finding severity
// (BLOCK/ERROR → BLOCK > WARN > INFO), or "clean" if it has no findings.
func feedSev(fs []gateway.Finding) string {
	hasW, hasI := false, false
	for _, f := range fs {
		switch f.Severity {
		case "BLOCK", "ERROR":
			return "BLOCK"
		case "WARN":
			hasW = true
		case "INFO":
			hasI = true
		}
	}
	if hasW {
		return "WARN"
	}
	if hasI {
		return "INFO"
	}
	return "clean"
}

var feedFuncs = template.FuncMap{"feedSev": feedSev, "icon": gwicons.HTML}

// htmx 2.0.4, vendored (the gateway runs with no node/build).
//
//go:embed static/htmx.min.js
var htmxJS []byte

func serveHtmx(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	_, _ = w.Write(htmxJS)
}

// The brand particle-orb mark (docs/brand/icon.svg), embedded so dashboard
// tabs are findable in a crowded tab strip. Served at /static/favicon.svg
// (public route) and /favicon.ico for clients that ignore the link tag.
//
//go:embed static/favicon.svg
var faviconSVG []byte

func serveFavicon(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(faviconSVG)
}

// Template set: "rows" is shared by the full page and the /feed fragment.
var gwTmpl = func() *template.Template {
	t := template.New("gwdash").Funcs(feedFuncs)
	template.Must(t.New("rows").Parse(`{{if .Rows}}<table class="fr"><colgroup><col class="col-loc"><col class="col-msg"><col class="col-stat"><col class="col-reset"></colgroup><thead><tr><td class="k">time</td><td class="k">location</td><td class="k">status</td><td class="k"></td></tr></thead><tbody>{{range .Rows}}<tr data-feedsev="{{feedSev .Findings}}"><td class="loc"><time class="gw-ts gw-tc-{{.Time.UTC.Hour}}" datetime="{{.Time.UTC.Format "2006-01-02T15:04:05Z"}}">{{.Time.UTC.Format "2006-01-02 15:04:05"}}Z</time></td><td class="gw-msgcell"><span class="gw-repo">{{.Repo}}</span>{{if .RefDisplays}}{{range .RefDisplays}}<button type="button" class="gw-ref" aria-expanded="false" title="show file locations">{{.Name}}{{if .ShortSHA}} <span class="gw-sha" title="commit SHA: same on dev box, gateway, and upstream">{{.ShortSHA}}</span>{{end}}</button>{{end}}{{else}}{{range .Refs}}<button type="button" class="gw-ref" aria-expanded="false" title="show file locations">{{.}}</button>{{end}}{{end}}{{range .Locations}}<span class="gw-rmsg">{{.}}</span>{{end}}{{if and .Messages (not .Locations)}}<span class="gw-rmsg sub">-</span>{{end}}</td><td class="gw-statcell"><span class="gw-stat">{{if .Accept}}<span class="acc">{{icon "accept"}} accept</span>{{else}}<span class="rej">{{icon "reject"}} REJECT</span>{{end}}</span>{{with .NotificationStatus}}<span class="gw-notif gw-notif-{{.Indicator}}" title="{{.Message}}">{{icon .Symbol}} {{.Message}}</span>{{end}}{{if .Findings}}<div class="gw-finds">{{range .Findings}}<span class="gw-find">{{if .Message}}<button type="button" class="fnd {{.Severity}}" aria-expanded="false" title="show rule detail">{{.Severity}} {{.ID}}</button><span class="dmsg">{{.Message}}</span>{{else}}<span class="fnd {{.Severity}}">{{.Severity}} {{.ID}}</span>{{end}}</span>{{end}}</div>{{end}}{{with .ActiveLoop}}<div class="gw-looprow"><span class="fnd LOOP" title="Active fix-loop on PR #{{.PRNumber}}">{{icon "loop"}} {{.AttemptCount}}/{{.MaxAttempts}}{{if .CurrentBot}} {{.CurrentBot}}{{end}}</span></div>{{end}}</td><td class="gw-resetcell">{{with .ActiveLoop}}<form class="gw-loopresetform" method="post" action="{{.ResetURL}}" hx-post="{{.ResetURL}}" hx-confirm="Reset loop state for PR #{{.PRNumber}}? This deletes the per-PR state file."><button type="submit" class="gw-loopreset" title="Reset loop state for PR #{{.PRNumber}}">Reset</button></form>{{end}}</td></tr>{{end}}</tbody></table>{{else}}<div class="frame" style="padding:18px;color:var(--gw-text-muted);background:var(--gw-bg-panel);border:1px dashed var(--gw-border);border-radius:6px"><p style="margin:0 0 8px;color:var(--gw-text-soft)"><b>No decisions yet.</b></p><p style="margin:0 0 4px;font-size:13px">Once you've authorized an SSH key and registered a repo, every push lands here as accept / reject / observed, typically within a second of the push completing.</p><p style="margin:6px 0 0;font-size:12px;color:var(--gw-text-fainter)">Setup: <a href="/ssh-keys" style="color:var(--gw-accent)">/ssh-keys</a> → <a href="/policy" style="color:var(--gw-accent)">/policy</a> → on your dev box <code>git remote set-url origin git@&lt;host&gt;:2222/&lt;repo&gt;.git</code> &amp; <code>git push</code>.</p></div>{{end}}`))
	template.Must(t.New("content").Parse(`{{if .Notice}}<div class="warn">{{icon "warn"}} {{.Notice}}</div>{{end}}
<section>
<h2 class="gw-pagehead">Feed</h2>
<p class="gw-pagedesc">Live gateway decisions: every push since the last container start, newest first.</p>
<p class="gw-pagedesc">Auto-refreshes every 5s by default (configurable on <a href="/settings" style="color:var(--gw-accent)">Settings</a>).</p>
<p class="gw-feed-stats">{{.Summary.Repos}} repo(s) · {{.Summary.Accepts}} accepts · {{.Summary.Rejects}} rejects</p>
{{if .Summary.TopBlock}}<p class="gw-feed-stats">Top block: <b>{{.Summary.TopBlock}}</b> ({{.Summary.TopBlockN}})</p>{{end}}
<form class="gw-filters">
  <select name="repo" hx-get="/feed" hx-target="#feed" hx-include="[name]">
    <option value="">all repos</option>
    {{$cur := .Filter.Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}
  </select>
  <label><input type="checkbox" name="rejects" value="1" hx-get="/feed" hx-target="#feed" hx-include="[name]"{{if .Filter.RejectsOnly}} checked{{end}}> only rejects</label>
  <label><input type="checkbox" name="last100" value="1" hx-get="/feed" hx-target="#feed" hx-include="[name]"{{if .Filter.Last100}} checked{{end}}> last 100</label>
  <input type="search" id="feed-search" class="gw-searchbox" placeholder="filter feed…" aria-label="filter feed"><span id="feed-count" class="sub"></span>
  <span class="gw-statusfilter"><span class="sub">status:</span><span class="gw-sevchips"><button type="button" class="gw-feedchip fnd BLOCK" data-feedsev="BLOCK" aria-pressed="true">BLOCK</button><button type="button" class="gw-feedchip fnd WARN" data-feedsev="WARN" aria-pressed="true">WARN</button><button type="button" class="gw-feedchip fnd INFO" data-feedsev="INFO" aria-pressed="true">INFO</button><button type="button" class="gw-feedchip fnd" data-feedsev="clean" aria-pressed="true">clean</button></span></span>
  <span class="gw-export"><a href="/feed/export?format=jsonl" hx-boost="false" download>Export JSONL</a> · <a href="/feed/export?format=csv" hx-boost="false" download>Export CSV</a></span>
</form>
<div id="feed" hx-get="/feed" hx-trigger="load, every 5s" hx-include="[name]">{{template "rows" .}}</div>
<div id="feed-older">{{template "loadolder" .}}</div>
</section>`))
	template.Must(t.New("feed").Parse(`{{template "rows" .}}`))
	template.Must(t.New("loadolder").Parse(`{{if .Summary.HasMore}}<button class="gw-loadolder" hx-get="/feed?before={{.Summary.OldestTime.UTC.Format "2006-01-02T15:04:05Z07:00"}}{{if .Filter.Repo}}&repo={{.Filter.Repo | urlquery}}{{end}}{{if .Filter.RejectsOnly}}&rejects=1{{end}}{{if .Filter.Last100}}&last100=1{{end}}" hx-target="this" hx-swap="outerHTML">Load older</button>{{end}}`))
	template.Must(t.New("olderpage").Parse(`{{template "rows" .}}{{template "loadolder" .}}`))
	return t
}()

// gwPageData wraps the view model with a config notice so the dashboard page
// can render a loud banner; the embedded ViewModel keeps .Summary/.Rows/.Filter
// available to the templates unchanged. The /feed fragment does not use it.
type gwPageData struct {
	gateway.ViewModel
	Notice string
}

func renderGwPage(w http.ResponseWriter, vm gateway.ViewModel, notice string, chrome chromeData) {
	var buf bytes.Buffer
	if err := gwTmpl.ExecuteTemplate(&buf, "content", gwPageData{ViewModel: vm, Notice: notice}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{Title: "gateway", Chrome: chrome, Content: template.HTML(buf.String())})
}

// policyRootNotice returns a non-empty operator warning when the policy root
// looks misconfigured. The deploy failure mode this guards against is silent: a
// wrong --policy-root resolves to a default that holds no repos, and the UI then
// shows "0 repos" exactly like a normal fresh gateway. Empty string = at least
// one registered repo, all good.
func policyRootNotice(policyRoot string) string {
	if _, err := os.Stat(policyRoot); err != nil {
		return fmt.Sprintf("No gateway config directory at %s. Is --policy-root correct, and is this the gateway box?", policyRoot)
	}
	if len(listGatewayRepos(policyRoot)) == 0 {
		return "No repos registered yet. Visit /repos to register your first one (you'll need an SSH key authorized at /ssh-keys for the push side too)."
	}
	return ""
}

func renderGwFeed(w http.ResponseWriter, vm gateway.ViewModel) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := gwTmpl.ExecuteTemplate(w, "feed", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func filterFromQuery(r *http.Request, limit int) gateway.Filter {
	last100 := r.URL.Query().Get("last100") == "1"
	if last100 && limit > 100 {
		limit = 100
	}
	var before time.Time
	if b := r.URL.Query().Get("before"); b != "" {
		if t, err := time.Parse(time.RFC3339, b); err == nil {
			before = t
		}
	}
	return gateway.Filter{
		Repo:        r.URL.Query().Get("repo"),
		RejectsOnly: r.URL.Query().Get("rejects") == "1",
		Last100:     last100,
		Limit:       limit,
		Before:      before,
	}
}

func randToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func gatewayDashboard(args []string) int {
	fs := flag.NewFlagSet("gateway dashboard", flag.ExitOnError)
	_ = fs.Bool("serve", false, "run the web UI (the only mode)")
	port := fs.Int("port", 7900, "port to serve on")
	addr := fs.String("addr", "127.0.0.1", "bind address (127.0.0.1 = localhost only; 0.0.0.0 = LAN)")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root")
	tail := fs.Int("tail", 500, "max decisions read per repo")
	allowEdits := fs.Bool("allow-edits", false, "enable policy tuning (write controls + POST routes)")
	reposRoot := fs.String("repos-root", "", "dir holding the gateway's bare repos (<repo>.git); enables live check preview")
	sshKeysPath := fs.String("ssh-authorized-keys", "/srv/gateway/ssh/authorized_keys", "path to the sshd authorized_keys file the dashboard manages (set empty to hide the Keys page)")
	scopedAccess := fs.Bool("scoped-access", false, "confine each key to its ACL-granted repos. Fail-safe: REFUSES to start if any key would bypass scoping. Run 'gateway access migrate' first")
	authModeFlag := fs.String("auth", "setup-token", "auth mode: 'setup-token' (default; single-admin bcrypt auth, bootstrap via setup token) or 'off' (skip middleware, only safe when fronted by a reverse proxy that authenticates)")
	authDBPath := fs.String("auth-db", "", "path to the SQLite DB backing single-admin auth (default: <policy-root>/_auth.db)")
	authSessionTTL := fs.Duration("auth-session-ttl", 12*time.Hour, "session cookie lifetime")
	agentExcerpts := fs.Bool("agent-api-excerpts", false, "include finding excerpt text in agent API receipts (default: redacted)")
	_ = fs.Parse(args)

	// Fail-safe: refuse --scoped-access while any key would bypass scoping
	// (a plain/non-forced-command key still reaches every repo via git-shell).
	// This closes the migrate footgun - you can't silently run scoped mode with
	// bypass-able keys present.
	if err := scopedAccessGuard(*scopedAccess, *sshKeysPath); err != nil {
		fmt.Fprintf(os.Stderr, "gateway dashboard: REFUSING --scoped-access: %v\n", err)
		return 1
	}

	token := ""
	if *allowEdits {
		token = randToken()
	}

	// Record a build-update event when the binary on disk differs from the
	// SHA captured at the last start - gives operators a single place to see
	// "the dashboard's running code changed at <time>" without diving into the
	// systemd journal. First start writes the marker silently; same-SHA
	// restarts are no-ops.
	emitBuildUpdateEventIfChanged(*policyRoot)

	load := func(r *http.Request) gateway.ViewModel {
		f := filterFromQuery(r, *tail)
		vm := gateway.BuildView(gateway.ReadDecisionsBefore(*policyRoot, f.Before, *tail+1), f) // +1: read one past the display limit so BuildView.HasMore can detect an older page exists
		// Enrich rows with the newest active loop per repo, so the operator
		// can spot stuck PR loops in the feed and reset them inline (spec §6.2).
		applyActiveLoops(&vm, loadActiveLoopsByRepo(*policyRoot))
		return vm
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			repo = ""
		}
		renderGwPage(w, load(r), policyRootNotice(*policyRoot), buildChrome("feed", repo, *policyRoot))
	})
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		vm := load(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		name := "feed"
		if r.URL.Query().Get("before") != "" {
			name = "olderpage"
		}
		if err := gwTmpl.ExecuteTemplate(w, name, vm); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	mux.HandleFunc("/feed/export", feedExportHandler(*policyRoot))
	mux.HandleFunc("/feed/reset-loop", feedResetLoopHandler(*policyRoot, token))
	mux.HandleFunc("/health", healthHandler(*policyRoot, *reposRoot))
	mux.HandleFunc("/auto-pr", autoPRHandler(*policyRoot, *allowEdits, func() string { return token }))
	mux.HandleFunc("/auto-pr/config", autoPRSetupHandler(*policyRoot, *allowEdits, func() string { return token }))
	mux.HandleFunc("/auto-pr/retry", autoPRRetryHandler(*policyRoot, *allowEdits, func() string { return token }))
	mux.HandleFunc("/frames", serveGatewayFrames(*policyRoot))
	mux.HandleFunc("/events", serveGatewayEvents(*policyRoot))
	mux.HandleFunc("/settings", serveSettings(*policyRoot, *reposRoot, *authModeFlag, *allowEdits))
	mux.HandleFunc("/help", help.Handler())
	mux.HandleFunc("/static/htmx.min.js", serveHtmx)
	mux.HandleFunc("/static/gwshell.js", serveGwShellJS)
	mux.HandleFunc("/static/favicon.svg", serveFavicon)
	mux.HandleFunc("/favicon.ico", serveFavicon)

	// Agent API (REST + MCP). Verify is wired after the auth bootstrap below:
	// a nil store (--auth=off) leaves the agent API disabled (503), since
	// bearer tokens live in the auth DB.
	agentSvc := &agentapi.Service{
		PolicyRoot:     *policyRoot,
		ExposeExcerpts: *agentExcerpts,
		ReposRoot:      *reposRoot,
	}
	mux.Handle("/api/v1/", agentSvc.RESTHandler())
	mux.Handle("/mcp", agentSvc.MCPHandler())

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		serveStats(w, r, *policyRoot, *allowEdits, token)
	})

	mux.HandleFunc("/reports", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reports" {
			http.NotFound(w, r)
			return
		}
		serveReports(w, r, *policyRoot)
	})
	mux.HandleFunc("/reports/run", func(w http.ResponseWriter, r *http.Request) {
		serveReportRun(w, r, agentSvc)
	})

	var sshKeys *sshKeyHandlers
	if *sshKeysPath != "" {
		selfExe, _ := os.Executable()
		sshKeys = &sshKeyHandlers{
			keysPath:   *sshKeysPath,
			token:      token,
			scoped:     *scopedAccess,
			exe:        selfExe,
			policyRoot: *policyRoot,
			reposRoot:  *reposRoot,
		}
		mux.HandleFunc("/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
			sshKeys.list(w, r, *allowEdits, *policyRoot)
		})
	}
	mux.HandleFunc("/repos", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos" {
			http.NotFound(w, r)
			return
		}
		archived := r.URL.Query().Get("archived")
		if !validRepoName(archived) {
			archived = "" // ignore anything that isn't a real repo name (no injected text)
		}
		renderReposHTTP(w, r, reposPageOpts{
			AllowEdits:     *allowEdits,
			CSRFToken:      token,
			PolicyRoot:     *policyRoot,
			ReposRoot:      *reposRoot,
			Chrome:         buildChrome("repos", "", *policyRoot),
			ArchivedNotice: archived,
		})
	})
	mux.HandleFunc("/policy", func(w http.ResponseWriter, r *http.Request) {
		repos := listGatewayRepos(*policyRoot)
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			// Reject traversal/invalid names at the HTTP layer; don't pass into path joins.
			repo = ""
		}
		if repo == "" && len(repos) > 0 {
			repo = repos[0]
		}
		justRegistered := ""
		if r.URL.Query().Get("registered") == "1" {
			justRegistered = repo
		}
		fp, _ := gateway.LoadFramePolicy(*policyRoot, repo)
		vm := buildPolicyView(*policyRoot, repo, fp.Enabled)
		authoring := template.HTML("")
		notifRail := template.HTML("")
		if repo != "" {
			lp, _ := gateway.LoadLinterPolicy(*policyRoot, repo)
			authoring = template.HTML(renderAuthoringSection(repo, lp, *allowEdits))
			// Notification rail config moved to Auto-PR · Setup. Leave a pointer
			// here so operators who land on /policy looking for it find the door.
			notifRail = template.HTML(fmt.Sprintf(
				`<section class="frame"><h3 class="gw-section-head">Notification rail · moved</h3>`+
					`<p class="sub">Auto-PR (PR comment + webhook) configuration moved to its own page so it can grow without crowding policy.</p>`+
					`<p><a href="/auto-pr/config?repo=%s" style="color:var(--gw-accent)">→ Configure notification rail for <code>%s</code> on Auto-PR · Setup</a></p>`+
					`</section>`,
				html.EscapeString(repo), html.EscapeString(repo),
			))
		}
		renderPolicyHTTP(w, vm, policyPageOpts{AllowEdits: *allowEdits, CSRFToken: token, Repos: repos, ActiveTab: r.URL.Query().Get("tab"), Authoring: authoring, NotifRail: notifRail, Notice: policyRootNotice(*policyRoot), Chrome: buildChrome("policy", repo, *policyRoot), ScanRec: loadScanRec(*policyRoot, repo), Archived: gateway.ListArchivedRepos(*policyRoot), PolicyRoot: *policyRoot, JustRegistered: justRegistered})
	})
	if *allowEdits {
		ph := policyHandlers{policyRoot: *policyRoot, token: token}
		mux.HandleFunc("/policy/severity", ph.severity)
		mux.HandleFunc("/policy/repo", ph.repo)
		mux.HandleFunc("/policy/frames/toggle", ph.toggleFrame)
		mux.HandleFunc("/policy/kits/apply", ph.applyKit)
		mux.HandleFunc("/policy/kits/clear", ph.clearKit)
		mux.HandleFunc("/policy/kits/new-form", ph.userKitForm)
		mux.HandleFunc("/policy/kits/create", ph.createUserKit)
		mux.HandleFunc("/policy/kits/delete", ph.deleteUserKitHandler)
		mux.HandleFunc("/policy/userkits/clear", ph.clearCustomKit)
		mux.HandleFunc("/policy/category/clear", ph.clearCategory)
		ah := authoringHandlers{policyRoot: *policyRoot, token: token, reposRoot: *reposRoot}
		mux.HandleFunc("/policy/check/preview", ah.preview)
		mux.HandleFunc("/policy/check/add", ah.add)
		mux.HandleFunc("/policy/check/delete", ah.delete)
		mux.HandleFunc("/policy/check/severity", ah.severity)
		mux.HandleFunc("/policy/check/enabled", ah.enabled)
		wh := whitelistHandlers{policyRoot: *policyRoot, token: token}
		mux.HandleFunc("/policy/whitelist/add", wh.add)
		mux.HandleFunc("/policy/whitelist/remove", wh.remove)
		selfExe, _ := os.Executable()
		rl := repoLifecycleHandlers{
			policyRoot: *policyRoot,
			reposRoot:  *reposRoot,
			selfExe:    selfExe,
			token:      token,
		}
		mux.HandleFunc("/policy/repo/add", rl.add)
		mux.HandleFunc("/policy/repo/archive", rl.archive)
		mux.HandleFunc("/policy/repo/observe", rl.observe)
		mux.HandleFunc("/policy/repo/restore", rl.restore)
		mux.HandleFunc("/policy/repo/delete", rl.delete)
		mux.HandleFunc("/policy/repo/settings", rl.settings)
		mux.HandleFunc("/policy/repo/groups", rl.groups)
		mux.HandleFunc("/policy/repo/scan-apply", rl.scanApply)
		mux.HandleFunc("/policy/repo/scan-dismiss", rl.scanDismiss)
		mux.HandleFunc("/policy/repo/scan-rescan", rl.scanRescan)
		mux.HandleFunc("/policy/repo/credential", rl.credential)
		mux.HandleFunc("/policy/repo/repair", rl.repair)

		te := timeEstimatesHandlers{policyRoot: *policyRoot, token: token}
		mux.HandleFunc("/policy/repo/time-estimates", te.update)

		nh := notifRailHandlers{policyRoot: *policyRoot, token: token}
		mux.HandleFunc("/policy/notification/save", nh.save)
		mux.HandleFunc("/policy/notification/generate-secret", nh.generateSecret)

		if sshKeys != nil {
			mux.HandleFunc("/ssh-keys/add", sshKeys.add)
			mux.HandleFunc("/ssh-keys/delete", sshKeys.delete)
			mux.HandleFunc("/ssh-keys/grant", sshKeys.grant)
			mux.HandleFunc("/ssh-keys/revoke", sshKeys.revoke)
		}
	}

	// --- auth bootstrap ---
	var handler http.Handler = mux
	var authStore *auth.Store // captured at outer scope so maintenance loop can use it as SessionSweeper
	mode := authMode(*authModeFlag)
	if mode != authModeSetupToken && mode != authModeOff {
		fmt.Fprintf(os.Stderr, "gateway dashboard: --auth must be 'setup-token' or 'off', got %q\n", *authModeFlag)
		return 2
	}
	authEnabledForChrome = (mode == authModeSetupToken)
	if mode == authModeSetupToken {
		dbPath := *authDBPath
		if dbPath == "" {
			dbPath = filepath.Join(*policyRoot, "_auth.db")
		}
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "gateway dashboard: auth-db dir: %v\n", err)
			return 2
		}
		store, err := auth.Open(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gateway dashboard: open auth DB: %v\n", err)
			return 2
		}
		defer store.Close()
		authStore = store
		agentSvc.Verify = store.VerifyAPIToken
		_ = store.SweepExpiredSessions()
		count, err := store.UserCount()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gateway dashboard: auth user count: %v\n", err)
			return 2
		}
		if count == 0 {
			tok, fresh, err := auth.EnsureSetupToken(*policyRoot)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gateway dashboard: setup token: %v\n", err)
				return 2
			}
			marker := "first-run"
			if !fresh {
				marker = "still-pending"
			}
			fmt.Printf("[nbg-setup] %s setup token: %s, visit /setup to claim\n", marker, tok)
		}
		ah := &authHandlers{
			store:         store,
			policyRoot:    *policyRoot,
			sessionTTL:    *authSessionTTL,
			mode:          mode,
			csrfToken:     token,
			rateLimitHits: map[string][]time.Time{},
		}
		mux.HandleFunc("/login", ah.login)
		mux.HandleFunc("/logout", ah.logout)
		mux.HandleFunc("/setup", ah.setup)
		handler = ah.Middleware(mux)
	}

	if notice := policyRootNotice(*policyRoot); notice != "" {
		fmt.Fprintf(os.Stderr, "\n!! gateway dashboard config warning !!\n!! %s\n!! serving an empty view until this is resolved.\n\n", notice)
	}

	// Maintenance loop - periodic git gc per bare repo. Disabled if no
	// reposRoot configured (preview/admin-only deployments). Config in
	// <policy-root>/gateway.toml [maintenance]; defaults to enabled + 168h.
	maintCfg, err := maintenance.Load(filepath.Join(*policyRoot, "gateway.toml"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway dashboard: maintenance config: %v\n", err)
		return 2
	}
	if *reposRoot != "" && maintCfg.Enabled {
		var sessionSweeper maintenance.SessionSweeper
		if authStore != nil {
			sessionSweeper = authStore
		}
		runner := maintenance.NewRunnerWithTasks(
			maintCfg, *reposRoot, *policyRoot, "/tmp",
			maintenance.ShellGC{},
			sessionSweeper,
			maintenanceEventSink{policyRoot: *policyRoot},
			maintenance.RealClock{},
		)
		go runner.Run(context.Background())
		fmt.Printf("  maintenance: gc + sessions + tmp + deadletter every %s\n", maintCfg.Interval)
		setMaintenanceStatusProvider(runner.Status)
	}

	// Notification rail: drain queued PR-comment + webhook deliveries. The
	// pre-receive hook enqueues on every rejected push; this loop delivers them.
	startNotificationDaemon(context.Background(), *policyRoot)
	fmt.Println(notifDaemonStartLine())

	bind := fmt.Sprintf("%s:%d", *addr, *port)
	fmt.Printf("nimblegate gateway dashboard: http://%s  (Ctrl-C to stop)\n", bind)
	fmt.Printf("  reading decisions from %s/*/audit.log\n", *policyRoot)
	printDashboardAccess(*addr, *port)
	err = http.ListenAndServe(bind, handler)
	fmt.Fprintf(os.Stderr, "gateway dashboard: %v\n", err)
	return 2
}

// printDashboardAccess spells out how to reach the dashboard from where the
// operator actually is. The dashboard is an admin surface, so the default
// loopback bind is deliberately not reachable from other machines - rather
// than leave a remote installer staring at an empty browser, print the on-host
// URL, the SSH-tunnel command (pinned to 127.0.0.1, since the dashboard is
// published on IPv4 and `localhost` may resolve to ::1 first and connect to
// nothing), and the deliberate LAN-exposure opt-in.
func printDashboardAccess(addr string, port int) {
	switch addr {
	case "127.0.0.1", "localhost", "::1":
		host := primaryIPv4()
		if host == "" {
			host = "<this-host>"
		}
		fmt.Printf("  reach it:\n")
		fmt.Printf("    - on this host:            http://localhost:%d\n", port)
		fmt.Printf("    - from another machine:    ssh -L %d:127.0.0.1:%d <user>@%s   then open http://localhost:%d\n", port, port, host, port)
		fmt.Printf("                               (use 127.0.0.1, not localhost - the dashboard is published on IPv4)\n")
		fmt.Printf("    - expose on a trusted LAN: set NIMBLEGATE_DASHBOARD_HOST=0.0.0.0 (admin surface - for the public internet use `nimblegate gateway tls-setup` instead)\n")
	default:
		fmt.Printf("  reachable on all interfaces at :%d - this is an admin surface; keep it on a trusted network or behind a reverse proxy with auth (`nimblegate gateway tls-setup`).\n", port)
		fmt.Printf("    (containerized? reach it via the host's published port from compose.yaml, not the container IP.)\n")
	}
}

// primaryIPv4 returns this host's first non-loopback IPv4 address, or "" if
// none is found. Used only to pre-fill the SSH-tunnel hint above - never to bind.
func primaryIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			if v4 := ipnet.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}

// maintenanceEventSink adapts gateway.AppendEvent to the maintenance.EventSink
// interface so the loop records into the existing JSONL log without the
// maintenance package having to know about the gateway package.
type maintenanceEventSink struct{ policyRoot string }

func (m maintenanceEventSink) Append(name string, payload map[string]any) {
	_ = gateway.AppendEvent(m.policyRoot, gateway.Event{
		Event:   name,
		Payload: payload,
		OK:      true,
	})
}

// maintenanceStatusProvider lets /health read the current Runner status
// without forming a hard dependency. Set once at daemon startup.
var (
	maintenanceStatusMu sync.RWMutex
	maintenanceStatusFn func() maintenance.Status
)

func setMaintenanceStatusProvider(fn func() maintenance.Status) {
	maintenanceStatusMu.Lock()
	defer maintenanceStatusMu.Unlock()
	maintenanceStatusFn = fn
}

func getMaintenanceStatus() (maintenance.Status, bool) {
	maintenanceStatusMu.RLock()
	defer maintenanceStatusMu.RUnlock()
	if maintenanceStatusFn == nil {
		return maintenance.Status{}, false
	}
	return maintenanceStatusFn(), true
}
