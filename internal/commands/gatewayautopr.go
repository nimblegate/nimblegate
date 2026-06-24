// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
	"nimblegate/internal/gwicons"
)

// autoPRData feeds the /auto-pr template. The landing page surfaces the
// notification rail (the "watch the agent fix its own commit" headline
// feature) as a first-class navigation entry - per-repo status table,
// active loops across all repos, recent delivery activity.
//
// Each row's Edit button deep-links to /policy?repo=X (where the actual
// edit form lives - keeps the data source single).
type autoPRData struct {
	Repos          []autoPRRepo
	ActiveLoops    []autoPRLoop
	RecentActivity []autoPREvent
	HasAny         bool
	ActiveTab      string // "dashboard" (default) | "repos" | "activity"
	AllowEdits     bool   // gates the Retry-now button
	CSRFToken      string // for the Retry-now form
}

type autoPRRepo struct {
	Name              string
	NotificationOn    bool
	WebhookConfigured bool
	WebhookURL        string // host-only display, secret never rendered
	QueueDepth        int
	DeadletterCount   int
	LastError         string // most recent delivery error (queue/deadletter), surfaced in-UI
	LastErrorHint     string // actionable one-liner derived from LastError
	Last24hDelivered  int
	Last24hAttempted  int // for the success-rate denominator
	ActiveLoopCount   int
	EditURL           string
}

type autoPRLoop struct {
	Repo         string
	PRNumber     int
	AttemptCount int
	MaxAttempts  int
	CurrentBot   string
	StickyURL    string
	ResetURL     string
}

type autoPREvent struct {
	Time      time.Time
	Repo      string
	Ref       string
	Decision  string // "rejected" | "observed"
	Outcome   string // "delivered" | "deadlettered" | "queued"
	Symbol    string // icon name (warn|notif|pending)
	FrameID   string // first finding's frame_id, for context
	EventLink string
}

func collectAutoPR(policyRoot string, now time.Time) autoPRData {
	d := autoPRData{}

	cutoff := now.Add(-24 * time.Hour)

	// listGatewayRepos globs */gateway.toml, which resolves the activation
	// symlinks each registered repo is (<policyRoot>/<name> -> _repos/<name>).
	// A previous os.ReadDir + DirEntry.IsDir() walk skipped every symlinked repo
	// (IsDir is Lstat-based and false for a symlink), so registered repos never
	// appeared here even though /repos listed them. Reuse the same lister /repos
	// uses so the two pages agree.
	for _, repoName := range listGatewayRepos(policyRoot) {
		row := autoPRRepo{
			Name:    repoName,
			EditURL: "/auto-pr/config?repo=" + repoName,
		}

		// Read gateway.toml to learn whether notification rail is configured.
		policy, err := gateway.FilePolicyStore{Root: policyRoot}.Load(repoName)
		if err == nil && policy.Notification != nil {
			row.NotificationOn = policy.Notification.Enabled
			if policy.Notification.WebhookURL != "" {
				row.WebhookConfigured = true
				row.WebhookURL = displayHost(policy.Notification.WebhookURL)
			}
		}

		// Queue depth + deadletter count.
		queuePath := filepath.Join(policyRoot, repoName, "pr-comment-queue.jsonl")
		queueRecs, _ := notification.ReadQueueRecords(queuePath)
		row.QueueDepth = len(queueRecs)

		dlPath := filepath.Join(policyRoot, repoName, "pr-comment-deadletter.jsonl")
		dlRecs, _ := notification.ReadQueueRecords(dlPath)
		row.DeadletterCount = len(dlRecs)

		// Surface the most recent delivery error in the dashboard so a failing
		// rail is visible here, not only in docker logs. Prefer a current queue
		// error (live), fall back to the deadletter (already exhausted).
		for _, rec := range queueRecs {
			if rec.LastError != "" {
				row.LastError = rec.LastError
			}
		}
		if row.LastError == "" {
			for _, rec := range dlRecs {
				if rec.LastError != "" {
					row.LastError = rec.LastError
				}
			}
		}
		row.LastErrorHint = deliveryErrorHint(row.LastError)

		// 24h delivery stats from audit log.
		auditPath := filepath.Join(policyRoot, repoName, "audit.log")
		row.Last24hDelivered, row.Last24hAttempted = countRecentDeliveries(auditPath, cutoff)

		// Active loops (count + list).
		stateDir := filepath.Join(policyRoot, repoName, "pr-comment-state")
		loops := readActiveLoops(stateDir, repoName)
		row.ActiveLoopCount = len(loops)
		for _, l := range loops {
			d.ActiveLoops = append(d.ActiveLoops, l)
		}

		d.Repos = append(d.Repos, row)

		// Recent activity (rejected pushes with notification status in last 24h).
		events := readRecentNotificationEvents(auditPath, repoName, cutoff, 5)
		d.RecentActivity = append(d.RecentActivity, events...)
	}

	// Sort repos by name (stable for tests + predictable for users).
	sort.Slice(d.Repos, func(i, j int) bool { return d.Repos[i].Name < d.Repos[j].Name })
	// Sort active loops: highest attempt first (most concerning).
	sort.Slice(d.ActiveLoops, func(i, j int) bool { return d.ActiveLoops[i].AttemptCount > d.ActiveLoops[j].AttemptCount })
	// Recent activity: newest first, cap at 10.
	sort.Slice(d.RecentActivity, func(i, j int) bool { return d.RecentActivity[i].Time.After(d.RecentActivity[j].Time) })
	if len(d.RecentActivity) > 10 {
		d.RecentActivity = d.RecentActivity[:10]
	}

	d.HasAny = len(d.Repos) > 0
	return d
}

