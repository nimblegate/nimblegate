// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"
	"nimblegate/internal/whitelist"
)

type whitelistHandlers struct {
	policyRoot string
	token      string
}

type wlRemoveConfirmCtx struct {
	Repo, Frame, Path string
}
type wlRemoveReceiptCtx struct {
	Frame, Path string
	Removed     bool
}

type wlConfirmCtx struct {
	Repo, Frame, Path, Reason, Scope string
	Pattern                          bool
}
type wlReceiptCtx struct {
	Path, Scope    string
	Pattern, Added bool
}

func scopeLabel(path string) (string, bool) {
	if pathIsPattern(path) {
		return "PATTERN (multiple files)", true
	}
	return "single file", false
}

// add is the two-phase whitelist write. confirm absent/0 → preview (no write),
// returns the confirmation panel. confirm=1 → commit, returns the receipt.
func (h whitelistHandlers) add(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo := r.FormValue("repo")
	frame := r.FormValue("frame")
	path := strings.TrimSpace(r.FormValue("path"))
	reason := strings.TrimSpace(r.FormValue("reason"))
	confirm := r.FormValue("confirm") == "1"

	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	if _, ok := stdlibFrameByID()[frame]; !ok {
		http.Error(w, "unknown frame", http.StatusBadRequest)
		return
	}
	if path == "" {
		http.Error(w, "path required", http.StatusBadRequest)
		return
	}
	if reason == "" {
		http.Error(w, "reason required", http.StatusBadRequest)
		return
	}
	scope, pattern := scopeLabel(path)

	if !confirm {
		_ = whitelistTmpl.ExecuteTemplate(w, "confirm", wlConfirmCtx{Repo: repo, Frame: frame, Path: path, Reason: reason, Scope: scope, Pattern: pattern})
		return
	}
	wlPath := filepath.Join(h.policyRoot, repo, ".appframes", "_canonical", "whitelist.toml")
	added, err := whitelist.AddEntry(wlPath, whitelist.Entry{Frame: frame, Path: path, Reason: reason})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if added {
		_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
			Event: "whitelist-add", Repo: repo, OK: true,
			Payload: map[string]any{"frame": frame, "path": path, "reason": reason},
		})
	}
	_ = whitelistTmpl.ExecuteTemplate(w, "receipt", wlReceiptCtx{Path: path, Scope: scope, Pattern: pattern, Added: added})
}

// remove is the two-phase whitelist delete, mirroring add. confirm absent/0 →
// preview (no write); confirm=1 → commit + HX-Refresh so the Whitelisted panel
// re-renders without the removed row.
func (h whitelistHandlers) remove(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo := r.FormValue("repo")
	frame := r.FormValue("frame")
	path := r.FormValue("path")
	confirm := r.FormValue("confirm") == "1"

	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(frame) == "" {
		http.Error(w, "frame required", http.StatusBadRequest)
		return
	}

	if !confirm {
		_ = whitelistTmpl.ExecuteTemplate(w, "removeConfirm", wlRemoveConfirmCtx{Repo: repo, Frame: frame, Path: path})
		return
	}
	wlPath := filepath.Join(h.policyRoot, repo, ".appframes", "_canonical", "whitelist.toml")
	removed, err := whitelist.RemoveEntry(wlPath, frame, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if removed {
		_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
			Event: "whitelist-remove",
			Repo:  repo,
			OK:    true,
			Payload: map[string]any{
				"frame": frame,
				"path":  path,
			},
		})
	}
	// HX-Refresh tells htmx to do a full page reload so the Whitelisted table
	// re-fetches without the just-removed row. Non-htmx clients (curl) get the
	// small receipt body.
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Refresh", "true")
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = whitelistTmpl.ExecuteTemplate(w, "removeReceipt", wlRemoveReceiptCtx{Frame: frame, Path: path, Removed: removed})
}

var whitelistTmpl = func() *template.Template {
	t := template.New("wl").Funcs(template.FuncMap{"icon": gwicons.HTML})
	template.Must(t.New("confirm").Parse(`<div class="wlconfirm">About to whitelist: frame: <code>{{.Frame}}</code> · path: <code>{{.Path}}</code> · scope: <b{{if .Pattern}} class="warn"{{end}}>{{if .Pattern}}{{icon "warn"}} {{end}}{{.Scope}}</b> · reason: "{{.Reason}}" · repo: {{.Repo}}<br><form style="display:inline"><input type="hidden" name="repo" value="{{.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.Path}}"><input type="hidden" name="reason" value="{{.Reason}}"><input type="hidden" name="confirm" value="1"><button type="button" hx-post="/policy/whitelist/add" hx-include="closest form" hx-target="closest .wlout" hx-swap="innerHTML">Confirm whitelist</button></form> <button type="button" onclick="this.closest('.wlout').innerHTML=''">Cancel</button></div>`))
	template.Must(t.New("receipt").Parse(`<span class="ok">whitelisted <code>{{.Path}}</code>: {{if .Pattern}}{{icon "warn"}} {{end}}{{.Scope}} {{if .Added}}{{icon "ok"}} (applies next push){{else}}already present {{icon "ok"}}{{end}}</span>`))
	template.Must(t.New("removeConfirm").Parse(`<div class="wlconfirm">Remove whitelist entry: frame: <code>{{.Frame}}</code> · path: <code>{{.Path}}</code> · repo: {{.Repo}}<br><form style="display:inline"><input type="hidden" name="repo" value="{{.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.Path}}"><input type="hidden" name="confirm" value="1"><button type="button" hx-post="/policy/whitelist/remove" hx-include="closest form" hx-target="closest .wlrm-out" hx-swap="innerHTML">Confirm remove</button></form> <button type="button" onclick="this.closest('.wlrm-out').innerHTML=''">Cancel</button></div>`))
	template.Must(t.New("removeReceipt").Parse(`<span class="ok">{{if .Removed}}removed <code>{{.Path}}</code> {{icon "ok"}}{{else}}entry not found{{end}}</span>`))
	return t
}()
