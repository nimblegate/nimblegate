// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/maintenance"
	"nimblegate/internal/gateway/notification"
	"nimblegate/internal/gwicons"
)

// dashStartTime is captured at package init so the /health page can render
// "uptime N" for the dashboard daemon line. The dashboard process is a
// long-running server and there's no other entry point that needs a precise
// "moment-of-start" value, so a package-level var read once is fine.
var dashStartTime = time.Now()

// healthData feeds the /health template (spec §9.3). It's a flat shape so the
// template is straightforward to read; aggregation lives in collectHealth.
type healthData struct {
	PID            int
	Uptime         string
	LastPollAgo    string
	DiskFreeStatus string // icon name: "ok" | "warn" | "-" - paired with bytes
	DiskFreeBytes  string
	Repos          []healthRepo
	WebhookSuccess int // 0-100
	CommentSuccess int // 0-100
	HasActivity    bool
	Maintenance    *maintenanceHealth

	// Repo skeleton summary across all registered repos. Populated when
	// reposRoot is non-empty (the verifier needs both roots). Renders as
	// one line in the Service status section; details live on /repos.
	SkeletonChecked     bool // false = couldn't run (no reposRoot configured) → suppress the line
	SkeletonReposTotal  int
	SkeletonReposIssues int // count of repos with ≥1 issue
	SkeletonIssuesTotal int // count of individual issues (a repo may contribute >1)
	SkeletonBlocking    int // subset of SkeletonIssuesTotal at IssueBlocking severity
}

// maintenanceHealth is the /health view of the maintenance loop. nil when
// the loop isn't running (no --repos-root configured, or
// [maintenance].enabled = false). The four tasks ship telemetry through
// the same struct so /health renders them uniformly.
type maintenanceHealth struct {
	Interval     string
	LastSweepAgo string // "never" if zero
	NextSweepIn  string // "-" if next isn't known
	SweepCount   int
	RepoCount    int
	ErrorCount   int
	PerRepo      []maintenanceRepoHealth

	// Task summary lines (shown inline next to "Maintenance:"). Empty if the
	// task hasn't run or wasn't configured.
	SessionSummary    template.HTML
	TmpOrphansSummary template.HTML
	DeadletterSummary template.HTML
}

type maintenanceRepoHealth struct {
	Repo string
	Ago  string
	Took string
	Err  string // empty if no error
}

type healthRepo struct {
	Name            string
	QueueDepth      int
	LastDrainAgo    string // "-" if no delivered notifications on file
	DeadletterCount int
}

