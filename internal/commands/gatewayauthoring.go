// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/gateway"
	"nimblegate/internal/linters"
)

type authoredCheck struct {
	Name     string
	Patterns string // comma-joined for display
	Regex    string
	Severity string
	Enabled  bool
	ReadOnly bool // true for subprocess/built-in linters (no command authoring in UI)
}

type authoringVM struct {
	Repo       string
	Checks     []authoredCheck
	AllowEdits bool
	Starters   []LinterStarter
}

func buildAuthoringVM(repo string, lp gateway.LinterPolicy, allowEdits bool) authoringVM {
	vm := authoringVM{Repo: repo, AllowEdits: allowEdits, Starters: LinterStarters}
	names := make([]string, 0, len(lp.Linters))
	for n := range lp.Linters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := lp.Linters[n]
		vm.Checks = append(vm.Checks, authoredCheck{
			Name: n, Patterns: strings.Join(c.Patterns, ", "), Regex: c.Regex,
			Severity: strings.ToUpper(c.Severity), Enabled: c.Enabled,
			ReadOnly: c.Kind != "regex", // only regex checks are UI-managed
		})
	}
	return vm
}

// mkrow lets the row template see both the section context ($) and the row.
func mkrow(vm authoringVM, c authoredCheck) map[string]any {
	return map[string]any{"AllowEdits": vm.AllowEdits, "Repo": vm.Repo, "Check": c}
}

var authoringTmpl = func() *template.Template {
	t := template.New("authoring").Funcs(template.FuncMap{"mkrow": mkrow})
	return template.Must(t.Parse(`
{{define "checkRow"}}
<div id="check-{{.Check.Name}}" class="hit">
  <b>{{.Check.Name}}</b> <code>{{.Check.Patterns}}</code> /{{.Check.Regex}}/
  {{if and $.AllowEdits (not .Check.ReadOnly)}}
    <select name="severity" hx-post="/policy/check/severity" hx-target="#check-{{.Check.Name}}" hx-vals='{"name":"{{.Check.Name}}","repo":"{{$.Repo}}"}'>
      <option {{if eq .Check.Severity "BLOCK"}}selected{{end}}>BLOCK</option>
      <option {{if eq .Check.Severity "WARN"}}selected{{end}}>WARN</option>
      <option {{if eq .Check.Severity "INFO"}}selected{{end}}>INFO</option>
    </select>
    <button hx-post="/policy/check/enabled" hx-target="#check-{{.Check.Name}}" hx-vals='{"name":"{{.Check.Name}}","repo":"{{$.Repo}}","enabled":"{{if .Check.Enabled}}false{{else}}true{{end}}"}'>{{if .Check.Enabled}}on{{else}}off{{end}}</button>
    <button hx-post="/policy/check/delete" hx-target="next .checkrm-out" hx-swap="innerHTML" hx-vals='{"name":"{{.Check.Name}}","repo":"{{$.Repo}}"}'>delete</button>
    <span class="checkrm-out"></span>
  {{else}}
    <span class="tag {{.Check.Severity}}">{{.Check.Severity}}</span>{{if .Check.ReadOnly}} <span class="loc">(read-only)</span>{{end}}
  {{end}}
</div>
{{end}}

{{define "section"}}
<div id="authoring" class="gw-authoring">
  {{range .Checks}}{{template "checkRow" (mkrow $ .)}}{{end}}
  {{if .AllowEdits}}
  <form id="lint-add">
    <label class="gw-lint-starter">Start from a pattern
      <select name="_starter" onchange="gwLintStarterApply(this)">
        <option value="">(write your own)</option>
        {{range .Starters}}
        <option value="{{.ID}}" data-name="{{.Name}}" data-patterns="{{.Patterns}}" data-regex="{{.Regex}}" data-severity="{{.Severity}}" title="{{.Description}}">{{.Label}}</option>
        {{end}}
      </select>
      <span class="gw-lint-starter-hint sub">Pick a starting pattern, then refine the name / globs / regex below. Your agent can help adjust the regex if it doesn't quite match what you want.</span>
    </label>
    <label>Name<input type="text" name="name" placeholder="lowercase, digits, dashes, e.g. internal-secret"></label>
    <label>File patterns<input type="text" name="patterns" placeholder="comma-separated globs, e.g. *.go, src/**/*.ts"></label>
    <label>Regex<input type="text" name="regex" placeholder="the pattern that flags a finding"></label>
    <label>Severity<select name="severity"><option>WARN</option><option>INFO</option><option>BLOCK</option></select></label>
    <div class="gw-authoring-actions">
      <button type="button" hx-post="/policy/check/preview" hx-target="#preview" hx-vals='{"repo":"{{.Repo}}"}' hx-include="closest form" class="secondary">Preview</button>
      <button type="submit" hx-post="/policy/check/add" hx-target="#authoring" hx-vals='{"repo":"{{.Repo}}"}' hx-include="closest form">Add check</button>
    </div>
  </form>
  <div id="preview"></div>
  <script>
  // Auto-fill the form fields when the operator picks a starter. The
  // dropdown's options carry data-* attributes with the starter values;
  // this picks them up and writes into the four sibling inputs. The
  // operator's intent is "give me a starting point" - overwriting only
  // the existing values is safe because they came from a previous starter
  // pick or are still placeholders.
  function gwLintStarterApply(select) {
    var opt = select.selectedOptions[0];
    if (!opt || !opt.value) return;
    var form = select.closest('form');
    var set = function(name, val) {
      var el = form.querySelector('[name="' + name + '"]');
      if (el && val !== undefined) el.value = val;
    };
    set('name', opt.getAttribute('data-name'));
    set('patterns', opt.getAttribute('data-patterns'));
    set('regex', opt.getAttribute('data-regex'));
    set('severity', opt.getAttribute('data-severity'));
  }
  </script>
  {{end}}
</div>
{{end}}

{{define "deleteConfirm"}}
<span class="wlconfirm gw-lint-confirm">Delete check <code>{{.Name}}</code>? <form hx-post="/policy/check/delete" hx-target="#authoring" hx-swap="outerHTML" hx-vals='{"name":"{{.Name}}","repo":"{{.Repo}}","confirm":"1"}'><button type="submit">Confirm delete</button></form> <button type="button" onclick="this.closest('.checkrm-out').innerHTML=''">Cancel</button></span>
{{end}}
`))
}()

