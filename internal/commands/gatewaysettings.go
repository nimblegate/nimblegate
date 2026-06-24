// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"time"
)

// settingsTabStrip renders the Settings page tab header. activeTab is
// "system" (default) | "display" | "about". Same CSS class as Auto-PR /
// Policy so all three tabbed pages share a consistent visual.
func settingsTabStrip(activeTab string) string {
	cls := func(t string) string {
		if t == activeTab {
			return "autopr-tab active"
		}
		return "autopr-tab"
	}
	return `<style>
.autopr-tabs{display:flex;gap:2px;margin:18px 0;border-bottom:1px solid var(--gw-border);padding:0}
.autopr-tab{display:inline-block;padding:10px 18px;color:var(--gw-text-muted);text-decoration:none;font-size:13px;font-weight:500;border-bottom:2px solid transparent;margin-bottom:-1px}
.autopr-tab:hover{color:var(--gw-text)}
.autopr-tab.active{color:var(--gw-accent);border-bottom-color:var(--gw-accent);font-weight:600}
</style>
<nav class="autopr-tabs">
<a href="/settings?tab=system" class="` + cls("system") + `">System</a>
<a href="/settings?tab=display" class="` + cls("display") + `">Display</a>
<a href="/settings?tab=about" class="` + cls("about") + `">About</a>
</nav>`
}

const displayPrefsHTML = `<section class="frame">
<h3 class="gw-section-head">Display preferences</h3>
<p class="sub">Browser-only. Resetting your browser data clears these.</p>
<div>
<div class="gw-setrow"><label for="set-sidebar">Sidebar starts</label><select id="set-sidebar" data-setting="gwrail"><option value="expanded">expanded</option><option value="collapsed">collapsed</option></select></div>
<div class="gw-setrow"><label for="set-feed">Feed auto-refresh</label><select id="set-feed" data-setting="gwfeedinterval"><option value="0">Off</option><option value="5">every 5s</option><option value="15">every 15s</option><option value="30">every 30s</option></select></div>
<div class="gw-setrow"><label for="set-tz">Timestamp timezone</label><select id="set-tz" data-setting="gwtz"><option value="local">Local</option><option value="utc">Server (UTC)</option></select></div>
<div class="gw-setrow"><label for="set-color">Timestamp color</label><select id="set-color" data-setting="gwtc"><option value="on">On</option><option value="off">Off</option></select></div>
<div class="gw-setrow"><label for="set-day">Day grouping</label><select id="set-day" data-setting="gwday"><option value="on">On</option><option value="off">Off</option></select></div>
</div>
</section>`

const aboutHTML = `<section class="frame">
<h3 class="gw-section-head">License</h3>
<p>nimblegate is source-available under the <a href="https://github.com/nimblegate/nimblegate/blob/main/LICENSE" target="_blank" rel="noopener" style="color:var(--gw-accent)">PolyForm Noncommercial 1.0.0</a> license. Non-commercial use is free and unrestricted, today and for good.</p>
<p class="sub">Commercial use requires a commercial license - <a href="https://github.com/nimblegate/nimblegate/blob/main/COMMERCIAL.md" target="_blank" rel="noopener" style="color:var(--gw-accent)">$99/year per company</a>. Larger orgs: email <a href="mailto:contact@nimblegate.com" style="color:var(--gw-accent)">contact@nimblegate.com</a>.</p>
</section>

<section class="frame">
<h3 class="gw-section-head">Project</h3>
<table class="gw-sysinfo-table">
<tr><td class="gw-sysinfo-k">Source + releases</td><td class="gw-sysinfo-v"><a href="https://github.com/nimblegate/nimblegate" target="_blank" rel="noopener" style="color:var(--gw-accent)">github.com/nimblegate/nimblegate</a> · <a href="https://github.com/nimblegate/nimblegate/releases" target="_blank" rel="noopener" style="color:var(--gw-accent)">releases</a></td></tr>
<tr><td class="gw-sysinfo-k">Website</td><td class="gw-sysinfo-v"><a href="https://nimblegate.com" target="_blank" rel="noopener" style="color:var(--gw-accent)">nimblegate.com</a></td></tr>
<tr><td class="gw-sysinfo-k">Donations</td><td class="gw-sysinfo-v"><a href="https://github.com/sponsors/nimblegate" target="_blank" rel="noopener" style="color:var(--gw-accent)">github.com/sponsors/nimblegate</a></td></tr>
<tr><td class="gw-sysinfo-k">Security disclosures</td><td class="gw-sysinfo-v"><a href="mailto:security@nimblegate.com" style="color:var(--gw-accent)">security@nimblegate.com</a></td></tr>
<tr><td class="gw-sysinfo-k">Updates</td><td class="gw-sysinfo-v">nimblegate never phones home or checks for updates. Watch the <a href="https://github.com/nimblegate/nimblegate/releases" target="_blank" rel="noopener" style="color:var(--gw-accent)">releases page</a> to know when a new version is out.</td></tr>
</table>
</section>`

// serveSettings renders the Settings page with three tabs:
// System (default - install info), Display (browser prefs), About (license + links).
func serveSettings(policyRoot, reposRoot, authMode string, allowEdits bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			repo = ""
		}
		tab := r.URL.Query().Get("tab")
		if tab != "display" && tab != "about" {
			tab = "system"
		}

		var body bytes.Buffer
		fmt.Fprint(&body, `<section><h2 class="gw-pagehead">Settings</h2>`)
		fmt.Fprint(&body, `<p class="gw-pagedesc">System info about this install, display preferences for this browser, and license + project links. Display preferences are browser-only (localStorage); System and About are read-only.</p>`)
		body.WriteString(settingsTabStrip(tab))

		switch tab {
		case "display":
			body.WriteString(displayPrefsHTML)
		case "about":
			body.WriteString(aboutHTML)
		default: // "system"
			info := collectSysInfo(policyRoot, reposRoot, authMode, allowEdits, dashStartTime, time.Now())
			body.WriteString(renderSysInfoSection(info))
		}
		body.WriteString(`</section>`)

		renderGwShell(w, gwLayout{Title: "settings : gateway", Chrome: buildChrome("settings", repo, policyRoot), Content: template.HTML(body.String())})
	}
}
