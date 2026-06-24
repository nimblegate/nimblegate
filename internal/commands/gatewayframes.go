// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"html/template"
	"net/http"
	"sort"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/gwicons"
	"nimblegate/internal/stdlib"
)

// serveGatewayFrames is the gateway's read-only frame-inspection surface. It
// shows the stdlib catalog grouped Category → Subcategory → Frame, matching
// the /policy browse tree layout so the operator's mental model is the same on
// both pages (Policy = the editable per-repo selection, Frames = the global
// catalog reference). Detail page reuses buildFrameDetail for full metadata.
func serveGatewayFrames(policyRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repo := r.URL.Query().Get("repo")
		if repo != "" && !validRepoName(repo) {
			repo = ""
		}
		stdlibFrames, _ := stdlib.Load()
		var projectFrames []frames.Frame
		expanded := make([]string, 0, len(stdlibFrames))
		for _, f := range stdlibFrames {
			expanded = append(expanded, f.ID())
		}
		cfg := config.ProjectConfig{}

		if id := r.URL.Query().Get("id"); id != "" {
			if d, ok := buildFrameDetail(id, stdlibFrames, projectFrames, expanded, cfg); ok {
				var buf bytes.Buffer
				if err := gwFrameDetailTmpl.Execute(&buf, d); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				renderGwShell(w, gwLayout{Title: id + " : gateway", Chrome: buildChrome("frames", repo, policyRoot), Content: template.HTML(buf.String())})
				return
			}
			http.Error(w, "no such frame: "+id, http.StatusNotFound)
			return
		}
		tree := buildFramesCatalogTree(stdlibFrames)
		var buf bytes.Buffer
		if err := gwFramesListTmpl.Execute(&buf, tree); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderGwShell(w, gwLayout{Title: "frames : gateway", Chrome: buildChrome("frames", repo, policyRoot), Content: template.HTML(buf.String())})
	}
}

// framesCatalogTree is the view-model for the /frames page - the same
// Category → Subcategory → Frame shape the /policy browse tree uses, read-only.
type framesCatalogTree struct {
	Total      int
	Categories []framesCatalogCategory
}

type framesCatalogCategory struct {
	ID            string
	Display       string
	Count         int
	Subcategories []framesCatalogSubcategory
}

type framesCatalogSubcategory struct {
	Name     string
	Display  string
	Frames   []framesCatalogFrame
	Children []framesCatalogSubcategory // nested sub-buckets (Platform > Vendor > Sub)
}

type framesCatalogFrame struct {
	ID         string
	Summary    string
	Severity   string // upper-case label, e.g. BLOCK
	SeverityCl string // lower-case form for the .gw-sev-* CSS class
}

// buildFramesCatalogTree groups all stdlib frames by v2 axis (Core /
// Framework / Platform / Domain) - same shape as the /policy browse tree.
// Each frame classifies to exactly one axis via classifyFrameAxis; the
// Platform axis nests vendor > sub-bucket using buildNestedPlatformSubs.
// Empty axes are omitted EXCEPT Core (universal floor) and Framework
// (always shown so the axis is visible).
func buildFramesCatalogTree(stdlibFrames []frames.Frame) framesCatalogTree {
	core := map[string][]framesCatalogFrame{}
	framework := map[string][]framesCatalogFrame{}
	platform := map[string][]framesCatalogFrame{}
	domain := map[string][]framesCatalogFrame{}
	displayByKey := map[string]string{}

	for _, f := range stdlibFrames {
		summary := firstLineOfBody(f.Body)
		sev := string(f.Frontmatter.Severity)
		ref := framesCatalogFrame{
			ID: f.ID(), Summary: summary, Severity: sev, SeverityCl: strings.ToLower(sev),
		}
		cl := classifyFrameAxis(f)
		switch cl.Axis {
		case v2AxisCore:
			core[cl.Sub] = append(core[cl.Sub], ref)
			displayByKey["core/"+cl.Sub] = cl.Display
		case v2AxisFramework:
			framework[cl.Sub] = append(framework[cl.Sub], ref)
			displayByKey["framework/"+cl.Sub] = cl.Display
		case v2AxisPlatform:
			for _, p := range effectivePlatforms(f.Frontmatter.Platform) {
				platform[p] = append(platform[p], ref)
			}
		case v2AxisDomain:
			domain[cl.Sub] = append(domain[cl.Sub], ref)
			displayByKey["domain/"+cl.Sub] = cl.Display
		}
	}

	flatSubs := func(buckets map[string][]framesCatalogFrame, prefix string) []framesCatalogSubcategory {
		out := make([]framesCatalogSubcategory, 0, len(buckets))
		for k, list := range buckets {
			sortFramesByDisplay(list)
			display := titleCase(k)
			if d, ok := displayByKey[prefix+k]; ok && d != "" {
				display = d
			}
			out = append(out, framesCatalogSubcategory{
				Name: k, Display: display, Frames: list,
			})
		}
		sortSubsByDisplay(out)
		return out
	}

	// Surface canonical framework sub-buckets even when no frame declares
	// one - matches /policy page so the axis shape is visible.
	for _, fw := range canonicalFrameworks {
		if _, ok := framework[fw]; !ok {
			framework[fw] = nil
		}
	}

	platformSubs := buildNestedPlatformCatalogSubs(platform)
	sortSubsByDisplay(platformSubs)
	for i := range platformSubs {
		sortFramesByDisplay(platformSubs[i].Frames)
	}

	var cats []framesCatalogCategory
	total := 0
	for _, ax := range v2AxisOrder {
		var subs []framesCatalogSubcategory
		switch ax.id {
		case v2AxisCore:
			subs = flatSubs(core, "core/")
		case v2AxisFramework:
			subs = flatSubs(framework, "framework/")
		case v2AxisPlatform:
			subs = platformSubs
		case v2AxisDomain:
			subs = flatSubs(domain, "domain/")
		}
		count := 0
		for _, s := range subs {
			count += countCatalogFrames(s)
		}
		if count == 0 && ax.id != v2AxisCore && ax.id != v2AxisFramework {
			continue
		}
		cats = append(cats, framesCatalogCategory{
			ID: string(ax.id), Display: ax.display, Count: count, Subcategories: subs,
		})
		total += count
	}
	return framesCatalogTree{Total: total, Categories: cats}
}

