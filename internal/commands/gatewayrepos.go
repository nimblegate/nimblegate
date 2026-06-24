// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"nimblegate/internal/gateway"
)

// reposPageOpts carries per-request context for the /repos page renderer.
type reposPageOpts struct {
	AllowEdits bool
	CSRFToken  string
	PolicyRoot string
	ReposRoot  string
	Chrome     chromeData
	// ArchivedNotice, when set, renders a "just archived" banner naming the repo
	// (set from the ?archived=<name> param after the archive action redirects
	// back here instead of jumping to /policy). Already validated by the caller.
	ArchivedNotice string
}

// repoRow is one row in the active-repos table.
type repoRow struct {
	Name          string
	UpstreamURL   string
	Enabled       bool
	Observe       bool
	CredentialSet bool
	ProtectedRefs []string
	FrameCount    int
	Issues        []gateway.SkeletonIssue // skeleton verify findings; empty = fully wired
	RelayFailing  bool                    // latest relay outcome for this repo was a failure
}

// renderReposHTTP is the HTTP entry point for GET /repos.
func renderReposHTTP(w http.ResponseWriter, r *http.Request, opts reposPageOpts) {
	var buf bytes.Buffer
	if err := renderReposPage(&buf, opts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{
		Title:     "repos",
		CSRFToken: opts.CSRFToken,
		Chrome:    opts.Chrome,
		Content:   template.HTML(buf.String()),
	})
}

// renderReposPage writes the /repos content fragment to w.
func renderReposPage(w io.Writer, opts reposPageOpts) error {
	activeRepos := listGatewayRepos(opts.PolicyRoot)
	archivedRepos := gateway.ListArchivedRepos(opts.PolicyRoot)

	// Latest relay outcome per repo (ReadEvents returns chronological order, so
	// the last relay-ok/relay-failed wins). Surfaces a silently-failing relay -
	// pushes the gateway accepted but couldn't deliver upstream.
	relayFailing := map[string]bool{}
	if evs, err := gateway.ReadEvents(opts.PolicyRoot, func(e gateway.Event) bool {
		return e.Event == "relay-ok" || e.Event == "relay-failed"
	}); err == nil {
		for _, e := range evs {
			relayFailing[e.Repo] = e.Event == "relay-failed"
		}
	}

	var rows []repoRow
	for _, name := range activeRepos {
		row := repoRow{Name: name, RelayFailing: relayFailing[name]}
		p, err := (gateway.FilePolicyStore{Root: opts.PolicyRoot}).Load(name)
		if err == nil {
			row.UpstreamURL = p.UpstreamURL
			row.Enabled = p.Enabled
			row.Observe = p.Observe
			row.ProtectedRefs = p.ProtectedRefs
		}
		row.CredentialSet = fileExists(filepath.Join(opts.PolicyRoot, name, "credential"))
		cfg, err := readFullConfig(filepath.Join(opts.PolicyRoot, name, "appframes.toml"))
		if err == nil {
			row.FrameCount = len(cfg.Frames.Enabled)
		}
		fp, err := gateway.LoadFramePolicy(opts.PolicyRoot, name)
		if err == nil {
			row.FrameCount = len(fp.Enabled)
		}
		if opts.ReposRoot != "" {
			issues, _ := (gateway.Skeleton{PolicyRoot: opts.PolicyRoot, ReposRoot: opts.ReposRoot}).Verify(name)
			row.Issues = issues
		}
		rows = append(rows, row)
	}

	fmt.Fprint(w, `<section>`)
	fmt.Fprint(w, `<h2 class="gw-pagehead">Repos</h2>`)
	fmt.Fprint(w, `<p class="gw-pagedesc">Register repos and manage their state. Edit a repo&#39;s policy at the <a href="/policy" style="color:var(--gw-accent)">Policy</a> page.</p>`)

	if opts.ArchivedNotice != "" {
		fmt.Fprintf(w, `<div class="gw-justregistered"><strong>%s</strong> archived. Find it in the <b>Archived repos</b> panel below to Restore or Delete permanently.</div>`, htmlEsc(opts.ArchivedNotice))
	}

	// Add new repo - collapsible, open by default when no active repos.
	if opts.AllowEdits {
		openAttr := ""
		if len(activeRepos) == 0 {
			openAttr = " open"
		}
		fmt.Fprintf(w, `<details class="frame gw-add-repo"%s><summary class="gw-section-head">+ Add new repo to gateway</summary>`, openAttr)
		fmt.Fprint(w, reposAddFormInner(opts))
		fmt.Fprint(w, `</details>`)
	}

	// Active repos table.
	if len(rows) > 0 {
		fmt.Fprint(w, `<h2 class="gw-section-head">Active repos</h2>`)
		renderRepoTable(w, rows, opts)
	}

	// Skeleton-verify findings banner - surfaces missing files / broken state
	// across all registered repos so an operator sees what's wrong without
	// having to read the audit log. Empty issues across all rows = render
	// nothing (no banner clutter when everything's wired correctly).
	renderRepoIssuesBanner(w, rows, opts)

	// Add or rotate upstream credential - own section so it isn't on every row.
	// Repo dropdown picks the target; same form handles both first-install and
	// later rotation since the underlying handler is overwrite-or-create.
	if opts.AllowEdits && len(rows) > 0 {
		fmt.Fprint(w, `<details class="frame gw-rotate-credential"><summary class="gw-section-head">Add or rotate upstream credential</summary><p class="sub">Install a PAT / deploy token for a repo that doesn&#39;t have one yet, or replace the stored token. The previous value is gone after submit: no audit-log trail beyond a "credential-update" event.</p>`)
		fmt.Fprintf(w, `<form class="gw-credform" hx-post="/policy/repo/credential" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded"><label>Repo <select name="repo" required>`,
			htmlEsc(opts.CSRFToken))
		for _, r := range rows {
			fmt.Fprintf(w, `<option value="%s">%s</option>`, htmlEsc(r.Name), htmlEsc(r.Name))
		}
		fmt.Fprint(w, `</select></label><label>New credential <input type="password" name="upstream_credential" autocomplete="new-password" required placeholder="PAT / deploy token: overwrites the current credential"></label><button type="submit">Update credential</button><p class="gw-credform-note">Stored mode 0600; previous value gone after submit.</p></form></details>`)
	}

	// Archived repos.
	if len(archivedRepos) > 0 {
		fmt.Fprintf(w, `<details class="frame gw-archived"><summary class="gw-section-head">Archived repos (%d)</summary>`, len(archivedRepos))
		for _, name := range archivedRepos {
			fmt.Fprintf(w, `<div class="gw-archived-row"><span>%s</span>`, htmlEsc(name))
			if opts.AllowEdits {
				fmt.Fprintf(w, `<form hx-post="/policy/repo/restore" hx-headers='{"X-CSRF-Token":"%s"}'><input type="hidden" name="name" value="%s"><button type="submit">Restore</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(name))
				fmt.Fprintf(w, `<form hx-post="/policy/repo/delete" hx-headers='{"X-CSRF-Token":"%s"}' hx-confirm="Permanently delete %s? This removes its bare repo (all git history) and policy/credential from the gateway. The upstream is untouched and other repos are unaffected. This cannot be undone."><input type="hidden" name="name" value="%s"><button type="submit" class="danger">Delete permanently</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(name), htmlEsc(name))
			}
			fmt.Fprint(w, `</div>`)
		}
		fmt.Fprint(w, `</details>`)
	}

	// Empty state.
	if len(rows) == 0 && len(archivedRepos) == 0 {
		fmt.Fprint(w, `<div class="gw-policy-empty"><p class="sub">No repos registered yet. Use the form above.</p></div>`)
	}

	fmt.Fprint(w, `</section>`)
	return nil
}