// deliveryErrorHint turns a raw upstream delivery error into an actionable
// one-liner for the Repos tab. The dominant first-time failure is a 403 because
// the token lacks the PR-comment permission - and the required scopes differ by
// host (verified against GitHub's fine-grained permission table + Gitea's OAuth2
// scopes): GitHub routes PR comments through the Issues API, so commenting needs
// Issues: write, while finding the PR needs Pull requests: read.
func deliveryErrorHint(lastError string) string {
	if lastError == "" {
		return ""
	}
	switch {
	case strings.Contains(lastError, "403"):
		return "The upstream token can't post PR/MR comments (HTTP 403). GitHub: a classic token with the repo scope, or fine-grained with Contents: Read and write + Pull requests: Read + Issues: Read and write (PR comments use the Issues API). Gitea: write:repository + read:repository + write:issue. GitLab: the api scope (full - no narrower scope allows MR comments). Fix the token, then click Retry now."
	case strings.Contains(lastError, "404"):
		return "Upstream returned 404 - repo or PR not found. Check the upstream URL and that an open PR exists for the branch, then Retry now."
	case strings.Contains(lastError, "401"):
		return "Upstream rejected the credential (HTTP 401). The token is wrong or expired - rotate it on the repo, then Retry now."
	default:
		return "Delivery to the upstream failed. Check the upstream URL + token, then click Retry now."
	}
}

// displayHost extracts host[:port] from a URL for safe display (no path, no
// credential). "https://webhook.example.com/path?token=xyz" → "webhook.example.com".
func displayHost(rawURL string) string {
	s := rawURL
	for _, prefix := range []string{"https://", "http://"} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			s = s[len(prefix):]
			break
		}
	}
	for i, c := range s {
		if c == '/' || c == '?' {
			return s[:i]
		}
	}
	return s
}

// countRecentDeliveries scans audit.log and returns (delivered, attempted)
// counts for notification-rail records within the cutoff window. Delivered =
// InlineSucceeded || DeliveredAt non-zero. Attempted = any notification
// record present.
func countRecentDeliveries(auditPath string, cutoff time.Time) (delivered, attempted int) {
	f, err := os.Open(auditPath)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var rec gateway.AuditRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		if rec.Notification == nil {
			continue
		}
		if rec.Time.Before(cutoff) {
			continue
		}
		attempted++
		if rec.Notification.InlineSucceeded || !rec.Notification.DeliveredAt.IsZero() {
			delivered++
		}
	}
	return delivered, attempted
}

// readActiveLoops walks <repo>/pr-comment-state/*.json and returns one
// autoPRLoop per non-exhausted state file.
func readActiveLoops(stateDir, repoName string) []autoPRLoop {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}
	out := []autoPRLoop{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(stateDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s notification.PRState
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		if s.Loop.Exhausted {
			continue // exhausted loops aren't "active" - they're parked for human review
		}
		out = append(out, autoPRLoop{
			Repo:         repoName,
			PRNumber:     s.PRNumber,
			AttemptCount: s.Loop.AttemptCount,
			MaxAttempts:  s.Loop.MaxAttempts,
			CurrentBot:   s.Mention.CurrentBot,
			StickyURL:    s.StickyComment.URL,
			ResetURL:     fmt.Sprintf("/feed/reset-loop?repo=%s&pr=%d", repoName, s.PRNumber),
		})
	}
	return out
}

