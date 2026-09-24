// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"

	"nimblegate/internal/gateway"
)

// commitLinker turns (repo, sha) into the upstream's commit page. It loads the
// host mapping once and each repo's upstream URL on first use, so one page
// render reads each gateway.toml at most once.
type commitLinker struct {
	policyRoot string
	links      gateway.WebLinks
	upstream   map[string]string
}

func newCommitLinker(policyRoot string) *commitLinker {
	links, _ := gateway.LoadWebLinks(policyRoot)
	return &commitLinker{policyRoot: policyRoot, links: links, upstream: map[string]string{}}
}

// URL returns the commit page, or "" when none can be worked out (the chip
// then falls back to click-to-copy).
func (c *commitLinker) URL(repo, sha string) string {
	up, ok := c.upstream[repo]
	if !ok {
		if p, err := (gateway.FilePolicyStore{Root: c.policyRoot}).Load(repo); err == nil {
			up = p.UpstreamURL
		}
		c.upstream[repo] = up
	}
	return gateway.CommitURL(up, sha, c.links)
}

func applyCommitLinks(vm *gateway.ViewModel, c *commitLinker) {
	for i := range vm.Rows {
		for j := range vm.Rows[i].RefDisplays {
			rd := &vm.Rows[i].RefDisplays[j]
			rd.URL = c.URL(vm.Rows[i].Repo, rd.SHA)
		}
	}
}

// renderWebLinksTab is the Settings "Commit links" tab: the host mapping
// editor plus a per-repo preview of the link each upstream produces.
func renderWebLinksTab(policyRoot string, allowEdits bool, csrfToken, saveErr string, saved bool) string {
	var b strings.Builder
	links, err := gateway.LoadWebLinks(policyRoot)
	b.WriteString(`<section class="frame"><h3 class="gw-section-head">Commit links</h3>`)
	b.WriteString(`<p class="sub">Short commit SHAs on the Feed and Overlaps pages link to the commit on your git host. An <code>https://</code> upstream, or an SSH upstream on github.com, gitlab.com, bitbucket.org or codeberg.org, links on its own. For anything else - typically an SSH upstream to a self-hosted Gitea whose web UI is on another port - add one line per git host: <code>&lt;host&gt; &lt;web-url&gt; [gitlab|bitbucket]</code>. Without a link, clicking a SHA copies it.</p>`)
	if err != nil {
		fmt.Fprintf(&b, `<div class="warn">Could not read weblinks.toml: %s</div>`, html.EscapeString(err.Error()))
	}
	if saveErr != "" {
		fmt.Fprintf(&b, `<div class="warn" data-weblinks-error="1">%s</div>`, html.EscapeString(saveErr))
	} else if saved {
		b.WriteString(`<div class="ok" data-weblinks-saved="1">Saved.</div>`)
	}
	if allowEdits {
		fmt.Fprintf(&b, `<form class="gw-credform" hx-post="/settings/weblinks" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded">`, html.EscapeString(csrfToken))
		fmt.Fprintf(&b, `<label>Host mappings<textarea name="hosts" rows="5" placeholder="192.168.1.20 http://192.168.1.20:3000">%s</textarea></label>`, html.EscapeString(links.Lines()))
		b.WriteString(`<button type="submit">Save</button></form>`)
	} else {
		if len(links.Hosts) == 0 {
			b.WriteString(`<p>No host mappings.</p>`)
		} else {
			fmt.Fprintf(&b, `<pre>%s</pre>`, html.EscapeString(links.Lines()))
		}
		b.WriteString(`<p class="sub">Start the dashboard with --allow-edits to change the mappings here.</p>`)
	}
	b.WriteString(`</section>`)

	b.WriteString(`<section class="frame"><h3 class="gw-section-head">What each repo links to</h3><table class="fr" id="weblinks-preview"><thead><tr><td class="k">repo</td><td class="k">upstream</td><td class="k">commit link</td></tr></thead><tbody>`)
	store := gateway.FilePolicyStore{Root: policyRoot}
	for _, repo := range listGatewayRepos(policyRoot) {
		up := ""
		if p, err := store.Load(repo); err == nil {
			up = p.UpstreamURL
		}
		link := gateway.CommitURL(up, "<sha>", links)
		cell := `<span class="sub">none: click copies the SHA</span>`
		if link != "" {
			cell = `<code>` + html.EscapeString(link) + `</code>`
		}
		fmt.Fprintf(&b, `<tr><td class="k">%s</td><td><code>%s</code></td><td>%s</td></tr>`, html.EscapeString(repo), html.EscapeString(redactURL(up)), cell)
	}
	b.WriteString(`</tbody></table></section>`)
	return b.String()
}

// redactURL drops any userinfo so a credential in an upstream URL is never
// rendered on the page.
func redactURL(s string) string {
	if u, err := url.Parse(s); err == nil && u.User != nil && u.Host != "" {
		u.User = nil
		return u.String()
	}
	return s
}

type webLinksHandlers struct {
	policyRoot string
	token      string
}

// save is POST /settings/weblinks. Form body: `hosts`, the textarea. An
// invalid line saves nothing and returns to the tab with the error.
func (h webLinksHandlers) save(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	hosts, err := gateway.ParseWebHostLines(r.FormValue("hosts"))
	if err != nil {
		redirectAfterAction(w, r, "/settings?tab=links&weblinks_err="+url.QueryEscape(err.Error()))
		return
	}
	if err := gateway.SaveWebLinks(h.policyRoot, gateway.WebLinks{Hosts: hosts}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{Event: "weblinks-update", OK: true, Payload: map[string]any{"hosts": len(hosts)}})
	redirectAfterAction(w, r, "/settings?tab=links&weblinks=saved")
}