// reposAddFormInner returns the inner <form> HTML for adding a repo, matching
// the addRepoForm template shape but without the outer <details> wrapper (since
// the /repos page supplies its own collapsible wrapper).
func reposAddFormInner(opts reposPageOpts) string {
	return fmt.Sprintf(`<form hx-post="/policy/repo/add" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded" hx-target="body" hx-swap="outerHTML"><label>Name <input type="text" name="name" required pattern="[a-zA-Z0-9_\-]+" placeholder="my-repo" title="Letters, numbers, hyphens (-) and underscores (_) only. No spaces or other symbols."></label><p class="sub">Letters, numbers, hyphens, underscores only.</p><label>Upstream URL <input type="text" name="upstream" required placeholder="https://github.com/owner/repo.git"></label><p class="sub">The repo's <b>HTTPS</b> clone URL (e.g. <code>https://github.com/owner/repo.git</code>) - not the <code>git@…</code> SSH form. The token below authenticates it; works for private repos too.</p><label>Upstream credential <input type="password" name="upstream_credential" autocomplete="new-password" placeholder="PAT / deploy token (optional, stored mode 0600, never logged)"></label><label>Protected refs <input type="text" name="protected_refs" value="refs/heads/main" placeholder="refs/heads/main refs/heads/release/*" title="space-separated full ref names; bare branch names like &#34;main&#34; are auto-prefixed with refs/heads/. Pattern is path.Match: &#34;release/*&#34; matches one segment, not recursive."></label><fieldset class="gw-status-fieldset"><legend>Status</legend><label><input type="checkbox" name="enabled" value="1" checked> enabled</label><label><input type="checkbox" name="observe" value="1"> observe-only</label></fieldset><p class="gw-add-note">The <b>Core</b> kit (15 catastrophic-prevention frames) is applied automatically. After registering, refine the selection in the Frame selection section at the bottom: apply additional kits (Web app, CF Pages, CF Workers, Security strict) or tick individual frames.</p><button type="submit">Register repo</button><p class="sub">First push will auto-scan and recommend more.</p></form>`,
		htmlEsc(opts.CSRFToken))
}