// collectHealth walks the policy root and returns a healthData snapshot.
// Pure function over filesystem state - easy to test by seeding a tempdir.
//
// The "last poll" line is approximated from the freshest DeliveredAt across
// every repo - the daemon only writes DeliveredAt on a successful drain, so
// "no delivered records anywhere" reads as "-" rather than a false alarm.
func collectHealth(policyRoot, reposRoot string, startTime time.Time, now time.Time) healthData {
	d := healthData{
		PID:    os.Getpid(),
		Uptime: formatUptime(now.Sub(startTime)),
	}

	// Disk-free: Statfs against the policy root. Failure (missing dir / non-
	// supported FS) renders as "-" so the page still loads on a fresh box.
	var st syscall.Statfs_t
	if err := syscall.Statfs(policyRoot, &st); err != nil {
		d.DiskFreeStatus = "-"
		d.DiskFreeBytes = "unavailable"
	} else {
		free := st.Bavail * uint64(st.Bsize)
		total := st.Blocks * uint64(st.Bsize)
		d.DiskFreeBytes = formatBytes(free)
		// Threshold: <10% free = warn, otherwise ok. Same heuristic as most
		// of the disk-low monitors in the wild; keeps the operator from
		// finding out about a full disk when the gateway starts refusing
		// pushes mid-day.
		if total > 0 && (free*100)/total < 10 {
			d.DiskFreeStatus = "warn"
		} else {
			d.DiskFreeStatus = "ok"
		}
	}

	// Per-repo queue depth + deadletter count + last drain time. Repos are
	// discovered by the same gateway.toml convention used by /repos and
	// /policy so an unregistered dir (e.g. _archive) doesn't appear here.
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	repos := make([]string, 0, len(matches))
	for _, m := range matches {
		repos = append(repos, filepath.Base(filepath.Dir(m)))
	}
	sort.Strings(repos)

	// Skeleton-verify aggregation: walk every registered repo and count
	// issues. Suppressed when reposRoot is empty (the verifier needs the
	// bare-repo path to detect missing bares); legacy test callers can
	// pass "" to skip this without changing existing output expectations.
	if reposRoot != "" {
		d.SkeletonChecked = true
		d.SkeletonReposTotal = len(repos)
		sk := gateway.Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
		for _, repo := range repos {
			issues, err := sk.Verify(repo)
			if err != nil || len(issues) == 0 {
				continue
			}
			d.SkeletonReposIssues++
			d.SkeletonIssuesTotal += len(issues)
			for _, iss := range issues {
				if iss.Severity == gateway.IssueBlocking {
					d.SkeletonBlocking++
				}
			}
		}
	}

	cutoff := now.Add(-24 * time.Hour)
	var (
		webhookAttempts, webhookSuccess int
		commentAttempts, commentSuccess int
		freshestDeliver                 time.Time
	)

	// Read every repo's audit tail once (one glob walk) and bucket records
	// by Repo so each iteration of the per-repo loop just scans its slice.
	auditByRepo := map[string][]gateway.AuditRecord{}
	for _, r := range gateway.ReadDecisions(policyRoot, 500) {
		auditByRepo[r.Repo] = append(auditByRepo[r.Repo], r)
	}

	for _, repo := range repos {
		hr := healthRepo{Name: repo, LastDrainAgo: "-"}

		// Queue depth: every parseable record in pr-comment-queue.jsonl.
		// Reading the queue is fail-soft; a missing file = depth 0.
		qrec, _ := notification.ReadQueueRecords(filepath.Join(policyRoot, repo, "pr-comment-queue.jsonl"))
		hr.QueueDepth = len(qrec)

		// Deadletter count: same shape, same read path, different file.
		dlrec, _ := notification.ReadQueueRecords(filepath.Join(policyRoot, repo, "pr-comment-deadletter.jsonl"))
		hr.DeadletterCount = len(dlrec)

		// Last drain + 24h success rates per repo: derived from the audit
		// log's NotificationStatus.
		var newestDelivered time.Time
		for _, r := range auditByRepo[repo] {
			n := r.Notification
			if n == nil {
				continue
			}
			if n.DeliveredAt.After(newestDelivered) {
				newestDelivered = n.DeliveredAt
			}
			if !r.Time.Before(cutoff) {
				// Webhook delivery is attempted whenever a notification is
				// queued (the rail engaged) - count it as one attempt.
				webhookAttempts++
				if n.InlineSucceeded || !n.DeliveredAt.IsZero() {
					webhookSuccess++
				}
				// PR-comment side: same counter shape, separate so the
				// operator can spot a per-side failure mode (e.g. webhook
				// 100% but comments 0% = upstream API token expired).
				commentAttempts++
				if !n.DeliveredAt.IsZero() && !n.Deadlettered {
					commentSuccess++
				}
			}
		}
		if !newestDelivered.IsZero() {
			hr.LastDrainAgo = relativeFromDelta(now.Sub(newestDelivered), newestDelivered)
			if newestDelivered.After(freshestDeliver) {
				freshestDeliver = newestDelivered
			}
		}
		d.Repos = append(d.Repos, hr)
	}

	if freshestDeliver.IsZero() {
		d.LastPollAgo = "no successful drain yet"
	} else {
		d.LastPollAgo = relativeFromDelta(now.Sub(freshestDeliver), freshestDeliver)
	}

	if webhookAttempts > 0 {
		d.WebhookSuccess = (webhookSuccess * 100) / webhookAttempts
		d.HasActivity = true
	}
	if commentAttempts > 0 {
		d.CommentSuccess = (commentSuccess * 100) / commentAttempts
		d.HasActivity = true
	}

	return d
}

