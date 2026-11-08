// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/kits"
	"nimblegate/internal/whitelist"
)

// repoPreview evaluates the latest pushed code of repo under the candidate
// enabled list, through the same Checker, caps and staging dir the
// pre-receive hook uses.
func repoPreview(policyRoot, reposRoot, repo string, enabled []string) (gateway.PreviewResult, gateway.Policy, error) {
	if reposRoot == "" {
		return gateway.PreviewResult{}, gateway.Policy{}, errors.New("no repos root")
	}
	pol, err := gateway.FilePolicyStore{Root: policyRoot}.Load(repo)
	if err != nil {
		return gateway.PreviewResult{}, gateway.Policy{}, err
	}
	bare := filepath.Join(reposRoot, repo+".git")
	gwCfg, _ := gateway.LoadGatewayConfig(policyRoot)
	res := gateway.PreviewPolicy(gateway.PreviewInput{
		Policy:       pol,
		GitDir:       bare,
		Checker:      engineChecker{},
		Enabled:      enabled,
		ScanTmpDir:   gateway.ScanStagingDir(bare, policyRoot),
		MaxTreeBytes: gwCfg.MaxTreeBytes,
		ScanTimeout:  gwCfg.ScanTimeout,
	})
	return res, pol, nil
}

// nextPushView is the "With this policy, the next push would" section.
type nextPushView struct {
	Repo     string
	Observe  bool
	OOB      bool
	Res      gateway.PreviewResult
	ShortRev string
	Findings previewFindingsView
}

type previewFindingsView struct {
	Repo      string
	Back      string // where whitelist-this returns: "" (policy page), "scan", "kit:<name>"
	Rows      []previewRow
	Total     int
	Truncated bool
}

type previewRow struct {
	Severity, Frame, File, Loc, Label string
	New                               bool // absent from the "before" preview (confirm view only)
	CanWhitelist                      bool
}

func shortRev(rev string) string {
	if len(rev) > 10 {
		return rev[:10]
	}
	return rev
}

func previewRowKey(f gateway.PreviewFinding) string {
	return f.Severity + "\x00" + f.Frame + "\x00" + f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Label
}

// buildFindings lays out res's rows; before, when non-nil, marks rows the
// candidate policy adds.
func buildFindings(repo, back string, res gateway.PreviewResult, before *gateway.PreviewResult, allowEdits bool) previewFindingsView {
	var seen map[string]bool
	if before != nil {
		seen = map[string]bool{}
		for _, f := range append(append([]gateway.PreviewFinding{}, before.Blocks...), before.Warns...) {
			seen[previewRowKey(f)] = true
		}
	}
	v := previewFindingsView{Repo: repo, Back: back, Total: res.BlockCount + res.WarnCount}
	add := func(f gateway.PreviewFinding, blocking bool) {
		loc := f.File
		if loc != "" && f.Line > 0 {
			loc += ":" + strconv.Itoa(f.Line)
		}
		v.Rows = append(v.Rows, previewRow{
			Severity: f.Severity, Frame: f.Frame, File: f.File, Loc: loc, Label: f.Label,
			New:          seen != nil && !seen[previewRowKey(f)],
			CanWhitelist: allowEdits && blocking && f.Frame != gateway.ScanFailedID && validExactPath(f.File),
		})
	}
	for _, f := range res.Blocks {
		add(f, true)
	}
	for _, f := range res.Warns {
		add(f, false)
	}
	v.Truncated = len(v.Rows) < v.Total
	return v
}

// nextPushFor builds the /policy preview section for the stored policy.
func nextPushFor(policyRoot, reposRoot, repo string, enabled []string, allowEdits bool) (nextPushView, bool) {
	res, pol, err := repoPreview(policyRoot, reposRoot, repo, enabled)
	if err != nil {
		return nextPushView{}, false
	}
	return nextPushView{
		Repo: repo, Observe: pol.Observe, Res: res, ShortRev: shortRev(res.Rev),
		Findings: buildFindings(repo, "", res, nil, allowEdits),
	}, true
}