func renderAuthoringSection(repo string, lp gateway.LinterPolicy, allowEdits bool) string {
	vm := buildAuthoringVM(repo, lp, allowEdits)
	var b strings.Builder
	_ = authoringTmpl.ExecuteTemplate(&b, "section", vm)
	return b.String()
}

type authoringHandlers struct {
	policyRoot string
	token      string
	reposRoot  string // for preview (Task 7); "" → preview unavailable
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
var validSev = map[string]bool{"BLOCK": true, "WARN": true, "INFO": true}

// repoNameRe matches safe repo names: lowercase alphanumeric, dots, dashes,
// underscores; must start with alphanumeric.  No path separators or traversal.
var repoNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// validRepoName rejects anything that would allow path traversal into the
// policy root.  The strings.Contains("..") check is the load-bearing guard;
// the regexp rejects separators and other unexpected chars.
func validRepoName(s string) bool {
	return repoNameRe.MatchString(s) && !strings.Contains(s, "..")
}

// guarded validates CSRF + same-origin + that the repo name is safe + that the
// repo is registered.  Returns the repo on success; writes the error and
// returns "" otherwise.
func (h authoringHandlers) guarded(w http.ResponseWriter, r *http.Request) string {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return ""
	}
	repo := r.FormValue("repo")
	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return ""
	}
	if _, err := (gateway.FilePolicyStore{Root: h.policyRoot}).Load(repo); err != nil {
		http.Error(w, "unknown repo", http.StatusBadRequest)
		return ""
	}
	return repo
}

func (h authoringHandlers) add(w http.ResponseWriter, r *http.Request) {
	repo := h.guarded(w, r)
	if repo == "" {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	regex := r.FormValue("regex")
	sev := strings.ToUpper(strings.TrimSpace(r.FormValue("severity")))
	patterns := splitPatterns(r.FormValue("patterns"))
	if !nameRe.MatchString(name) {
		http.Error(w, "invalid name (use a-z 0-9 -)", http.StatusBadRequest)
		return
	}
	if !validSev[sev] {
		http.Error(w, "invalid severity", http.StatusBadRequest)
		return
	}
	if len(patterns) == 0 {
		http.Error(w, "need at least one file pattern", http.StatusBadRequest)
		return
	}
	if _, err := regexp.Compile(regex); err != nil {
		http.Error(w, "invalid regex: "+err.Error(), http.StatusBadRequest)
		return
	}
	lp, err := gateway.LoadLinterPolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, "load error", http.StatusInternalServerError)
		return
	}
	if _, exists := lp.Linters[name]; exists {
		http.Error(w, "a check with that name already exists", http.StatusBadRequest)
		return
	}
	lp = lp.With(name, config.LinterConfig{Enabled: true, Kind: "regex", Severity: sev, Patterns: patterns, Regex: regex})
	if err := lp.Save(h.policyRoot, repo); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "linter-add", Repo: repo, OK: true,
		Payload: map[string]any{"name": name, "severity": sev, "patterns": patterns},
	})
	writeAuthoringSection(w, repo, lp, true)
}