// renderRepoTable writes the active-repos table rows to w.
func renderRepoTable(w io.Writer, rows []repoRow, opts reposPageOpts) {
	fmt.Fprint(w, `<table class="gw-repos-table"><thead><tr><th>Name</th><th>Upstream</th><th>Status</th><th>Protected refs</th><th>Frames</th><th>Actions</th></tr></thead><tbody>`)
	for _, row := range rows {
		fmt.Fprint(w, `<tr>`)

		// Name - linked to /policy?repo=<name>
		fmt.Fprintf(w, `<td><a href="/policy?repo=%s" style="color:var(--gw-accent)">%s</a></td>`,
			htmlEsc(row.Name), htmlEsc(row.Name))

		// Upstream URL (monospace, truncated)
		fmt.Fprintf(w, `<td class="gw-repos-url">%s</td>`, htmlEsc(row.UpstreamURL))

		// Status badges
		fmt.Fprint(w, `<td>`)
		if row.Enabled {
			fmt.Fprint(w, `<span class="gw-repo-badge on">enabled</span>`)
		} else {
			fmt.Fprint(w, `<span class="gw-repo-badge off">off</span>`)
		}
		if row.Observe {
			fmt.Fprint(w, `<span class="gw-repo-badge observe">observe</span>`)
		}
		if row.RelayFailing {
			fmt.Fprint(w, `<span class="gw-repo-badge" style="background:var(--gw-block-bg,#3a1414);color:var(--gw-block-text,#ff9b9b);border:1px solid var(--gw-block-border,#7a3030)" title="The gateway accepted pushes but the most recent relay to the upstream FAILED - pushes are not reaching your real host. Check the upstream URL (https:// for a PAT) and credential.">relay failing</span>`)
		}
		switch {
		case row.CredentialSet:
			fmt.Fprint(w, `<span class="gw-repo-badge cred">credential set</span>`)
		case isSSHUpstream(row.UpstreamURL):
			// SSH relay uses the gateway's global SSH identity (deploy key
			// or service-account); per-repo credential files are deliberately
			// not needed. Showing "unset" as a warning was misleading -
			// "n/a (SSH)" surfaces the architectural truth.
			fmt.Fprint(w, `<span class="gw-repo-badge cred-na" title="SSH relay: uses the gateway's SSH identity, no per-repo credential needed">credential n/a (SSH)</span>`)
		default:
			fmt.Fprint(w, `<span class="gw-repo-badge cred-unset">credential unset</span>`)
		}
		fmt.Fprint(w, `</td>`)

		// Protected refs
		fmt.Fprint(w, `<td class="gw-repos-refs">`)
		for i, ref := range row.ProtectedRefs {
			if i > 0 {
				fmt.Fprint(w, `<br>`)
			}
			fmt.Fprint(w, htmlEsc(ref))
		}
		fmt.Fprint(w, `</td>`)

		// Frame count
		fmt.Fprintf(w, `<td>%d</td>`, row.FrameCount)

		// Actions - Edit, Sync from upstream (when an upstream is set), Archive.
		// Credential rotation moved out to its own section under the table where
		// the operator picks the repo from a dropdown - rare destructive action
		// doesn't need to clutter every row.
		fmt.Fprint(w, `<td class="gw-repos-actions">`)
		fmt.Fprintf(w, `<a class="gw-repos-action-edit" href="/policy?repo=%s">Edit policy</a>`, htmlEsc(row.Name))
		if opts.AllowEdits {
			// Sync from upstream - always available when an upstream is set, so an
			// operator can (re)mirror the upstream's history on demand: repos
			// registered before auto-mirror existed, or a re-pull after the
			// upstream changed out-of-band. Delegates to the same repair op the
			// seed-pending issue uses.
			if row.UpstreamURL != "" {
				fmt.Fprintf(w, `<form hx-post="/policy/repo/repair" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded" hx-confirm="Mirror %s's branches + tags from the upstream into the gateway? Existing pushes are unaffected."><input type="hidden" name="repo" value="%s"><input type="hidden" name="operation" value="sync-from-upstream"><button type="submit" class="gw-repos-action-sync">Sync from upstream</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(row.Name), htmlEsc(row.Name))
			}
			// Observe ↔ enforce toggle - flip advisory mode in place, no
			// re-registration. Button posts the TARGET state (the opposite of
			// the row's current mode); the badge in the Status column reflects
			// the current one.
			if row.Observe {
				fmt.Fprintf(w, `<form hx-post="/policy/repo/observe" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded" hx-confirm="Switch %s to ENFORCE? Pushes with a policy violation will be rejected."><input type="hidden" name="name" value="%s"><input type="hidden" name="observe" value="0"><button type="submit" class="gw-repos-action-observe" title="Currently observe-only (advisory), switch to enforcing">Switch to enforce</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(row.Name), htmlEsc(row.Name))
			} else {
				fmt.Fprintf(w, `<form hx-post="/policy/repo/observe" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded" hx-confirm="Switch %s to OBSERVE-only? Would-blocks are recorded but the push is relayed anyway."><input type="hidden" name="name" value="%s"><input type="hidden" name="observe" value="1"><button type="submit" class="gw-repos-action-observe" title="Currently enforcing, switch to observe-only (advisory)">Switch to observe</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(row.Name), htmlEsc(row.Name))
			}
			fmt.Fprintf(w, `<form hx-post="/policy/repo/archive" hx-headers='{"X-CSRF-Token":"%s"}' hx-confirm="Archive %s? Files preserved in _repos/; pushes will fail until restored."><input type="hidden" name="name" value="%s"><button type="submit" class="danger gw-repos-action-archive">Archive</button></form>`,
				htmlEsc(opts.CSRFToken), htmlEsc(row.Name), htmlEsc(row.Name))
		}
		fmt.Fprint(w, `</td>`)

		fmt.Fprint(w, `</tr>`)
	}
	fmt.Fprint(w, `</tbody></table>`)
}

// renderRepoIssuesBanner emits a "Issues to address" section listing every
// skeleton-verify finding across all rows, with a one-click Repair button
// for each auto-repairable finding. Renders nothing when all rows are clean
// - answering the user's ask "if click does nothing, tell me what's missing"
// at the page level rather than per-handler.
func renderRepoIssuesBanner(w io.Writer, rows []repoRow, opts reposPageOpts) {
	var total int
	for _, r := range rows {
		total += len(r.Issues)
	}
	if total == 0 {
		return
	}
	fmt.Fprintf(w, `<details class="frame gw-repo-issues" open><summary class="gw-section-head">Issues to address (%d)</summary>`, total)
	fmt.Fprint(w, `<p class="sub">Each entry is a file the gateway expected to find but didn&#39;t. Auto-repair buttons reseed defaults; missing bare repos or credentials need operator action.</p>`)
	fmt.Fprint(w, `<table class="gw-repo-issues-table"><thead><tr><th>Repo</th><th>File</th><th>Severity</th><th>Issue</th><th>Action</th></tr></thead><tbody>`)
	for _, row := range rows {
		for _, iss := range row.Issues {
			fmt.Fprint(w, `<tr>`)
			fmt.Fprintf(w, `<td>%s</td>`, htmlEsc(row.Name))
			fmt.Fprintf(w, `<td><code>%s</code></td>`, htmlEsc(iss.File))
			sevClass := "warn"
			if iss.Severity == gateway.IssueBlocking {
				sevClass = "block"
			}
			fmt.Fprintf(w, `<td><span class="gw-repo-issue-sev %s">%s</span></td>`, sevClass, htmlEsc(string(iss.Severity)))
			fmt.Fprintf(w, `<td>%s<br><span class="sub">%s</span></td>`, htmlEsc(iss.What), htmlEsc(iss.Why))
			fmt.Fprint(w, `<td>`)
			if iss.Repair != "" && opts.AllowEdits {
				btn := "Repair"
				if iss.Repair == "sync-from-upstream" {
					btn = "Sync"
				}
				fmt.Fprintf(w,
					`<form hx-post="/policy/repo/repair" hx-headers='{"X-CSRF-Token":"%s"}' hx-encoding="application/x-www-form-urlencoded"><input type="hidden" name="repo" value="%s"><input type="hidden" name="operation" value="%s"><button type="submit">%s</button></form>`,
					htmlEsc(opts.CSRFToken), htmlEsc(row.Name), htmlEsc(iss.Repair), btn)
			} else {
				fmt.Fprint(w, `<span class="sub">operator action</span>`)
			}
			fmt.Fprint(w, `</td></tr>`)
		}
	}
	fmt.Fprint(w, `</tbody></table></details>`)
}

// fileExists reports whether path exists (used for credential detection).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// isSSHUpstream is a package-local thunk over gateway.IsSSHUpstream so the
// dashboard code reads the same way as before the helper was promoted up
// into the gateway package (where the skeleton verifier also needs it).
func isSSHUpstream(url string) bool { return gateway.IsSSHUpstream(url) }