// policyPreviews computes the /policy page's preview section and, when a scan
// recommendation is pending, the preview with it applied.
func policyPreviews(policyRoot, reposRoot, repo string, enabled []string, allowEdits bool, rec *ScanRecommendation) (*nextPushView, *gateway.PreviewResult) {
	if repo == "" {
		return nil, nil
	}
	v, ok := nextPushFor(policyRoot, reposRoot, repo, enabled, allowEdits)
	if !ok {
		return nil, nil
	}
	if rec == nil {
		return &v, nil
	}
	names := make([]string, 0, len(rec.RecommendedGroups))
	for _, g := range rec.RecommendedGroups {
		names = append(names, g.Name)
	}
	after, _, err := mergeRecommended(enabled, names)
	if err != nil {
		return &v, nil
	}
	res, _, err := repoPreview(policyRoot, reposRoot, repo, after)
	if err != nil {
		return &v, nil
	}
	return &v, &res
}

// validExactPath accepts a tree-relative path that a whitelist entry matches as
// exactly that one file: no glob metacharacters, no traversal, no line breaks.
func validExactPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	if strings.Contains(p, "..") {
		return false
	}
	if strings.ContainsAny(p, "*?[]{}\\") {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

type previewHandlers struct {
	policyRoot string
	reposRoot  string
	token      string
}

// whitelistThis adds one exact-path whitelist entry for a finding shown in a
// preview, to the gateway-held whitelist the gate overlays into every scan.
func (h previewHandlers) whitelistThis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo := r.FormValue("repo")
	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	if _, err := (gateway.FilePolicyStore{Root: h.policyRoot}).Load(repo); err != nil {
		http.Error(w, "unknown repo", http.StatusBadRequest)
		return
	}
	frame := r.FormValue("frame")
	if !whitelistKnownIDs(h.policyRoot, repo)[frame] {
		http.Error(w, "unknown frame", http.StatusBadRequest)
		return
	}
	path := r.FormValue("path")
	if !validExactPath(path) {
		http.Error(w, "path must be one exact file path in the repo", http.StatusBadRequest)
		return
	}
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" {
		http.Error(w, "reason required", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(reason, "\r\n") || strings.Contains(reason, "..") {
		http.Error(w, "reason must be one line without ..", http.StatusBadRequest)
		return
	}
	back := previewBackURL(repo, r.FormValue("back"))
	added, err := whitelist.AddEntry(gateway.WhitelistPath(h.policyRoot, repo), whitelist.Entry{Frame: frame, Path: path, Reason: reason})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if added {
		_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
			Event: "whitelist-add", Repo: repo, OK: true,
			Payload: map[string]any{"frame": frame, "path": path, "reason": reason, "via": "preview"},
		})
	}
	redirectAfterAction(w, r, back)
}

// previewBackURL rebuilds the page a whitelist-this form came from. Built
// here from a token, never taken from the request as a URL.
func previewBackURL(repo, back string) string {
	switch {
	case back == "scan":
		return "/policy/repo/scan-preview?name=" + url.QueryEscape(repo)
	case strings.HasPrefix(back, "kit:"):
		if ks, err := kits.LoadStdlib(); err == nil {
			if k, ok := ks.Get(strings.TrimPrefix(back, "kit:")); ok {
				return "/policy/kits/preview?repo=" + url.QueryEscape(repo) + "&name=" + url.QueryEscape(k.Name)
			}
		}
	}
	return "/policy?repo=" + url.QueryEscape(repo)
}

// previewConfirmView is the GET confirm page in front of a kit or
// recommendation apply.
type previewConfirmView struct {
	Repo         string
	Title        string
	Unavailable  string // why no before/after numbers could be produced
	Before       gateway.PreviewResult
	After        gateway.PreviewResult
	ShortRev     string
	Observe      bool
	OfferObserve bool
	Findings     previewFindingsView
	ApplyURL     string
	Fields       []previewField
}

type previewField struct{ Name, Value string }