// formatUptime renders a Duration as "Nd Nh Nm" / "Nh Nm" / "Nm Ns" /
// "Ns" - coarse enough to keep the line short, fine enough that a
// just-restarted dashboard reads as fresh.
func formatUptime(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours()/24), int(d.Hours())%24)
}

// formatBytes renders a byte count as "N GB" / "N MB" / "N KB" / "N B".
// Approximate (base-10 divisor) - the operator wants a glance, not a precise
// disk-usage report.
func formatBytes(n uint64) string {
	const (
		kb = 1000
		mb = kb * 1000
		gb = mb * 1000
		tb = gb * 1000
	)
	switch {
	case n >= tb:
		return fmt.Sprintf("%.1f TB", float64(n)/float64(tb))
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(kb))
	}
	return fmt.Sprintf("%d B", n)
}

var healthTmpl = template.Must(template.New("health").Funcs(template.FuncMap{"icon": gwicons.HTML}).Parse(`<style>
.gw-health{display:grid;grid-template-columns:max-content 1fr;gap:6px 18px;margin:0;font-size:13px}
.gw-health dt{color:var(--gw-text-muted);font-weight:500}
.gw-health dd{margin:0;color:var(--gw-text);font-variant-numeric:tabular-nums}
.gw-health-status-ok{color:#5ee68e}
.gw-health-status-warn{color:#e6905e}
.gw-health-rate{display:flex;align-items:center;gap:14px;margin:10px 0;font-size:13px}
.gw-health-rate .label{color:var(--gw-text-muted);min-width:180px}
.gw-health-rate .value{font-weight:600;color:var(--gw-text);font-variant-numeric:tabular-nums}
.gw-health-bar{flex:1;max-width:240px;height:6px;background:var(--gw-bg-soft);border-radius:3px;overflow:hidden}
.gw-health-bar > span{display:block;height:100%;background:var(--gw-accent)}
</style>
<h2 class="gw-pagehead">Health</h2>
<p class="gw-pagedesc">Notification rail telemetry: queue depth, deadletter counts, recent delivery success. Refresh manually; values are computed on each request.</p>

<section class="frame">
<h3 class="gw-section-head">Service status</h3>
<dl class="gw-health">
<dt>Dashboard service</dt><dd><span class="gw-health-status-ok">{{icon "ok"}}</span> running (PID {{.PID}}, uptime {{.Uptime}})</dd>
<dt>Daemon loop</dt><dd><span class="gw-health-status-ok">{{icon "ok"}}</span> running (last successful drain {{.LastPollAgo}})</dd>
<dt>Disk free</dt><dd>{{icon .DiskFreeStatus}} {{.DiskFreeBytes}}</dd>
{{if .SkeletonChecked}}<dt>Repo connection</dt><dd>{{if eq .SkeletonIssuesTotal 0}}<span class="gw-health-status-ok">{{icon "ok"}}</span> all {{.SkeletonReposTotal}} repo(s) connected{{else}}<span class="gw-health-status-warn">{{icon "warn"}}</span> {{.SkeletonIssuesTotal}} issue(s) across {{.SkeletonReposIssues}} repo(s){{if gt .SkeletonBlocking 0}}, {{.SkeletonBlocking}} blocking{{end}}, see <a href="/repos" style="color:var(--gw-accent)">Repos</a> to fix{{end}}</dd>{{end}}
{{with .Maintenance}}
<dt>Maintenance</dt><dd><span class="gw-health-status-ok">{{icon "ok"}}</span> gc every {{.Interval}}, last sweep {{.LastSweepAgo}} ({{.RepoCount}} repo(s){{if gt .ErrorCount 0}}, {{icon "warn"}} {{.ErrorCount}} error(s){{end}}); next in {{.NextSweepIn}}
  {{if .SessionSummary}}<div class="gw-maint-task">{{.SessionSummary}}</div>{{end}}
  {{if .TmpOrphansSummary}}<div class="gw-maint-task">{{.TmpOrphansSummary}}</div>{{end}}
  {{if .DeadletterSummary}}<div class="gw-maint-task">{{.DeadletterSummary}}</div>{{end}}
  {{if .PerRepo}}<details class="gw-maint-details"><summary>per-repo gc</summary>
    <table class="fr">
      <thead><tr><td class="k">Repo</td><td class="k">Ago</td><td class="k">Took</td><td class="k">Status</td></tr></thead>
      <tbody>{{range .PerRepo}}
      <tr><td>{{.Repo}}</td><td>{{.Ago}}</td><td>{{.Took}}</td><td>{{if .Err}}<span class="gw-health-status-warn">{{icon "warn"}}</span> {{.Err}}{{else}}<span class="gw-health-status-ok">{{icon "ok"}}</span>{{end}}</td></tr>
      {{end}}</tbody>
    </table>
  </details>{{end}}</dd>
{{end}}
</dl>
</section>

<section class="frame">
<h3 class="gw-section-head">Notification queue per repo</h3>
{{if .Repos}}
<table class="fr gw-feed">
  <thead><tr><td class="k">Repo</td><td class="k">Queue</td><td class="k">Last drain</td><td class="k">Deadletter</td></tr></thead>
  <tbody>{{range .Repos}}
  <tr><td>Repo: {{.Name}}</td><td>Queue: {{.QueueDepth}}</td><td>{{.LastDrainAgo}}</td><td>Deadletter: {{.DeadletterCount}}{{if gt .DeadletterCount 0}} <button type="button" hx-post="/health/investigate?repo={{.Name}}">Investigate</button>{{end}}</td></tr>
  {{end}}</tbody>
</table>
{{else}}
<p class="sub">No repos registered yet. Register one at <a href="/repos" style="color:var(--gw-accent)">Repos</a>.</p>
{{end}}
</section>

<section class="frame">
<h3 class="gw-section-head">Recent activity (last 24h)</h3>
{{if .HasActivity}}
<div class="gw-health-rate"><span class="label">Webhook delivery success</span><div class="gw-health-bar"><span style="width:{{.WebhookSuccess}}%"></span></div><span class="value">{{.WebhookSuccess}}%</span></div>
<div class="gw-health-rate"><span class="label">PR comment success</span><div class="gw-health-bar"><span style="width:{{.CommentSuccess}}%"></span></div><span class="value">{{.CommentSuccess}}%</span></div>
{{else}}
<p class="sub">No notification-rail activity in the last 24h yet.</p>
{{end}}
</section>
`))