// readRecentNotificationEvents returns the most recent N audit events that
// have notification status, within the cutoff window.
func readRecentNotificationEvents(auditPath, repoName string, cutoff time.Time, limit int) []autoPREvent {
	f, err := os.Open(auditPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	// Collect records first, then correlate their notification EventIDs against
	// the queue + deadletter files so each event shows its live outcome
	// (delivered / queued / deadlettered) - the audit record itself only stores
	// the EventID at push time.
	var records []gateway.AuditRecord
	dec := json.NewDecoder(f)
	for {
		var rec gateway.AuditRecord
		if err := dec.Decode(&rec); err != nil {
			break
		}
		records = append(records, rec)
	}
	gateway.CorrelateNotificationStatus(filepath.Dir(auditPath), records)

	out := []autoPREvent{}
	for _, rec := range records {
		if rec.Notification == nil {
			continue
		}
		if rec.Time.Before(cutoff) {
			continue
		}
		ev := autoPREvent{
			Time:    rec.Time,
			Repo:    repoName,
			FrameID: firstFrameID(rec.Findings),
		}
		if len(rec.Refs) > 0 {
			ev.Ref = rec.Refs[0]
		}
		// An accepted row carrying a notification is uniquely a resolution (a
		// closed fix-loop); rejects are the not-accepted rows.
		resolved := rec.Accept
		if resolved {
			ev.Decision = "resolved"
		} else {
			ev.Decision = "rejected"
		}
		switch {
		case rec.Notification.Deadlettered:
			ev.Outcome = "deadlettered"
			ev.Symbol = "warn"
		case !rec.Notification.DeliveredAt.IsZero() || rec.Notification.InlineSucceeded:
			if resolved {
				ev.Outcome = "resolved"
				ev.Symbol = "ok"
			} else {
				ev.Outcome = "delivered"
				ev.Symbol = "notif"
			}
		default:
			ev.Outcome = "queued"
			ev.Symbol = "pending"
		}
		ev.EventLink = "/?repo=" + repoName
		out = append(out, ev)
	}
	// Cap per-repo events (the caller aggregates + re-caps globally).
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func firstFrameID(findings []gateway.Finding) string {
	for _, f := range findings {
		if f.ID != "" {
			return f.ID
		}
	}
	return ""
}

var autoPRTmpl = template.Must(template.New("autopr").Funcs(template.FuncMap{"icon": gwicons.HTML}).Parse(`<style>
.autopr-hero{padding:18px 24px;background:linear-gradient(135deg,var(--gw-bg-panel),var(--gw-bg-soft));border:1px solid var(--gw-border);border-radius:8px;margin-bottom:24px}
.autopr-hero h2{margin:0 0 8px;color:var(--gw-text);font-size:18px}
.autopr-hero p{margin:0;color:var(--gw-text-soft);font-size:13px;line-height:1.5}
.autopr-table{width:100%;border-collapse:collapse;margin:8px 0 24px}
.autopr-table th{text-align:left;padding:8px 10px;background:var(--gw-bg-soft);font-size:11px;font-weight:600;color:var(--gw-text-muted);text-transform:uppercase;letter-spacing:.4px;border-bottom:1px solid var(--gw-border)}
.autopr-table td{padding:10px;border-bottom:1px solid var(--gw-border-soft);font-size:13px}
.autopr-table tbody tr:hover{background:var(--gw-bg-hover)}
.autopr-table .num{text-align:right;font-variant-numeric:tabular-nums}
.autopr-pill{display:inline-block;padding:2px 8px;border-radius:10px;font-size:11px;font-weight:600}
.autopr-pill-on{background:#0a3a1b;color:#5ee68e}
.autopr-pill-off{background:var(--gw-bg-soft);color:var(--gw-text-fainter)}
.autopr-pill-deadletter{background:#3a1a0a;color:#e6905e}
.autopr-pill-loop{background:#1a2a3a;color:#5e9be6}
.autopr-pill-queue{background:#2a2a0a;color:#e6d05e}
.autopr-btn{display:inline-block;padding:4px 10px;border:1px solid var(--gw-border);border-radius:4px;color:var(--gw-text-soft);text-decoration:none;font-size:12px;background:var(--gw-bg-soft)}
.autopr-btn:hover{background:var(--gw-bg-hover);color:var(--gw-text)}
.autopr-loops{margin:8px 0 24px}
.autopr-loop{display:flex;align-items:center;gap:12px;padding:10px;background:var(--gw-bg-panel);border:1px solid var(--gw-border);border-radius:6px;margin-bottom:6px}
.autopr-loop .mono{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;color:var(--gw-text-fainter)}
.autopr-filter{margin:8px 0 14px;display:flex;align-items:center;gap:8px}
.autopr-filter input{flex:1;max-width:340px;padding:7px 10px;font-size:13px;font-family:inherit;background:var(--gw-bg-soft);border:1px solid var(--gw-border);border-radius:4px;color:var(--gw-text)}
.autopr-filter input:focus{outline:none;border-color:var(--gw-accent)}
.autopr-filter-count{font-size:12px;color:var(--gw-text-fainter)}
.autopr-activity{margin-top:8px}
.autopr-activity-row{display:flex;gap:12px;padding:6px 0;font-size:12px;border-bottom:1px solid var(--gw-border-soft)}
.autopr-activity-row time{color:var(--gw-text-fainter);min-width:120px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
.autopr-empty{padding:24px;text-align:center;color:var(--gw-text-fainter);background:var(--gw-bg-panel);border:1px dashed var(--gw-border);border-radius:6px}
</style>

<div class="autopr-hero">
<h2>Auto-PR · the agent fix-loop</h2>
<p>When the gateway rejects a push, it posts a structured comment on the upstream PR <strong>and</strong> fires a webhook with the same JSON payload. Agents listening on either rail (Claude Code, Cursor, Copilot, custom CI) see the rejection in machine-parseable form and can fix it themselves, the headline value moment.</p>
</div>

{{if not .HasAny}}
<div class="autopr-empty">
<p><strong>No repos configured yet.</strong></p>
<p>Once you register a repo at <a href="/repos">Repos</a> and enable <code>[notification] enabled = true</code> in its config, it'll show up here with delivery stats + any active fix loops.</p>
</div>
{{else if eq .ActiveTab "repos"}}

<h3 class="gw-section-head">Repos</h3>
<table class="autopr-table">
<thead><tr><th>Repo</th><th>Status</th><th>Webhook</th><th class="num">Queue</th><th class="num">Deadletter</th><th class="num">Active loops</th><th class="num">24h delivered</th><th></th></tr></thead>
<tbody>
{{range .Repos}}
<tr>
<td><strong>{{.Name}}</strong></td>
<td>{{if .NotificationOn}}<span class="autopr-pill autopr-pill-on">enabled</span>{{else}}<span class="autopr-pill autopr-pill-off">off</span>{{end}}</td>
<td>{{if .WebhookConfigured}}<span class="mono" style="font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:11px;color:var(--gw-text-fainter)">{{.WebhookURL}}</span>{{else}}<span style="color:var(--gw-text-fainter)">-</span>{{end}}</td>
<td class="num">{{if gt .QueueDepth 0}}<span class="autopr-pill autopr-pill-queue">{{.QueueDepth}}</span>{{else}}<span style="color:var(--gw-text-fainter)">0</span>{{end}}</td>
<td class="num">{{if gt .DeadletterCount 0}}<span class="autopr-pill autopr-pill-deadletter">{{.DeadletterCount}}</span>{{else}}<span style="color:var(--gw-text-fainter)">0</span>{{end}}</td>
<td class="num">{{if gt .ActiveLoopCount 0}}<span class="autopr-pill autopr-pill-loop">{{.ActiveLoopCount}}</span>{{else}}<span style="color:var(--gw-text-fainter)">0</span>{{end}}</td>
<td class="num">{{.Last24hDelivered}}{{if gt .Last24hAttempted 0}} / {{.Last24hAttempted}}{{end}}</td>
<td><a class="autopr-btn" href="{{.EditURL}}">Edit config</a>{{if and $.AllowEdits (or (gt .QueueDepth 0) (gt .DeadletterCount 0))}} <form hx-post="/auto-pr/retry" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-confirm="Retry pending comment deliveries for {{.Name}}? Resets the retry backoff and re-queues any deadlettered comments so they deliver on the next poll." style="display:inline"><input type="hidden" name="repo" value="{{.Name}}"><button type="submit" class="autopr-btn" title="Reset backoff + requeue deadletter - use after fixing the upstream token">Retry now</button></form>{{end}}</td>
</tr>
{{if .LastError}}<tr><td colspan="8" style="padding:6px 10px;background:var(--gw-error-bg,#3a1d1d);color:var(--gw-error-text,#f8d4d4);font-size:12px;border-bottom:1px solid var(--gw-error-border,#c33)">{{icon "warn"}} delivery failing: <code>{{.LastError}}</code><br>{{.LastErrorHint}}</td></tr>{{end}}
{{end}}
</tbody>
</table>

{{else if eq .ActiveTab "activity"}}

<h3 class="gw-section-head">Recent activity (last 24h)</h3>
{{if .RecentActivity}}
<div class="autopr-activity">
{{range .RecentActivity}}
<div class="autopr-activity-row">
<time>{{.Time.UTC.Format "2006-01-02 15:04"}}Z</time>
<span><strong>{{.Repo}}</strong></span>
<span class="mono">{{.Ref}}</span>
<span>{{icon .Symbol}} {{.Outcome}}</span>
{{if .FrameID}}<span class="mono">{{.FrameID}}</span>{{end}}
<span style="flex:1"></span>
<a class="autopr-btn" href="{{.EventLink}}">View in feed</a>
</div>
{{end}}
</div>
{{else}}
<div class="autopr-empty">
<p>No notification activity in the last 24 hours.</p>
<p style="font-size:12px">Once a rejected push triggers a webhook or PR comment, it'll show up here.</p>
</div>
{{end}}

{{else}}{{/* dashboard (default): Active loops */}}

<h3 class="gw-section-head">Active loops</h3>
<p style="color:var(--gw-text-soft);font-size:13px;margin:0 0 12px">PRs currently mid-fix-loop. Higher attempt counts surface first.</p>
{{if .ActiveLoops}}
<div class="autopr-filter">
<input type="search" id="autopr-loop-filter" placeholder="Filter by repo or PR #" oninput="autoPRFilterLoops(this.value)" autocomplete="off">
<span class="autopr-filter-count" id="autopr-loop-count">{{len .ActiveLoops}} active</span>
</div>
<div class="autopr-loops" id="autopr-loop-list">
{{range .ActiveLoops}}
<div class="autopr-loop" data-repo="{{.Repo}}" data-pr="{{.PRNumber}}">
<span><strong>{{.Repo}}</strong> · PR #{{.PRNumber}}</span>
<span class="autopr-pill autopr-pill-loop">attempt {{.AttemptCount}}/{{.MaxAttempts}}</span>
<span class="mono">{{.CurrentBot}}</span>
<span style="flex:1"></span>
{{if .StickyURL}}<a class="autopr-btn" href="{{.StickyURL}}">View PR</a>{{end}}
<form style="display:inline;margin:0" hx-post="{{.ResetURL}}" hx-confirm="Reset loop state for {{.Repo}} PR #{{.PRNumber}}? This deletes the per-PR state file."><button type="submit" class="autopr-btn">Reset Loop</button></form>
</div>
{{end}}
</div>
<script>
function autoPRFilterLoops(q){
  q=(q||'').toLowerCase().trim();
  var rows=document.querySelectorAll('#autopr-loop-list .autopr-loop');
  var shown=0;
  rows.forEach(function(el){
    var repo=(el.dataset.repo||'').toLowerCase();
    var pr=String(el.dataset.pr||'');
    var match=!q||repo.indexOf(q)!==-1||pr.indexOf(q)!==-1||('#'+pr).indexOf(q)!==-1;
    el.style.display=match?'':'none';
    if(match)shown++;
  });
  var c=document.getElementById('autopr-loop-count');
  if(c)c.textContent=shown+' of '+rows.length+' shown';
}
</script>
{{else}}
<div class="autopr-empty">
<p>No active fix loops right now.</p>
<p style="font-size:12px">When the gateway rejects a push that has an open PR, an entry appears here while the agent attempts to fix.</p>
</div>
{{end}}

{{end}}
`))

// renderAutoPR writes the /auto-pr body fragment into w (no shell chrome).
// Exposed for tests; the HTTP handler wraps the same render in the gateway shell.
func renderAutoPR(w *bytes.Buffer, data autoPRData) error {
	return autoPRTmpl.Execute(w, data)
}

// autoPRTabStrip renders the four-tab header used by all Auto-PR sub-pages.
// activeTab is one of "dashboard" (active loops) | "repos" (per-repo table) |
// "activity" (recent activity feed) | "setup" (per-repo edit form). Inline
// styles keep the component self-contained (the shell embeds them with the
// page body).
func autoPRTabStrip(activeTab, repoQuery string) string {
	cls := func(t string) string {
		if t == activeTab {
			return "autopr-tab active"
		}
		return "autopr-tab"
	}
	setupHref := "/auto-pr/config"
	if repoQuery != "" {
		setupHref += "?repo=" + repoQuery
	}
	return `<style>
.autopr-tabs{display:flex;gap:2px;margin:0 0 18px;border-bottom:1px solid var(--gw-border);padding:0}
.autopr-tab{display:inline-block;padding:10px 18px;color:var(--gw-text-muted);text-decoration:none;font-size:13px;font-weight:500;border-bottom:2px solid transparent;margin-bottom:-1px}
.autopr-tab:hover{color:var(--gw-text)}
.autopr-tab.active{color:var(--gw-accent);border-bottom-color:var(--gw-accent);font-weight:600}
</style>
<nav class="autopr-tabs">
<a href="/auto-pr" class="` + cls("dashboard") + `">Dashboard</a>
<a href="/auto-pr?tab=repos" class="` + cls("repos") + `">Repos</a>
<a href="/auto-pr?tab=activity" class="` + cls("activity") + `">Activity</a>
<a href="` + setupHref + `" class="` + cls("setup") + `">Setup</a>
</nav>`
}

// autoPRHandler is the HTTP handler for /auto-pr (Dashboard tab). Validates
// path, collects cross-repo notification data, renders body, wraps in shell.
func autoPRHandler(policyRoot string, allowEdits bool, csrfToken func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auto-pr" {
			http.NotFound(w, r)
			return
		}
		tab := r.URL.Query().Get("tab")
		if tab != "repos" && tab != "activity" {
			tab = "dashboard"
		}
		data := collectAutoPR(policyRoot, time.Now())
		data.ActiveTab = tab
		data.AllowEdits = allowEdits
		data.CSRFToken = csrfToken()
		var body bytes.Buffer
		body.WriteString(autoPRTabStrip(tab, ""))
		if err := renderAutoPR(&body, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderGwShell(w, gwLayout{
			Title:   "gateway: Auto-PR",
			Chrome:  buildChrome("auto-pr", "", policyRoot),
			Content: template.HTML(body.String()),
		})
	}
}

// autoPRRetryHandler resets the comment-delivery backoff for one repo and
// re-queues any deadlettered records, so a fixed upstream token delivers on the
// next ~5s poll instead of waiting out a multi-hour backoff. This is the
// operator's server-free recovery from a wrong/missing token scope - no editing
// queue files on the box. POST + CSRF + --allow-edits.
func autoPRRetryHandler(policyRoot string, allowEdits bool, csrfToken func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auto-pr/retry" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !allowEdits {
			http.Error(w, "edits disabled", http.StatusForbidden)
			return
		}
		if !csrfOK(r, csrfToken()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		repo := r.FormValue("repo")
		if !validRepoName(repo) || repo == "_repos" {
			http.Error(w, "invalid repo", http.StatusBadRequest)
			return
		}
		queuePath := filepath.Join(policyRoot, repo, "pr-comment-queue.jsonl")
		dlPath := filepath.Join(policyRoot, repo, "pr-comment-deadletter.jsonl")
		// Requeue deadletter first (moves them into the queue), then reset the
		// whole queue's backoff so everything retries on the next poll.
		requeued, _ := notification.RequeueDeadletter(queuePath, dlPath)
		reset, err := notification.ResetQueueBackoff(queuePath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = gateway.AppendEvent(policyRoot, gateway.Event{
			Event: "retry-requested", Repo: repo, OK: true,
			Payload: map[string]any{"reset": reset, "requeued": requeued},
		})
		redirectAfterAction(w, r, "/auto-pr?tab=repos")
	}
}

// autoPRSetupHandler is the HTTP handler for /auto-pr/config (Setup tab).
// Renders the per-repo notification rail edit form. With no ?repo= param,
// shows a repo picker. The form reuses renderNotificationRailSection with
// the panel embed style + return_to=/auto-pr/config so save redirects back.
func autoPRSetupHandler(policyRoot string, allowEdits bool, csrfToken func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auto-pr/config" {
			http.NotFound(w, r)
			return
		}
		repo := r.URL.Query().Get("repo")
		saveErr := r.URL.Query().Get("notifrail_err")
		saveOK := r.URL.Query().Get("notifrail") == "saved"

		var body bytes.Buffer
		body.WriteString(autoPRTabStrip("setup", repo))

		repos := listConfiguredRepos(policyRoot)
		// Active repo dropdown - same shape as /policy. Always rendered, even
		// with no repo selected, so the operator can switch repos without
		// scrolling back up to the topbar.
		body.WriteString(renderAutoPRRepoPicker(repos, repo))

		if repo == "" {
			if len(repos) == 0 {
				body.WriteString(`<section class="frame"><p class="sub">No repos registered yet. <a href="/repos" style="color:var(--gw-accent)">Register a repo</a> first, then return here to configure its notification rail.</p></section>`)
			} else {
				body.WriteString(`<section class="frame"><p class="sub">Select a repo above to edit its notification rail.</p></section>`)
			}
		} else {
			view := loadNotifRailView(policyRoot, repo)
			token := ""
			if csrfToken != nil {
				token = csrfToken()
			}
			renderNotificationRailSectionWith(&body, repo, view, allowEdits, token, saveErr, saveOK, notifRailRenderOpts{
				ReturnTo: "/auto-pr/config",
				Embed:    "panel",
			})
		}

		renderGwShell(w, gwLayout{
			Title:   "gateway: Auto-PR · Setup",
			Chrome:  buildChrome("auto-pr", repo, policyRoot),
			Content: template.HTML(body.String()),
		})
	}
}