func previewUnavailable(res gateway.PreviewResult) string {
	switch {
	case res.Disabled:
		return "Gating is off for this repo: the next push is relayed without a check whatever the frame list."
	case res.NoCode:
		return "No pushed code yet, so there is nothing to preview against. The kit applies to the first push."
	case res.NoGatedRef:
		return "No pushed branch matches this repo's protected refs yet, so there is nothing to preview against."
	}
	return ""
}

func (h previewHandlers) renderConfirm(w http.ResponseWriter, repo, title, back, applyURL string, fields []previewField, before, after []string) {
	b, pol, err := repoPreview(h.policyRoot, h.reposRoot, repo, before)
	if err != nil {
		http.Error(w, "unknown repo", http.StatusBadRequest)
		return
	}
	a, _, _ := repoPreview(h.policyRoot, h.reposRoot, repo, after)
	v := previewConfirmView{
		Repo: repo, Title: title, Before: b, After: a, ShortRev: shortRev(a.Rev),
		Observe:      pol.Observe,
		OfferObserve: pol.Enabled && !pol.Observe && a.WouldReject(),
		Unavailable:  previewUnavailable(a),
		Findings:     buildFindings(repo, back, a, &b, true),
		ApplyURL:     applyURL,
		Fields:       append(fields, previewField{"confirm", "1"}),
	}
	var buf bytes.Buffer
	if err := policyTmpl.ExecuteTemplate(&buf, "previewConfirm", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderGwShell(w, gwLayout{Title: "policy - " + repo, CSRFToken: h.token, Chrome: buildChrome("policy", repo, h.policyRoot), Content: template.HTML(buf.String())})
}

// kitConfirm previews the next push with a kit merged into the repo's list.
func (h previewHandlers) kitConfirm(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	ks, err := kits.LoadStdlib()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	k, ok := ks.Get(r.URL.Query().Get("name"))
	if !ok {
		http.Error(w, "unknown kit", http.StatusBadRequest)
		return
	}
	fp, err := gateway.LoadFramePolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	after := append([]string{}, fp.Enabled...)
	for _, id := range k.Frames {
		if !containsStr(after, id) {
			after = append(after, id)
		}
	}
	h.renderConfirm(w, repo, fmt.Sprintf("Apply kit: %s", k.Display), "kit:"+k.Name, "/policy/kits/apply",
		[]previewField{{"repo", repo}, {"name", k.Name}}, fp.Enabled, after)
}

// scanConfirm previews the next push with the scan recommendation applied.
func (h previewHandlers) scanConfirm(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("name")
	if !validRepoName(repo) {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	rec := loadScanRec(h.policyRoot, repo)
	if rec == nil {
		http.Error(w, "no pending recommendation", http.StatusNotFound)
		return
	}
	names := make([]string, 0, len(rec.RecommendedGroups))
	for _, g := range rec.RecommendedGroups {
		names = append(names, g.Name)
	}
	fp, err := gateway.LoadFramePolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	after, _, err := mergeRecommended(fp.Enabled, names)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.renderConfirm(w, repo, "Apply recommended: "+strings.Join(names, ", "), "scan", "/policy/repo/scan-apply",
		[]previewField{{"name", repo}}, fp.Enabled, after)
}

// switchToObserveIfAsked handles the confirm view's "apply and switch to
// observe" button: observe=1 flips the repo to observe mode before the apply
// writes, so a failed switch never leaves a newly-blocking policy enforced.
// Returns false after writing an error response.
func switchToObserveIfAsked(w http.ResponseWriter, r *http.Request, policyRoot, repo, via string) bool {
	if r.FormValue("observe") != "1" {
		return true
	}
	if err := (gateway.FilePolicyStore{Root: policyRoot}).SetObserve(repo, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	_ = gateway.AppendEvent(policyRoot, gateway.Event{
		Event: "set-observe", Repo: repo, OK: true,
		Payload: map[string]any{"observe": true, "via": via},
	})
	return true
}

func init() {
	t := policyTmpl
	template.Must(t.New("previewFindings").Parse(`{{if .Rows}}<details class="gw-np-findings"><summary>Findings ({{.Total}})</summary><table class="fr"><tr><td class="k">severity</td><td class="k">frame</td><td class="k">file:line</td><td class="k">label</td><td class="k"></td></tr>{{range .Rows}}<tr><td>{{.Severity}}{{if .New}} <b class="warn">new</b>{{end}}</td><td><code>{{.Frame}}</code></td><td>{{if .Loc}}<code>{{.Loc}}</code>{{else}}-{{end}}</td><td>{{.Label}}</td><td>{{if .CanWhitelist}}<form class="gw-np-wl" hx-post="/policy/preview/whitelist"><input type="hidden" name="repo" value="{{$.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.File}}"><input type="hidden" name="back" value="{{$.Back}}"><input type="text" name="reason" required placeholder="reason (required)"> <button type="submit">Whitelist this</button></form>{{end}}</td></tr>{{end}}</table>{{if .Truncated}}<p class="sub">Showing the first {{len .Rows}} of {{.Total}}.</p>{{end}}</details>{{end}}`))
	template.Must(t.New("nextPush").Parse(`<section class="frame gw-next-push" id="gw-next-push"{{if .OOB}} hx-swap-oob="outerHTML"{{end}}><h3 class="gw-section-head">Next push preview</h3>{{with .Res}}{{if .Disabled}}<p>Gating is off for this repo: the next push is relayed without a check.</p>{{else if .NoCode}}<p>No pushed code yet: the preview runs once the repo has a commit.</p>{{else if .NoGatedRef}}<p>No pushed branch matches this repo's protected refs yet, so there is nothing to preview against.</p>{{else}}<p>With this policy, the next push would: <b>block {{.BlockCount}}</b>, warn {{.WarnCount}}{{if .ScanError}}. The scan failed, so the push would be rejected.{{end}}</p><p class="sub">Checked against <code>{{.Ref}}</code> at <code>{{$.ShortRev}}</code> (latest pushed code), with the gateway-held whitelist and scan settings, the way the gate runs it.{{if $.Observe}} Observe mode: blocks are recorded and the push is relayed anyway.{{end}}</p>{{if .ScanError}}<p class="warn">{{.ScanError}}</p>{{end}}{{template "previewFindings" $.Findings}}{{end}}{{end}}</section>`))
	template.Must(t.New("previewConfirm").Parse(`<h2 class="gw-pagehead">{{.Title}}</h2><p class="gw-pagedesc">Repo <strong>{{.Repo}}</strong>. Nothing changes until you press Apply.</p><section class="frame gw-next-push">{{if .Unavailable}}<p>{{.Unavailable}}</p>{{else}}<p>Next push, checked against <code>{{.After.Ref}}</code> at <code>{{.ShortRev}}</code>:</p><table class="fr"><tr><td class="k"></td><td class="k">now</td><td class="k">after apply</td></tr><tr><td>block</td><td>{{.Before.BlockCount}}</td><td><b>{{.After.BlockCount}}</b></td></tr><tr><td>warn</td><td>{{.Before.WarnCount}}</td><td>{{.After.WarnCount}}</td></tr></table>{{if .After.ScanError}}<p class="warn">The scan failed, so the push would be rejected: {{.After.ScanError}}</p>{{end}}{{if .After.WouldReject}}<p class="warn">{{if .Observe}}This repo is in observe mode: these blocks are recorded and the push is relayed anyway.{{else}}This repo enforces: after applying, the next push is rejected until these are fixed or whitelisted.{{end}}</p>{{end}}{{template "previewFindings" .Findings}}{{end}}</section><div class="gw-scan-actions"><form hx-post="{{.ApplyURL}}" style="display:inline">{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}<button type="submit">Apply</button></form>{{if .OfferObserve}}<form hx-post="{{.ApplyURL}}" style="display:inline">{{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}<input type="hidden" name="observe" value="1"><button type="submit">Apply and switch {{.Repo}} to observe mode</button></form>{{end}}<a href="/policy?repo={{.Repo}}">Cancel</a></div>`))
}