// sortFramesByDisplay orders catalog frames by their visible Summary text
// (case-insensitive), falling back to the frame ID when Summary is empty.
// Matches the "alphabetical by what the user reads" expectation, instead
// of the prior sort-by-ID which let category prefixes (a-...) override the
// summary order the user actually sees.
func sortFramesByDisplay(list []framesCatalogFrame) {
	sort.SliceStable(list, func(i, j int) bool {
		a := strings.ToLower(list[i].Summary)
		b := strings.ToLower(list[j].Summary)
		if a == "" {
			a = strings.ToLower(list[i].ID)
		}
		if b == "" {
			b = strings.ToLower(list[j].ID)
		}
		if a != b {
			return a < b
		}
		return list[i].ID < list[j].ID
	})
}

// sortSubsByDisplay sorts subcategory entries alphabetically by their
// Display label (case-insensitive). Without this, sub-buckets sort by
// their Name field which puts relabeled entries out of place (e.g.,
// Domain sub "web" displays as "HTML" but sorts at the end alphabetically
// by name; sorted by Display, HTML lands between Filesystem and Network).
// Children are sorted recursively.
func sortSubsByDisplay(subs []framesCatalogSubcategory) {
	sort.SliceStable(subs, func(i, j int) bool {
		return strings.ToLower(subs[i].Display) < strings.ToLower(subs[j].Display)
	})
	for i := range subs {
		if len(subs[i].Children) > 0 {
			sortSubsByDisplay(subs[i].Children)
		}
	}
}

// countCatalogFrames returns the count of frames at-or-under one subcategory
// (recurses into Children for nested platform sub-buckets).
func countCatalogFrames(sub framesCatalogSubcategory) int {
	n := len(sub.Frames)
	for _, c := range sub.Children {
		n += countCatalogFrames(c)
	}
	return n
}

// buildNestedPlatformCatalogSubs is the /frames-page analogue of
// buildNestedPlatformSubs in gatewaytuning.go: vendor parent with
// sub-bucket Children. Falls back to top-level placement for orphan
// sub-buckets (no vendor entry).
func buildNestedPlatformCatalogSubs(platformCross map[string][]framesCatalogFrame) []framesCatalogSubcategory {
	subBucketsByVendor := map[string][]string{}
	for sub, vendor := range vendorOf {
		subBucketsByVendor[vendor] = append(subBucketsByVendor[vendor], sub)
	}
	for _, list := range subBucketsByVendor {
		sort.Strings(list)
	}
	vendorSet := map[string]bool{}
	for _, v := range vendorOf {
		vendorSet[v] = true
	}
	subBucketParent := map[string]string{}
	for sub, vendor := range vendorOf {
		subBucketParent[sub] = vendor
	}

	allKeys := make([]string, 0, len(platformCross))
	for k := range platformCross {
		allKeys = append(allKeys, k)
	}
	sort.Strings(allKeys)

	rendered := map[string]bool{}
	var out []framesCatalogSubcategory
	for _, key := range allKeys {
		if rendered[key] {
			continue
		}
		if parent, isSub := subBucketParent[key]; isSub {
			if _, hasParent := platformCross[parent]; !hasParent {
				list := platformCross[key]
				sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
				out = append(out, framesCatalogSubcategory{
					Name: key, Display: titleCase(key), Frames: list,
				})
				rendered[key] = true
			}
			continue
		}
		list := platformCross[key]
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		node := framesCatalogSubcategory{
			Name: key, Display: titleCase(key), Frames: list,
		}
		if vendorSet[key] {
			for _, sub := range subBucketsByVendor[key] {
				if frames, ok := platformCross[sub]; ok {
					sort.Slice(frames, func(i, j int) bool { return frames[i].ID < frames[j].ID })
					node.Children = append(node.Children, framesCatalogSubcategory{
						Name: sub, Display: titleCase(sub), Frames: frames,
					})
					rendered[sub] = true
				}
			}
		}
		out = append(out, node)
		rendered[key] = true
	}
	return out
}