func (h authoringHandlers) delete(w http.ResponseWriter, r *http.Request) {
	repo := h.guarded(w, r)
	if repo == "" {
		return
	}
	name := r.FormValue("name")
	confirm := r.FormValue("confirm") == "1"
	lp, err := gateway.LoadLinterPolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, "load error", http.StatusInternalServerError)
		return
	}
	if c, ok := lp.Linters[name]; !ok || c.Kind != "regex" {
		http.Error(w, "not an editable check", http.StatusBadRequest)
		return
	}
	if !confirm {
		_ = authoringTmpl.ExecuteTemplate(w, "deleteConfirm", struct{ Name, Repo string }{Name: name, Repo: repo})
		return
	}
	lp = lp.Delete(name)
	if err := lp.Save(h.policyRoot, repo); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "linter-delete", Repo: repo, OK: true,
		Payload: map[string]any{"name": name},
	})
	writeAuthoringSection(w, repo, lp, true)
}

func (h authoringHandlers) severity(w http.ResponseWriter, r *http.Request) {
	repo := h.guarded(w, r)
	if repo == "" {
		return
	}
	name := r.FormValue("name")
	sev := strings.ToUpper(strings.TrimSpace(r.FormValue("severity")))
	if !validSev[sev] {
		http.Error(w, "invalid severity", http.StatusBadRequest)
		return
	}
	lp, err := gateway.LoadLinterPolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, "load error", http.StatusInternalServerError)
		return
	}
	if c, ok := lp.Linters[name]; !ok || c.Kind != "regex" {
		http.Error(w, "not an editable check", http.StatusBadRequest)
		return
	}
	lp = lp.SetSeverity(name, sev)
	if err := lp.Save(h.policyRoot, repo); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "linter-severity", Repo: repo, OK: true,
		Payload: map[string]any{"name": name, "severity": sev},
	})
	writeAuthoringSection(w, repo, lp, true)
}

func (h authoringHandlers) enabled(w http.ResponseWriter, r *http.Request) {
	repo := h.guarded(w, r)
	if repo == "" {
		return
	}
	name := r.FormValue("name")
	on := r.FormValue("enabled") == "true"
	lp, err := gateway.LoadLinterPolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, "load error", http.StatusInternalServerError)
		return
	}
	if c, ok := lp.Linters[name]; !ok || c.Kind != "regex" {
		http.Error(w, "not an editable check", http.StatusBadRequest)
		return
	}
	lp = lp.SetEnabled(name, on)
	if err := lp.Save(h.policyRoot, repo); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "linter-enabled", Repo: repo, OK: true,
		Payload: map[string]any{"name": name, "enabled": on},
	})
	writeAuthoringSection(w, repo, lp, true)
}

func (h authoringHandlers) preview(w http.ResponseWriter, r *http.Request) {
	repo := h.guarded(w, r)
	if repo == "" {
		return
	}
	regex := r.FormValue("regex")
	patterns := splitPatterns(r.FormValue("patterns"))
	re, err := regexp.Compile(regex)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<div class="hit">invalid regex: %s</div>`, template.HTMLEscapeString(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if h.reposRoot == "" {
		_, _ = w.Write([]byte(`<div class="hit">preview unavailable (no --repos-root configured)</div>`))
		return
	}
	bare := filepath.Join(h.reposRoot, repo+".git")
	dir, cleanup, err := gateway.PreviewTree(bare)
	if err != nil {
		_, _ = w.Write([]byte(`<div class="hit">no pushed tree to preview against yet</div>`))
		return
	}
	defer cleanup()
	hits, err := linters.ScanRegexContent(dir, patterns, re, nil)
	if err != nil {
		_, _ = w.Write([]byte(`<div class="hit">preview scan error</div>`))
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<div class="hit">would flag %d</div>`, len(hits))
	sort.Slice(hits, func(i, j int) bool { return hits[i].File < hits[j].File })
	for i, hh := range hits {
		if i >= 20 {
			fmt.Fprintf(&b, `<div class="hit">… and %d more</div>`, len(hits)-20)
			break
		}
		fmt.Fprintf(&b, `<div class="hit"><code>%s:%d</code> %s</div>`,
			template.HTMLEscapeString(hh.File), hh.Line, template.HTMLEscapeString(hh.Label))
	}
	_, _ = w.Write([]byte(b.String()))
}

func splitPatterns(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func writeAuthoringSection(w http.ResponseWriter, repo string, lp gateway.LinterPolicy, allowEdits bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderAuthoringSection(repo, lp, allowEdits)))
}
