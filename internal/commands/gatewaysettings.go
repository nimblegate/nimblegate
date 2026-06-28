// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"

	"nimblegate/internal/gateway"
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

const aboutStaticHTML = `<section class="frame">
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

// renderAboutTab builds the About tab: the editable (or read-only) commercial
// license attestation block, followed by the static License + Project sections.
// With allowEdits off, the block shows current state only - no form, no hx-post,
// no write-route reference (matches the codebase rule that read-only renders
// emit no write surface).
func renderAboutTab(lic gateway.License, allowEdits bool, csrfToken string) string {
	var b strings.Builder
	b.WriteString(`<section class="frame"><h3 class="gw-section-head">Commercial license</h3>`)
	if allowEdits {
		checked := ""
		if lic.Commercial {
			checked = " checked"
		}
		fmt.Fprintf(&b, `<form class="gw-credform" hx-post="/settings/license" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded">`, html.EscapeString(csrfToken))
		fmt.Fprintf(&b, `<label style="flex-direction:row;align-items:center;gap:6px"><input type="checkbox" name="commercial" value="1"%s> I hold a commercial license</label>`, checked)
		fmt.Fprintf(&b, `<label>Lemon Squeezy order number / reference (optional)<input type="text" name="order_ref" value="%s" placeholder="LS-ORDER-12345"></label>`, html.EscapeString(lic.OrderRef))
		b.WriteString(`<p class="gw-credform-note">This is your own self-reported declaration. It is not validated and contacts no server.</p>`)
		b.WriteString(`<button type="submit">Save</button>`)
		fmt.Fprintf(&b, ` <a class="gw-help-toggle" style="text-decoration:none" href="%s">Get a license</a>`, licenseBuyURL)
		b.WriteString(`</form>`)
	} else {
		if lic.Commercial {
			b.WriteString(`<p>Status: <b>Commercial, licensed</b> (self-reported).</p>`)
			if lic.OrderRef != "" {
				fmt.Fprintf(&b, `<p class="sub">Order reference: %s</p>`, html.EscapeString(lic.OrderRef))
			}
		} else {
			b.WriteString(`<p>Status: <b>Non-commercial use</b>.</p>`)
		}
		b.WriteString(`<p class="sub">Start the dashboard with --allow-edits to record a commercial license attestation here.</p>`)
	}
	b.WriteString(`</section>`)
	b.WriteString(aboutStaticHTML)
	return b.String()
}

// serveSettings renders the Settings page with three tabs:
// System (default - install info), Display (browser prefs), About (license + links).
func serveSettings(policyRoot, reposRoot, authMode string, allowEdits bool, csrfToken string) http.HandlerFunc {
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
			lic, _ := gateway.LoadLicense(policyRoot)
			body.WriteString(renderAboutTab(lic, allowEdits, csrfToken))
		default: // "system"
			info := collectSysInfo(policyRoot, reposRoot, authMode, allowEdits, dashStartTime, time.Now())
			body.WriteString(renderSysInfoSection(info))
		}
		body.WriteString(`</section>`)

		renderGwShell(w, gwLayout{Title: "settings : gateway", CSRFToken: csrfToken, Chrome: buildChrome("settings", repo, policyRoot), Content: template.HTML(body.String())})
	}
}