// firstLineOfBody returns the first non-empty markdown line of a frame body -
// used as an inline summary under each frame ID in the catalog tree. Markdown
// header markers are stripped so headings still read cleanly.
func firstLineOfBody(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimLeft(line, "# ")
		return line
	}
	return ""
}

var gwFramesListTmpl = template.Must(template.New("gwframes").Parse(
	`<section>
<h2 class="gw-pagehead">Frames</h2>
<p class="gw-pagedesc">The stdlib catalog of checks the gateway can enforce against pushed trees. Inspection only. Apply kits or tick individual frames per-repo on <a href="/policy" style="color:var(--gw-accent)">Policy</a>.</p>
<div class="gw-fbar"><input type="search" id="frame-search" class="gw-searchbox" placeholder="filter frames…" aria-label="filter frames"><span class="gw-sevchips"><button type="button" class="gw-sevchip fnd BLOCK" data-sev="BLOCK" aria-pressed="true">BLOCK</button><button type="button" class="gw-sevchip fnd WARN" data-sev="WARN" aria-pressed="true">WARN</button><button type="button" class="gw-sevchip fnd INFO" data-sev="INFO" aria-pressed="true">INFO</button></span> <span id="frame-count" class="sub">{{.Total}} frame(s)</span></div>
<div class="gw-policy-browse" id="frames-catalog">
{{define "gw-sub"}}
    <details class="gw-sub">
      <summary>{{.Display}}</summary>
      {{if .Frames}}<ul class="gw-frames">
        {{range .Frames}}
        <li data-sev="{{.Severity}}">
          <a href="/frames?id={{.ID}}"><code>{{.ID}}</code></a>
          <span class="gw-fr-sev gw-sev-{{.SeverityCl}}">{{.Severity}}</span>
          {{if .Summary}}<div class="meta">{{.Summary}}</div>{{end}}
        </li>
        {{end}}
      </ul>{{end}}
      {{range .Children}}{{template "gw-sub" .}}{{end}}
    </details>
{{end}}
{{range .Categories}}
  <details class="gw-cat">
    <summary>{{.Display}} <span class="gw-summary-count">{{.Count}}</span></summary>
    {{range .Subcategories}}{{template "gw-sub" .}}{{end}}
  </details>
{{else}}
  <p class="sub">No frames loaded.</p>
{{end}}
</div>
</section>`))

var gwFrameDetailTmpl = template.Must(template.New("gwframe").Funcs(template.FuncMap{"icon": gwicons.HTML}).Parse(
	`<section>
<h2 class="gw-pagehead"><span class="tag {{.Severity}}">{{.Severity}}</span> {{.ID}}</h2>
<p class="gw-pagedesc"><a href="/frames" style="color:var(--gw-accent);text-decoration:none;display:inline-flex;align-items:center;gap:4px"><svg viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="10 4 5 8 10 12"/></svg> all frames</a> · {{.Category}} · {{.Lifecycle}}</p>
<table class="fr">
  <tr><td class="k">Category</td><td>{{.Category}}</td></tr>
  <tr><td class="k">Tier</td><td>T{{.Tier}}</td></tr>
  <tr><td class="k">Severity</td><td>{{.Severity}}{{if .OverriddenFrom}} (overridden from {{.OverriddenFrom}}){{end}}</td></tr>
  <tr><td class="k">Triggers</td><td>{{.Triggers}}</td></tr>
  {{if .Tags}}<tr><td class="k">Tags</td><td>{{.Tags}}</td></tr>{{end}}
  <tr><td class="k">Lifecycle</td><td>{{.Lifecycle}}</td></tr>
  <tr><td class="k">Enabled</td><td>{{.Enabled}}{{if not .HasCheck}} · {{icon "warn"}} no check function bound{{end}}</td></tr>
  <tr><td class="k">Source</td><td>{{.Source}} - {{.SourcePath}}</td></tr>
</table>
<pre class="body">{{.Body}}</pre>
</section>`))
