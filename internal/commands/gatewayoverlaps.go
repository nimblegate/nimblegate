// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"
)

// overlapHistoryLimit caps the recorded-history table; overlapTailPerRepo is how
// far back each repo's audit log is read to fill it.
const (
	overlapHistoryLimit = 100
	overlapTailPerRepo  = 500
)

// shaChip is one commit SHA on the page: a link when the upstream's commit page
// can be worked out, click-to-copy otherwise.
type shaChip struct {
	SHA string
	URL string
}

type overlapLiveRow struct {
	Repo        string
	Overlap     gateway.Overlap
	Mine, Other shaChip
}

type overlapHistRow struct {
	Repo        string
	Hour        int
	TimeAttr    string
	TimeStr     string
	Overlap     gateway.Overlap
	Mine, Other shaChip
}

type overlapsPageData struct {
	Notice     string
	Repos      []string
	Repo       string
	Checked    int // enforce-mode repos checked live
	Live       []overlapLiveRow
	LiveErrors []string
	History    []overlapHistRow
}

var overlapsTmpl = func() *template.Template {
	t := template.New("overlaps").Funcs(template.FuncMap{
		"icon":  gwicons.HTML,
		"short": func(ref string) string { return strings.TrimPrefix(ref, "refs/heads/") },
		"join":  strings.Join,
		"sha": func(s string) string {
			if len(s) > 7 {
				return s[:7]
			}
			return s
		},
	})
	template.Must(t.New("chip").Parse(`{{if .URL}} <a class="gw-sha" href="{{.URL}}" target="_blank" rel="noopener" title="open commit {{.SHA}} upstream">{{sha .SHA}}</a>{{else if .SHA}} <span class="gw-sha" data-copy="{{.SHA}}" title="commit {{.SHA}} - click to copy">{{sha .SHA}}</span>{{end}}`))
	template.Must(t.New("content").Parse(`<section>
<h2 class="gw-pagehead">Overlaps</h2>
<p class="gw-pagedesc">Open branches that change the same files, so parallel agents find out before their work collides at merge time. Enforce-mode repos only; branches with no commit in 14 days are ignored.</p>
<div class="frame">
{{if .Notice}}<div class="warn">{{icon "warn"}} {{.Notice}}</div>{{end}}
<form class="gw-filters" method="get" action="/overlaps">
  <select name="repo" data-overlaps-repo onchange="this.form.submit()">
    <option value="">all repos</option>
    {{$cur := .Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}
  </select>
</form>
<h3 class="gw-section-head">Now</h3>
{{range .LiveErrors}}<div class="warn">{{icon "warn"}} {{.}}</div>{{end}}
{{if .Live}}
<table class="fr" id="overlaps-live">
<thead><tr><td class="k">repo</td><td class="k">branches</td><td class="k">shared files</td></tr></thead>
<tbody>
{{range .Live}}<tr><td class="k">{{.Repo}}</td><td>{{short .Overlap.Ref}}{{template "chip" .Mine}} &harr; {{short .Overlap.OtherRef}}{{template "chip" .Other}}</td><td>{{join .Overlap.Files ", "}}</td></tr>
{{end}}</tbody>
</table>
{{else}}<div class="sub">{{if .Checked}}No open branches share files right now.{{else}}No enforce-mode repo to check.{{end}}</div>{{end}}
<h3 class="gw-section-head">Recorded on push</h3>
{{if .History}}
<table class="fr" id="overlaps-history">
<thead><tr><td class="k">time</td><td class="k">repo</td><td class="k">pushed</td><td class="k">overlaps with</td><td class="k">shared files</td></tr></thead>
<tbody>
{{range .History}}<tr><td class="loc"><time class="gw-ts gw-tc-{{.Hour}}" datetime="{{.TimeAttr}}">{{.TimeStr}}</time></td><td class="k">{{.Repo}}</td><td>{{short .Overlap.Ref}}{{template "chip" .Mine}}</td><td>{{short .Overlap.OtherRef}}{{template "chip" .Other}}</td><td>{{join .Overlap.Files ", "}}</td></tr>
{{end}}</tbody>
</table>
{{else}}<div class="sub">No overlaps recorded yet.</div>{{end}}
</div>
</section>`))
	return t
}()

// serveGatewayOverlaps shows open branches that change the same files: live
// pairs computed from the bare repos, then the overlaps recorded at push time.
// Read-only.
func serveGatewayOverlaps(policyRoot, reposRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/overlaps" {
			http.NotFound(w, r)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			repo = ""
		}
		data := overlapsPageData{Notice: policyRootNotice(policyRoot), Repos: listGatewayRepos(policyRoot), Repo: repo}

		linker := newCommitLinker(policyRoot)
		chips := func(repo string, o gateway.Overlap) (shaChip, shaChip) {
			return shaChip{SHA: o.SHA, URL: linker.URL(repo, o.SHA)}, shaChip{SHA: o.OtherSHA, URL: linker.URL(repo, o.OtherSHA)}
		}
		store := gateway.FilePolicyStore{Root: policyRoot}
		for _, name := range data.Repos {
			if repo != "" && name != repo {
				continue
			}
			pol, err := store.Load(name)
			if err != nil || !pol.Enabled || pol.Observe {
				continue
			}
			data.Checked++
			ovs, err := gateway.CurrentOverlaps(filepath.Join(reposRoot, name+".git"))
			if err != nil {
				data.LiveErrors = append(data.LiveErrors, name+": "+err.Error())
				continue
			}
			for _, o := range ovs {
				mine, other := chips(name, o)
				data.Live = append(data.Live, overlapLiveRow{Repo: name, Overlap: o, Mine: mine, Other: other})
			}
		}

		recs := gateway.ReadDecisions(policyRoot, overlapTailPerRepo)
		sort.SliceStable(recs, func(i, j int) bool { return recs[i].Time.After(recs[j].Time) })
		for _, rec := range recs {
			if repo != "" && rec.Repo != repo {
				continue
			}
			t := rec.Time.UTC()
			for _, o := range rec.Overlaps {
				mine, other := chips(rec.Repo, o)
				data.History = append(data.History, overlapHistRow{
					Mine:     mine,
					Other:    other,
					Repo:     rec.Repo,
					Hour:     t.Hour(),
					TimeAttr: t.Format("2006-01-02T15:04:05Z"),
					TimeStr:  t.Format("2006-01-02 15:04:05") + "Z",
					Overlap:  o,
				})
			}
			if len(data.History) >= overlapHistoryLimit {
				data.History = data.History[:overlapHistoryLimit]
				break
			}
		}

		var buf bytes.Buffer
		if err := overlapsTmpl.ExecuteTemplate(&buf, "content", data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		renderGwShell(w, gwLayout{Title: "overlaps: gateway", Chrome: buildChrome("overlaps", repo, policyRoot), Content: template.HTML(buf.String())})
	}
}