// renderHealth writes the /health body fragment into w (no shell chrome).
// Exposed for tests that exercise the template alone; the HTTP handler wraps
// the same render in the gateway shell.
func renderHealth(w *bytes.Buffer, data healthData) error {
	return healthTmpl.Execute(w, data)
}

// healthHandler is the HTTP handler for /health. Validates path, collects
// health data, renders the body, wraps it in the gateway shell.
// reposRoot may be empty (the page still renders; the Repo skeleton line
// is suppressed).
func healthHandler(policyRoot, reposRoot, sshKeysPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		tab := "status"
		if r.URL.Query().Get("tab") == "diagnostics" {
			tab = "diagnostics"
		}
		var body bytes.Buffer
		body.WriteString(healthTabStrip(tab))
		if tab == "diagnostics" {
			online := r.URL.Query().Get("online") == "1"
			body.WriteString(string(renderHealthDiagnostics(policyRoot, reposRoot, sshKeysPath, r.Host, online)))
		} else {
			now := time.Now()
			data := collectHealth(policyRoot, reposRoot, dashStartTime, now)
			// Pull live maintenance status from the daemon's provider, if running.
			if st, ok := getMaintenanceStatus(); ok {
				data.Maintenance = maintenanceHealthFromStatus(st, now)
			}
			if err := renderHealth(&body, data); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		renderGwShell(w, gwLayout{
			Title:   "gateway: health",
			Chrome:  buildChrome("health", "", policyRoot),
			Content: template.HTML(body.String()),
		})
	}
}