// renderAutoPRRepoPicker emits the Active repo section used by the Setup tab.
// Same shape as the /policy page's picker (Editing: <select>), wrapped in a
// frame so it visually matches the rest of the Auto-PR tabs.
func renderAutoPRRepoPicker(repos []string, current string) string {
	var b bytes.Buffer
	b.WriteString(`<section class="frame">`)
	b.WriteString(`<h3 class="gw-section-head">Active repo</h3>`)
	b.WriteString(`<div class="gw-repo-picker">`)
	if len(repos) == 0 {
		b.WriteString(`<p class="sub">No repos registered. <a href="/repos" style="color:var(--gw-accent)">Register one in Repos</a></p>`)
	} else {
		b.WriteString(`<form style="display:inline"><label for="gw-autopr-repo" style="font-size:12px;color:var(--gw-text-muted);margin-right:6px">Editing:</label>`)
		b.WriteString(`<select name="repo" id="gw-autopr-repo" onchange="if(this.value===''){window.location='/auto-pr/config'}else if(this.value==='__repos'){window.location='/repos'}else{window.location='/auto-pr/config?repo='+encodeURIComponent(this.value)}">`)
		if current == "" {
			b.WriteString(`<option value="" selected>- pick a repo -</option>`)
		}
		for _, r := range repos {
			sel := ""
			if r == current {
				sel = " selected"
			}
			fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, html.EscapeString(r), sel, html.EscapeString(r))
		}
		b.WriteString(`<option value="__repos">- Manage repos →</option>`)
		b.WriteString(`</select></form>`)
	}
	b.WriteString(`</div>`)
	if current != "" {
		fmt.Fprintf(&b, `<p class="sub" style="margin-top:8px">Selected: <strong>%s</strong></p>`, html.EscapeString(current))
	}
	b.WriteString(`</section>`)
	return b.String()
}

// listConfiguredRepos returns the registered repos under policyRoot, sorted
// alphabetically. Delegates to listGatewayRepos (globs */gateway.toml) so it
// resolves the activation symlinks - a prior os.ReadDir + DirEntry.IsDir() walk
// skipped every symlinked repo, leaving the Setup tab's repo dropdown empty.
func listConfiguredRepos(policyRoot string) []string {
	return listGatewayRepos(policyRoot)
}