// maintenanceHealthFromStatus converts the runner's Status snapshot into the
// /health view model. Pure - tests pass a synthesized Status without spinning
// up the loop.
func maintenanceHealthFromStatus(st maintenance.Status, now time.Time) *maintenanceHealth {
	mh := &maintenanceHealth{
		SweepCount: st.SweepCount,
		RepoCount:  len(st.PerRepo),
	}
	for _, rr := range st.PerRepo {
		if rr.Err != nil {
			mh.ErrorCount++
		}
		mh.PerRepo = append(mh.PerRepo, maintenanceRepoHealth{
			Repo: rr.Repo,
			Ago:  formatAgo(now.Sub(rr.StartedAt)),
			Took: rr.Took.Truncate(time.Millisecond).String(),
			Err: func() string {
				if rr.Err != nil {
					return rr.Err.Error()
				}
				return ""
			}(),
		})
	}

	// Task 2 - auth sessions
	if !st.LastSessionSweep.Took.IsZero() {
		if st.LastSessionSweep.Err != nil {
			mh.SessionSummary = gwicons.HTML("warn") + template.HTML(" sessions: "+template.HTMLEscapeString(st.LastSessionSweep.Err.Error()))
		} else {
			mh.SessionSummary = "sessions: pruned expired"
		}
	}

	// Task 3 - /tmp/afgw-* orphans
	if !st.LastTmpOrphans.Took.IsZero() {
		if st.LastTmpOrphans.Err != nil {
			mh.TmpOrphansSummary = gwicons.HTML("warn") + template.HTML(" tmp orphans: "+template.HTMLEscapeString(st.LastTmpOrphans.Err.Error()))
		} else if st.LastTmpOrphans.Removed > 0 {
			mh.TmpOrphansSummary = template.HTML(fmt.Sprintf("tmp orphans: removed %d", st.LastTmpOrphans.Removed))
		} else {
			mh.TmpOrphansSummary = "tmp orphans: none found"
		}
	}

	// Task 4 - deadletter retention
	if len(st.LastDeadletter) > 0 {
		var totalPruned, totalKept, dlErrs int
		for _, dr := range st.LastDeadletter {
			totalPruned += dr.Pruned
			totalKept += dr.Kept
			if dr.Err != nil {
				dlErrs++
			}
		}
		if dlErrs > 0 {
			mh.DeadletterSummary = gwicons.HTML("warn") + template.HTML(fmt.Sprintf(" deadletter: %d error(s)", dlErrs))
		} else {
			mh.DeadletterSummary = template.HTML(fmt.Sprintf("deadletter: pruned %d, kept %d", totalPruned, totalKept))
		}
	}
	if st.LastSweepAt.IsZero() {
		mh.LastSweepAgo = "never"
	} else {
		mh.LastSweepAgo = formatAgo(now.Sub(st.LastSweepAt))
	}
	if st.NextSweepAt.IsZero() {
		mh.NextSweepIn = "-"
		mh.Interval = "-"
	} else {
		untilNext := st.NextSweepAt.Sub(now)
		if untilNext < 0 {
			untilNext = 0
		}
		mh.NextSweepIn = formatUptime(untilNext)
		// Interval is the difference between next and last (when last is known);
		// fall back to "-" if we can't compute it.
		if !st.LastSweepAt.IsZero() {
			mh.Interval = st.NextSweepAt.Sub(st.LastSweepAt).String()
		} else {
			mh.Interval = formatUptime(untilNext)
		}
	}
	return mh
}

// formatAgo renders a Duration since some past event as "Nd ago" / "Nh ago" /
// "Nm ago" / "just now". Symmetric with formatUptime but past-tense.
func formatAgo(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
