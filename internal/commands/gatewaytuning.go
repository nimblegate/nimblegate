// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// NOTE: dashboard /policy renderer is being rewritten in T9-T11.
// T9 = new policyVM shape. T10 = renderer (this file). T11 = htmx endpoints.

package commands

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gwicons"
	"nimblegate/internal/kits"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/whitelist"
)

// csrfOK validates a mutating request: X-CSRF-Token must match the per-process
// token (constant-time) and the request must not be cross-site. Absent
// Sec-Fetch-Site (older clients) is allowed - the secret token is the defense.
func csrfOK(r *http.Request, token string) bool {
	got := r.Header.Get("X-CSRF-Token")
	if token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site", "same-site":
		return false
	}
	return true
}

// policyVM is the view model for the /policy page category-tree browse.
// Categories lists the canonical taxonomy in order, with empty categories omitted.
// BuiltinKits is the Quick-start row of pre-defined starter kits.
type policyVM struct {
	Repo        string
	Enabled     []string        // flat list, source of truth
	AppliedKits []string        // built-in kit names currently applied
	UserKits    []policyUserKit // user-defined custom kits
	Categories  []policyCategory
	BuiltinKits []policyKitInfo // for the Quick-start row
}

type policyCategory struct {
	ID            string // canonical lowercase, e.g. "security"
	Display       string // Title Case, e.g. "Security"
	Subcategories []policySubcategory
}

type policySubcategory struct {
	Name     string // e.g. "credentials"
	Display  string // e.g. "Credentials"
	Frames   []policyFrameRef
	Children []policySubcategory // optional nested sub-sub-buckets (e.g., Platform > Cloudflare > Cf-Pages)
}

type policyFrameRef struct {
	ID       string // e.g. "security/no-hardcoded-credentials"
	Enabled  bool
	Severity string
	Tier     int
}

type policyUserKit struct {
	Name   string
	Frames []policyFrameRef
}

type policyKitInfo struct {
	Name         string
	Display      string
	Description  string
	FrameCount   int
	FullyApplied bool // every frame already in Enabled
}

// policyPageOpts carries per-request context for the /policy page renderer.
type policyPageOpts struct {
	AllowEdits     bool
	CSRFToken      string
	Repos          []string
	ActiveTab      string        // "frames" (default) | "linters" | "whitelist"
	Authoring      template.HTML // pre-rendered authored-checks section (step 3)
	NotifRail      template.HTML // pre-rendered notification rail section (spec §7.4)
	Notice         string        // loud config warning (empty = none)
	Chrome         chromeData
	ScanRec        *ScanRecommendation // banner shown when non-nil and !Dismissed
	Archived       []string            // lib entries without an active symlink
	PolicyRoot     string              // gateway policy root - used to resolve per-tier time-estimates for the editor
	JustRegistered string              // non-empty repo name: show success banner at page top
}

// groupToggle + groupToggleSets are retained for the add-repo form (T10 will migrate them).
type groupToggle struct {
	Name    string
	Checked bool
}

// groupToggleSets holds the two-axis toggles rendered on /policy + add-repo.
type groupToggleSets struct {
	Coverage []groupToggle
	Stack    []groupToggle
	Other    []groupToggle
}

// coverageGroups + stackGroups retained for the add-repo form until T10 rewrites it.
var coverageGroups = []string{"@tier-1", "@tier-6", "@security-strict"}
var stackGroups = []string{"@web", "@cf-pages", "@cloudflare", "@migrations"}

// buildGroupToggleSets returns one toggle per canonical group plus one per
// any extra-already-enabled group not in either canonical list. Order:
// canonical first (Coverage, then Stack), then the extras (Other) in
// enabled-order.
func buildGroupToggleSets(enabled []string) groupToggleSets {
	enabledSet := make(map[string]bool, len(enabled))
	for _, g := range enabled {
		enabledSet[g] = true
	}
	seen := make(map[string]bool, len(coverageGroups)+len(stackGroups))
	cov := make([]groupToggle, 0, len(coverageGroups))
	for _, g := range coverageGroups {
		cov = append(cov, groupToggle{Name: g, Checked: enabledSet[g]})
		seen[g] = true
	}
	stk := make([]groupToggle, 0, len(stackGroups))
	for _, g := range stackGroups {
		stk = append(stk, groupToggle{Name: g, Checked: enabledSet[g]})
		seen[g] = true
	}
	var oth []groupToggle
	for _, g := range enabled {
		if !seen[g] {
			oth = append(oth, groupToggle{Name: g, Checked: true})
			seen[g] = true
		}
	}
	return groupToggleSets{Coverage: cov, Stack: stk, Other: oth}
}

// buildPolicyView constructs the new category-tree policyVM.
// policyRoot is the gateway policy root dir; repo is the repo slug; enabled is the
// flat list of active frame IDs (already expanded by the caller).
func buildPolicyView(policyRoot, repo string, enabled []string) policyVM {
	allFrames, _ := stdlib.Load()
	ks, _ := kits.LoadStdlib()

	enabledSet := map[string]bool{}
	for _, id := range enabled {
		enabledSet[id] = true
	}

	// v2 axis grouping: each frame classifies to exactly one of Core /
	// Framework / Platform / Domain via classifyFrameAxis. Platform is the
	// only axis that gets a vendor > sub-bucket nested tree (built later
	// via buildNestedPlatformSubs from the per-frame platform bucket).
	coreBuckets := map[string][]policyFrameRef{}      // core sub-bucket (git/commands) → frames
	frameworkBuckets := map[string][]policyFrameRef{} // framework name (svelte/astro/...) → frames
	platformCross := map[string][]policyFrameRef{}    // platform leaf (vendor or sub-bucket) → frames
	domainBuckets := map[string][]policyFrameRef{}    // domain sub-bucket (security/html/...) → frames
	displayBySub := map[string]string{}               // sub-bucket id → display label (for relabels like web→HTML)

	for _, f := range allFrames {
		ref := policyFrameRef{
			ID:       f.ID(),
			Enabled:  enabledSet[f.ID()],
			Severity: string(f.Frontmatter.Severity),
			Tier:     f.Frontmatter.EffectiveTier(),
		}
		cl := classifyFrameAxis(f)
		switch cl.Axis {
		case v2AxisCore:
			coreBuckets[cl.Sub] = append(coreBuckets[cl.Sub], ref)
			displayBySub["core/"+cl.Sub] = cl.Display
		case v2AxisFramework:
			frameworkBuckets[cl.Sub] = append(frameworkBuckets[cl.Sub], ref)
			displayBySub["framework/"+cl.Sub] = cl.Display
		case v2AxisPlatform:
			// Honor the effectivePlatforms dedup: a frame with both vendor
			// and sub-bucket tags goes ONLY into the sub-bucket bucket. The
			// nested tree builder later reconstructs vendor > sub layout.
			for _, p := range effectivePlatforms(f.Frontmatter.Platform) {
				platformCross[p] = append(platformCross[p], ref)
			}
		case v2AxisDomain:
			domainBuckets[cl.Sub] = append(domainBuckets[cl.Sub], ref)
			displayBySub["domain/"+cl.Sub] = cl.Display
		}
	}

	var cats []policyCategory
	for _, ax := range v2AxisOrder {
		switch ax.id {
		case v2AxisCore:
			subs := buildFlatBuckets(coreBuckets, "core/", displayBySub)
			// Core is always shown so operators see the universal floor.
			cats = append(cats, policyCategory{ID: string(ax.id), Display: ax.display, Subcategories: subs})
		case v2AxisFramework:
			// Always surface the canonical framework list so operators see
			// the axis shape (svelte/astro/react/...) even when no frame
			// declares one of them yet.
			for _, fw := range canonicalFrameworks {
				if _, ok := frameworkBuckets[fw]; !ok {
					frameworkBuckets[fw] = nil
				}
			}
			subs := buildFlatBuckets(frameworkBuckets, "framework/", displayBySub)
			cats = append(cats, policyCategory{ID: string(ax.id), Display: ax.display, Subcategories: subs})
		case v2AxisPlatform:
			subs := buildNestedPlatformSubs(platformCross)
			sortPolicySubsByDisplay(subs)
			for i := range subs {
				sortPolicyFramesByID(subs[i].Frames)
			}
			if len(subs) > 0 {
				cats = append(cats, policyCategory{ID: string(ax.id), Display: ax.display, Subcategories: subs})
			}
		case v2AxisDomain:
			subs := buildFlatBuckets(domainBuckets, "domain/", displayBySub)
			if len(subs) > 0 {
				cats = append(cats, policyCategory{ID: string(ax.id), Display: ax.display, Subcategories: subs})
			}
		}
	}

	var builtins []policyKitInfo
	for _, name := range []string{"core", "web-app", "cf-pages-project", "cf-workers-project", "security-strict"} {
		k, ok := ks.Get(name)
		if !ok {
			continue
		}
		fully := true
		for _, fid := range k.Frames {
			if !enabledSet[fid] {
				fully = false
				break
			}
		}
		builtins = append(builtins, policyKitInfo{
			Name: k.Name, Display: k.Display, Description: k.Description,
			FrameCount: len(k.Frames), FullyApplied: fully,
		})
	}

	return policyVM{
		Repo:        repo,
		Enabled:     enabled,
		AppliedKits: readAppliedKits(policyRoot, repo),
		UserKits:    readUserKits(policyRoot, repo, enabledSet),
		Categories:  cats,
		BuiltinKits: builtins,
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func titleCase(s string) string {
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// vendorOf maps platform sub-buckets to their parent vendor. Sourced from the
// v2 stdlib layout (internal/stdlib/v2/platform/<vendor>/<sub-bucket>/). When a
// frame's frontmatter lists both the vendor and a sub-bucket (e.g.,
// platform: [cloudflare, cf-pages]), the dashboard's platform tree would
// surface the frame twice - once under "Cloudflare", once under "Cf Pages".
// effectivePlatforms drops the vendor entry when a sub-bucket of that vendor
// is also present, so each frame appears exactly once in the platform tree.
var vendorOf = map[string]string{
	"cf-pages":   "cloudflare",
	"cf-workers": "cloudflare",
	"cf-d1":      "cloudflare",
	"cf-kv":      "cloudflare",
}

// buildFlatBuckets converts a sub-bucket-keyed frames map into a sorted
// flat policySubcategory list. Used for Core / Framework / Domain axes
// where no further nesting applies. displayBySub supplies labels for the
// keys (e.g., "web" → "HTML") when an override exists; otherwise the
// key passes through titleCase. Output is sorted by Display label so
// relabeled entries land in alphabetical position (HTML between
// Filesystem and Network, not at the end as "web").
func buildFlatBuckets(buckets map[string][]policyFrameRef, displayKeyPrefix string, displayBySub map[string]string) []policySubcategory {
	out := make([]policySubcategory, 0, len(buckets))
	for k, frames := range buckets {
		display := titleCase(k)
		if d, ok := displayBySub[displayKeyPrefix+k]; ok && d != "" {
			display = d
		}
		sortPolicyFramesByID(frames)
		out = append(out, policySubcategory{
			Name: k, Display: display, Frames: frames,
		})
	}
	sortPolicySubsByDisplay(out)
	return out
}

// sortPolicyFramesByID orders policy frames alphabetically by their ID
// (the visible label on the /policy page is the ID itself - no separate
// summary field - so by-ID is the user-visible alphabetical order here).
func sortPolicyFramesByID(list []policyFrameRef) {
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(list[i].ID) < strings.ToLower(list[j].ID)
	})
}

// sortPolicySubsByDisplay sorts subcategory entries by Display label
// (case-insensitive), recursing into Children for the platform tree.
func sortPolicySubsByDisplay(subs []policySubcategory) {
	sort.SliceStable(subs, func(i, j int) bool {
		return strings.ToLower(subs[i].Display) < strings.ToLower(subs[j].Display)
	})
	for i := range subs {
		if len(subs[i].Children) > 0 {
			sortPolicySubsByDisplay(subs[i].Children)
		}
	}
}

// buildNestedPlatformSubs converts a flat tag→frames map into the nested
// Platform > Vendor > Sub-bucket tree the dashboard renders. Frames tagged
// with only a vendor (e.g., "cloudflare") sit on the vendor's Frames slice;
// frames tagged with a sub-bucket (e.g., "cf-pages") nest into the vendor's
// Children. Unrelated platform tags (aws, vercel) become flat vendor entries
// with no children. Output is alphabetically sorted at each level for diff
// stability.
func buildNestedPlatformSubs(platformCross map[string][]policyFrameRef) []policySubcategory {
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

	allKeys := sortedMapKeys(platformCross)
	rendered := map[string]bool{}

	var out []policySubcategory
	for _, key := range allKeys {
		if rendered[key] {
			continue
		}
		if parent, isSub := subBucketParent[key]; isSub {
			if _, hasParent := platformCross[parent]; !hasParent {
				out = append(out, policySubcategory{
					Name: key, Display: titleCase(key), Frames: platformCross[key],
				})
				rendered[key] = true
			}
			continue
		}
		node := policySubcategory{
			Name: key, Display: titleCase(key), Frames: platformCross[key],
		}
		if vendorSet[key] {
			for _, sub := range subBucketsByVendor[key] {
				if frames, ok := platformCross[sub]; ok {
					node.Children = append(node.Children, policySubcategory{
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

// effectivePlatforms returns the platform tags a frame should be displayed
// under. Vendor tags are dropped when a sub-bucket of the same vendor is
// also tagged; sub-bucket tags are always kept; unrelated platform tags
// pass through unchanged. Pure.
func effectivePlatforms(tags []string) []string {
	if len(tags) <= 1 {
		return tags
	}
	has := make(map[string]bool, len(tags))
	for _, t := range tags {
		has[t] = true
	}
	suppress := make(map[string]bool)
	for sub, vendor := range vendorOf {
		if has[sub] && has[vendor] {
			suppress[vendor] = true
		}
	}
	if len(suppress) == 0 {
		return tags
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !suppress[t] {
			out = append(out, t)
		}
	}
	return out
}

// readAppliedKits reads the applied_kits list from <policyRoot>/<repo>/appframes.toml.
func readAppliedKits(policyRoot, repo string) []string {
	if repo == "" {
		return nil
	}
	cfg, err := readFullConfig(filepath.Join(policyRoot, repo, "appframes.toml"))
	if err != nil {
		return nil
	}
	return cfg.UI.AppliedKits
}

// userKitTOML mirrors one [[ui.user_kits]] table in appframes.toml.
type userKitTOML struct {
	Name   string   `toml:"name"`
	Frames []string `toml:"frames"`
}

// configFile is the full in-memory representation of appframes.toml.
// Used by readFullConfig / writeFullConfig for whole-file mutations.
type configFile struct {
	Frames struct {
		Enabled []string `toml:"enabled"`
	} `toml:"frames"`
	UI struct {
		AppliedKits []string      `toml:"applied_kits"`
		UserKits    []userKitTOML `toml:"user_kits"`
	} `toml:"ui"`
}

func readFullConfig(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := &configFile{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func writeFullConfig(path string, cfg *configFile) error {
	var b strings.Builder
	b.WriteString("[frames]\nenabled = [\n")
	for _, id := range cfg.Frames.Enabled {
		fmt.Fprintf(&b, "    %q,\n", id)
	}
	b.WriteString("]\n\n[ui]\napplied_kits = [")
	for i, n := range cfg.UI.AppliedKits {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", n)
	}
	b.WriteString("]\n")
	for _, uk := range cfg.UI.UserKits {
		fmt.Fprintf(&b, "\n[[ui.user_kits]]\nname = %q\nframes = [\n", uk.Name)
		for _, f := range uk.Frames {
			fmt.Fprintf(&b, "    %q,\n", f)
		}
		b.WriteString("]\n")
	}
	return atomicWriteFile(path, []byte(b.String()))
}

// readUserKits reads [[ui.user_kits]] from <policyRoot>/<repo>/appframes.toml and
// maps each kit to a []policyFrameRef using the supplied enabled set.
func readUserKits(policyRoot, repo string, enabled map[string]bool) []policyUserKit {
	if repo == "" {
		return nil
	}
	cfg, err := readFullConfig(filepath.Join(policyRoot, repo, "appframes.toml"))
	if err != nil {
		return nil
	}
	var out []policyUserKit
	for _, uk := range cfg.UI.UserKits {
		var refs []policyFrameRef
		for _, fid := range uk.Frames {
			refs = append(refs, policyFrameRef{ID: fid, Enabled: enabled[fid]})
		}
		out = append(out, policyUserKit{Name: uk.Name, Frames: refs})
	}
	return out
}

// addUserKit appends a new [[ui.user_kits]] entry to cfgPath and also ticks
// every frame in frames into [frames].enabled. Returns an error if a kit with
// the same name already exists.
func addUserKit(cfgPath, name string, frames []string) error {
	cfg, err := readFullConfig(cfgPath)
	if err != nil {
		return err
	}
	for _, uk := range cfg.UI.UserKits {
		if uk.Name == name {
			return fmt.Errorf("user kit %q already exists", name)
		}
	}
	cfg.UI.UserKits = append(cfg.UI.UserKits, userKitTOML{Name: name, Frames: frames})
	for _, f := range frames {
		if !containsStr(cfg.Frames.Enabled, f) {
			cfg.Frames.Enabled = append(cfg.Frames.Enabled, f)
		}
	}
	return writeFullConfig(cfgPath, cfg)
}

// deleteUserKit removes the named [[ui.user_kits]] entry from cfgPath.
// Deliberately does NOT untick any frames - the operator clears those via the
// chip × or individual frame unticks.
func deleteUserKit(cfgPath, name string) error {
	cfg, err := readFullConfig(cfgPath)
	if err != nil {
		return err
	}
	out := cfg.UI.UserKits[:0]
	for _, uk := range cfg.UI.UserKits {
		if uk.Name != name {
			out = append(out, uk)
		}
	}
	cfg.UI.UserKits = out
	return writeFullConfig(cfgPath, cfg)
}

func enabledMatch(id string, expanded []string) bool {
	for _, pat := range expanded {
		if pat == id {
			return true
		}
		if strings.HasSuffix(pat, "/*") && strings.HasPrefix(id, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

// loadScanRec reads <policyRoot>/<repo>/scan-recommendation.json. Returns nil
// on any of: file absent, parse failure, or rec.Dismissed == true - the banner
// is a hint, not a guarantee, so all errors are silent (best-effort).
func loadScanRec(policyRoot, repo string) *ScanRecommendation {
	if repo == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(policyRoot, repo, "scan-recommendation.json"))
	if err != nil {
		return nil
	}
	var rec ScanRecommendation
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil
	}
	if rec.Dismissed {
		return nil
	}
	return &rec
}

// listGatewayRepos returns the registered repo names (dirs under policyRoot
// containing gateway.toml), sorted. Used by the /policy repo picker.
func listGatewayRepos(policyRoot string) []string {
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	var out []string
	for _, m := range matches {
		out = append(out, filepath.Base(filepath.Dir(m)))
	}
	sort.Strings(out)
	return out
}

// policyTmpl renders the peripheral sections of /policy that are NOT part of the
// category-tree rewrite (T9-T11). The add-repo form, scan banner, archived panel,
// credential section, time-estimates editor, and archive/restore actions all live
// here. The category-tree body is stubbed with a placeholder until T10 ships the
// real renderer.
var policyTmpl = func() *template.Template {
	t := template.New("policy").Funcs(template.FuncMap{"icon": gwicons.HTML})
	template.Must(t.New("addRepoForm").Parse(`{{if .AllowEdits}}<details class="frame gw-add-repo"{{if not .Repos}} open{{end}}><summary class="gw-section-head">+ Add new repo to gateway</summary><form hx-post="/policy/repo/add" hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded" hx-target="body" hx-swap="outerHTML"><label>Name <input type="text" name="name" required pattern="[a-zA-Z0-9_\-]+" placeholder="my-repo" title="Letters, numbers, hyphens (-) and underscores (_) only. No spaces or other symbols."></label><p class="sub">Letters, numbers, hyphens, underscores only.</p><label>Upstream URL <input type="text" name="upstream" required placeholder="https://github.com/owner/repo.git"></label><p class="sub">The repo's <b>HTTPS</b> clone URL (e.g. <code>https://github.com/owner/repo.git</code>) - not the <code>git@…</code> SSH form. The token below authenticates it; works for private repos too.</p><label>Upstream credential <input type="password" name="upstream_credential" autocomplete="new-password" placeholder="PAT / deploy token (optional, stored mode 0600, never logged)"></label><label>Protected refs <input type="text" name="protected_refs" value="refs/heads/main" placeholder="refs/heads/main refs/heads/release/*" title="space-separated full ref names; bare branch names like &#34;main&#34; are auto-prefixed with refs/heads/. Pattern is path.Match: &#34;release/*&#34; matches one segment, not recursive."></label><fieldset class="gw-status-fieldset"><legend>Status</legend><label><input type="checkbox" name="enabled" value="1" checked> enabled</label><label><input type="checkbox" name="observe" value="1"> observe-only</label></fieldset><p class="gw-add-note">The <b>Core</b> kit (15 catastrophic-prevention frames) is applied automatically. After registering, refine the selection in the Frame selection section at the bottom: apply additional kits (Web app, CF Pages, CF Workers, Security strict) or tick individual frames.</p><button type="submit">Register repo</button><p class="sub">First push will auto-scan and recommend more.</p></form></details>{{end}}`))
	// credentialSection is kept defined here so /repos can reuse the same form shape
	// by invoking it with a data value that carries Repo, AllowEdits, and CSRFToken.
	template.Must(t.New("credentialSection").Parse(`{{if and .AllowEdits .Repo}}<details class="frame gw-credential"><summary class="gw-section-head">Add or rotate upstream credential</summary><form class="gw-credform" hx-post="/policy/repo/credential" hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded"><input type="hidden" name="repo" value="{{.Repo}}"><label>New credential<input type="password" name="upstream_credential" autocomplete="new-password" placeholder="PAT / deploy token: overwrites the current credential"></label><button type="submit">Update credential</button><p class="gw-credform-note">Stored at <code>&lt;policy-root&gt;/{{.Repo}}/credential</code> with mode 0600. The previous value is gone after submit; no audit-log surface beyond a "credential-update" event with no payload.</p></form></details>{{end}}`))
	template.Must(t.New("timeEstimatesSection").Parse(`{{if and .AllowEdits .Repo}}<details class="frame gw-time-estimates"><summary class="gw-section-head">Time-prevented estimates</summary><p class="gw-te-hint">Hours of debugging saved per blocking finding, by frame tier. These weight the <a href="/stats">/stats</a> "time saved" estimate only - they do not affect gating. A tier left at its built-in number is marked "default".</p><form hx-post="/policy/repo/time-estimates" hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}' hx-encoding="application/x-www-form-urlencoded"><input type="hidden" name="repo" value="{{.Repo}}"><div class="gw-te-grid">{{range .TimeEstimates}}<label class="gw-te-row"><span class="gw-te-tier">Tier {{.Tier}}</span><input type="number" step="0.05" min="0" name="tier-{{.Tier}}" value="{{printf "%g" .Value}}"><span class="sub">{{if .IsDefault}}default{{else}}override{{end}}</span></label>{{end}}</div><button type="submit">Save estimates</button></form></details>{{end}}`))
	template.Must(t.New("scanBanner").Parse(`{{with .ScanRec}}{{if not .Dismissed}}<div class="frame gw-scan-banner"><h2>Scan recommendation</h2><div class="sub">scanned {{.ScannedAt}}</div><div>recommended: {{range .RecommendedGroups}}<span class="gw-group">{{.Name}}{{if not .Always}} (would flag {{.WouldFlag}}){{end}}</span> {{end}}</div>{{if $.AllowEdits}}<div class="gw-scan-actions"><button hx-post="/policy/repo/scan-apply" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-vals='{"name":"{{$.Repo}}"}'>Apply recommended</button><button hx-post="/policy/repo/scan-dismiss" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-vals='{"name":"{{$.Repo}}"}'>Dismiss</button><button hx-post="/policy/repo/scan-rescan" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-vals='{"name":"{{$.Repo}}"}'>Rescan now</button></div>{{end}}</div>{{end}}{{end}}`))
	template.Must(t.New("archivedPanel").Parse(`{{if .Archived}}<details class="frame gw-archived"><summary>Archived repos ({{len .Archived}})</summary>{{range .Archived}}<div class="gw-archived-row"><span>{{.}}</span>{{if $.AllowEdits}}<form hx-post="/policy/repo/restore" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}'><input type="hidden" name="name" value="{{.}}"><button type="submit">Restore</button></form><form hx-post="/policy/repo/delete" hx-headers='{"X-CSRF-Token":"{{$.CSRFToken}}"}' hx-confirm="Permanently delete {{.}}? This removes its bare repo (all git history) and policy/credential from the gateway. The upstream is untouched and other repos are unaffected. This cannot be undone."><input type="hidden" name="name" value="{{.}}"><button type="submit" class="danger">Delete permanently</button></form>{{end}}</div>{{end}}</details>{{end}}`))
	template.Must(t.New("editRepoForm").Parse(`<details class="frame gw-edit-repo"><summary class="gw-section-head">+ Edit existing repo in gateway</summary>{{if .Repos}}<form><label>Repo: <select name="repo" hx-get="/policy" hx-target="body" hx-swap="outerHTML"><option value="">(pick a repo to edit)</option>{{range .Repos}}<option value="{{.}}"{{if eq . $.Repo}} selected{{end}}>{{.}}</option>{{end}}</select></label></form>{{if .Repo}}<p class="warn">{{icon "warn"}} You&#39;re editing live config for <b>{{.Repo}}</b>. Frame toggle changes take effect on next push to this repo.</p>{{end}}{{else}}<p class="sub">No repos registered yet. Add one above.</p>{{end}}</details>`))
	template.Must(t.New("repoPicker").Parse(`<div class="gw-repo-picker">{{if .Repos}}<form style="display:inline"><label for="gw-policy-repo" style="font-size:12px;color:var(--gw-text-muted);margin-right:6px">Editing:</label><select name="repo" id="gw-policy-repo" data-policy-tab="{{.ActiveTab}}" onchange="if(this.value==='__repos'){window.location='/repos'}else if(this.value){var t=this.dataset.policyTab||'';var q='/policy?repo='+encodeURIComponent(this.value);if(t&&t!=='frames')q+='&tab='+encodeURIComponent(t);window.location=q}">{{$cur := .Repo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}<option value="__repos">Manage repos →</option></select></form>{{else}}<div class="frame gw-policy-empty"><p class="sub">No repos registered yet. <a href="/repos" style="color:var(--gw-accent)">Add one in Repos</a></p></div>{{end}}</div>`))
	template.Must(t.New("justRegisteredBanner").Parse(`{{if .JustRegistered}}<div class="gw-justregistered">{{icon "ok"}} <strong>{{.JustRegistered}}</strong> registered. Core kit applied. Refine selection below or go back to <a href="/repos">Repos</a>.</div>{{end}}`))
	template.Must(t.New("pageHeader").Parse(`{{if .Notice}}<div class="warn">{{icon "warn"}} {{.Notice}}</div>{{end}}
<h2 class="gw-pagehead">Policy</h2>
<p class="gw-pagedesc">Pick a repo to edit its frame policy. Manage repos at <a href="/repos" style="color:var(--gw-accent)">Repos</a>.</p>
{{template "justRegisteredBanner" .}}
{{template "scanBanner" .}}`))
	template.Must(t.New("pageTrailer").Parse(`{{if .Authoring}}<h3 class="gw-section-head">Custom linters</h3><p class="sub">Regex-based rules you authored, runs alongside the stdlib frames.</p>{{.Authoring}}{{end}}
{{if .NotifRail}}{{.NotifRail}}{{end}}
{{if .Whitelisted}}<h3 class="gw-section-head">Whitelist ({{len .Whitelisted}})</h3><table class="fr"><tr><td class="k">frame</td><td class="k">path</td><td class="k">reason</td>{{if .AllowEdits}}<td class="k"></td>{{end}}</tr>{{range .Whitelisted}}<tr><td>{{.Frame}}</td><td>{{.Path}}</td><td>{{.Reason}}</td>{{if $.AllowEdits}}<td><form style="display:inline"><input type="hidden" name="repo" value="{{$.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.Path}}"><button type="button" hx-post="/policy/whitelist/remove" hx-include="closest form" hx-target="next .wlrm-out" hx-swap="innerHTML">Remove</button></form><div class="wlrm-out"></div></td>{{end}}</tr>{{end}}</table>{{end}}`))
	template.Must(t.New("whitelistSection").Parse(`{{if .Whitelisted}}<h3 class="gw-section-head">Whitelist ({{len .Whitelisted}})</h3><p class="gw-policy-context">Editing existing repo: <strong>{{.Repo}}</strong></p><p class="sub">Per-finding exemptions: paths skip the named frame for the recorded reason.</p><table class="fr"><tr><td class="k">frame</td><td class="k">path</td><td class="k">reason</td>{{if .AllowEdits}}<td class="k"></td>{{end}}</tr>{{range .Whitelisted}}<tr><td>{{.Frame}}</td><td>{{.Path}}</td><td>{{.Reason}}</td>{{if $.AllowEdits}}<td><form style="display:inline"><input type="hidden" name="repo" value="{{$.Repo}}"><input type="hidden" name="frame" value="{{.Frame}}"><input type="hidden" name="path" value="{{.Path}}"><button type="button" hx-post="/policy/whitelist/remove" hx-include="closest form" hx-target="next .wlrm-out" hx-swap="innerHTML">Remove</button></form><div class="wlrm-out"></div></td>{{end}}</tr>{{end}}</table>{{else}}<h3 class="gw-section-head">Whitelist</h3><p class="gw-policy-context">Editing existing repo: <strong>{{.Repo}}</strong></p><p class="sub">No suppressions yet. Add one from a /feed row's "Whitelist" button when a frame fires on a legitimate exception.</p>{{end}}`))
	return t
}()

// policyPageData carries context for the policyTmpl peripheral sections.
// T10 replaces this with the full category-tree render data.
type policyPageData struct {
	Repo           string
	AllowEdits     bool
	CSRFToken      string
	Repos          []string
	ActiveTab      string // "frames" | "linters" | "whitelist"
	Enabled        []string
	Notice         string
	Authoring      template.HTML
	NotifRail      template.HTML // pre-rendered notification rail section (spec §7.4)
	ScanRec        *ScanRecommendation
	Archived       []string
	JustRegistered string
	Whitelisted    []whitelistRow
	UpstreamURL    string // current upstream, pre-fills the Edit repo settings form
	ProtectedRefs  string // current protected refs (space-joined), pre-fills the form
	TimeEstimates  []effectiveTimeEstimate // per-tier hours (override or default) for the editor
}

// renderPolicyPage writes the /policy page content fragment (without shell chrome)
// to w. Layout:
//  1. Page notice + heading + just-registered banner + scan banner
//  2. Active repo section: picker + selected repo + currently enabled summary
//  3. Per-repo editing sections: custom linters, whitelist
//  4. Frame selection (kit chips + browse tree) - gated on a repo being selected
func renderPolicyPage(w io.Writer, vm policyVM, opts policyPageOpts) error {
	activeTab := opts.ActiveTab
	if activeTab != "linters" && activeTab != "whitelist" {
		activeTab = "frames"
	}
	data := policyPageData{
		Repo:           vm.Repo,
		AllowEdits:     opts.AllowEdits,
		CSRFToken:      opts.CSRFToken,
		Repos:          opts.Repos,
		ActiveTab:      activeTab,
		Enabled:        vm.Enabled,
		Notice:         opts.Notice,
		Authoring:      opts.Authoring,
		NotifRail:      opts.NotifRail,
		ScanRec:        opts.ScanRec,
		Archived:       opts.Archived,
		JustRegistered: opts.JustRegistered,
	}
	if vm.Repo != "" && opts.PolicyRoot != "" {
		wlPath := filepath.Join(opts.PolicyRoot, vm.Repo, ".appframes", "_canonical", "whitelist.toml")
		known := map[string]bool{}
		for id := range stdlibFrameByID() {
			known[id] = true
		}
		if wl, err := whitelist.Load(wlPath, known, time.Now().UTC()); err == nil && wl != nil {
			for _, ev := range wl.Entries() {
				data.Whitelisted = append(data.Whitelisted, whitelistRow{Frame: ev.Frame, Path: ev.Path, Reason: ev.Reason})
			}
		}
		// Pre-fill the Edit-repo-settings form from the current policy.
		if p, err := (gateway.FilePolicyStore{Root: opts.PolicyRoot}).Load(vm.Repo); err == nil {
			data.UpstreamURL = p.UpstreamURL
			data.ProtectedRefs = strings.Join(p.ProtectedRefs, " ")
		}
		// Resolve per-tier time estimates (override or built-in default) for the editor.
		if te, err := resolveTimeEstimates(opts.PolicyRoot, vm.Repo); err == nil {
			data.TimeEstimates = te
		}
	}

	// Section 1: page notice + heading + just-registered banner + scan banner.
	var header bytes.Buffer
	if err := policyTmpl.ExecuteTemplate(&header, "pageHeader", data); err != nil {
		return err
	}
	if _, err := w.Write(header.Bytes()); err != nil {
		return err
	}

	// Section 2: tab strip (when a repo is selected) - tabs first so the
	// page's conceptual structure reads top-down (this page is about Frames /
	// Custom linters / Whitelist), then the active-repo bar appears under
	// the tabs as a "you are editing X" context line for the tab content.
	if vm.Repo != "" {
		fmt.Fprint(w, policyTabStrip(activeTab, vm.Repo))
	}

	// Section 3: active repo picker + selected repo line + enabled summary.
	// Sits between the tabs and the tab content so the "which repo am I
	// editing?" question is answered right next to the content.
	renderActiveRepoSection(w, vm, data)

	// Time-prevented estimates editor: per-repo tier-hour overrides for the
	// /stats time-saved model. Backend shipped in v0.1.0; the render was never
	// wired in the category-tree rewrite, so reconnect it here.
	if vm.Repo != "" && opts.AllowEdits {
		var teBuf bytes.Buffer
		if err := policyTmpl.ExecuteTemplate(&teBuf, "timeEstimatesSection", data); err != nil {
			return err
		}
		if _, err := w.Write(teBuf.Bytes()); err != nil {
			return err
		}
	}

	// Section 4: tab content (only the active tab).
	if vm.Repo != "" {
		switch activeTab {
		case "linters":
			if data.Authoring != "" {
				fmt.Fprintf(w, `<h3 class="gw-section-head">Custom linters</h3><p class="gw-policy-context">Editing existing repo: <strong>%s</strong></p><p class="sub">Regex-based rules you authored, runs alongside the stdlib frames.</p>`, htmlEsc(vm.Repo))
				if _, err := w.Write([]byte(data.Authoring)); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(w, `<h3 class="gw-section-head">Custom linters</h3><p class="gw-policy-context">Editing existing repo: <strong>%s</strong></p><p class="sub">Read-only mode or no linters yet. Start the dashboard with <code>--allow-edits</code> to author one.</p>`, htmlEsc(vm.Repo))
			}
		case "whitelist":
			var buf bytes.Buffer
			if err := policyTmpl.ExecuteTemplate(&buf, "whitelistSection", data); err != nil {
				return err
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return err
			}
		default: // "frames"
			if err := renderFrameSelection(w, vm); err != nil {
				return err
			}
		}
	} else if len(opts.Repos) > 0 {
		if _, err := fmt.Fprint(w, `<section class="gw-policy gw-policy-empty"><h2>Frame selection</h2><p class="sub">Select a repo above to edit its frame policy.</p></section>`); err != nil {
			return err
		}
	}
	return nil
}

// policyTabStrip renders the Frames / Custom linters / Whitelist tab header.
// activeTab is "frames", "linters", or "whitelist".
func policyTabStrip(activeTab, repo string) string {
	cls := func(t string) string {
		if t == activeTab {
			return "autopr-tab active"
		}
		return "autopr-tab"
	}
	q := "?repo=" + repo
	return `<style>
.autopr-tabs{display:flex;gap:2px;margin:18px 0;border-bottom:1px solid var(--gw-border);padding:0}
.autopr-tab{display:inline-block;padding:10px 18px;color:var(--gw-text-muted);text-decoration:none;font-size:13px;font-weight:500;border-bottom:2px solid transparent;margin-bottom:-1px}
.autopr-tab:hover{color:var(--gw-text)}
.autopr-tab.active{color:var(--gw-accent);border-bottom-color:var(--gw-accent);font-weight:600}
</style>
<nav class="autopr-tabs">
<a href="/policy` + q + `" class="` + cls("frames") + `">Frames</a>
<a href="/policy` + q + `&amp;tab=linters" class="` + cls("linters") + `">Custom linters</a>
<a href="/policy` + q + `&amp;tab=whitelist" class="` + cls("whitelist") + `">Whitelist</a>
</nav>`
}

// renderActiveRepoSection emits the Active repo block: the compact repo picker
// followed by the selected-repo line and the currently-enabled-frames summary
// (when a repo is selected), or an empty-state prompt linking to /repos.
// The repo-header div (id="repo-header") is included here as the htmx OOB
// swap target for the /policy/repo toggle handler.
func renderActiveRepoSection(w io.Writer, vm policyVM, data policyPageData) {
	fmt.Fprint(w, `<section class="frame">`)
	fmt.Fprint(w, `<h3 class="gw-section-head">Active repo</h3>`)
	var picker bytes.Buffer
	_ = policyTmpl.ExecuteTemplate(&picker, "repoPicker", data)
	_, _ = w.Write(picker.Bytes())
	if vm.Repo != "" {
		fmt.Fprintf(w, `<div id="repo-header"><p class="sub">Selected: <strong>%s</strong></p></div>`, htmlEsc(vm.Repo))
		renderEnabledSummary(w, vm)
	} else if len(data.Repos) == 0 {
		fmt.Fprint(w, `<p class="sub">No repo selected. Pick one above or register a new one at <a href="/repos">Repos</a>.</p>`)
	}
	fmt.Fprint(w, `</section>`)
}

// renderAppliedKitsRow emits the "Currently applied kits:" row with stable ID
// for OOB swap targeting on frame toggle.
func renderAppliedKitsRow(w io.Writer, vm policyVM) {
	fmt.Fprint(w, `<div id="gw-policy-kits-row" class="gw-policy-applied">Currently applied kits:`)
	n := computeAppliedKitPills(w, vm)
	if n == 0 {
		fmt.Fprint(w, ` <span class="sub">none</span>`)
	}
	fmt.Fprint(w, `</div>`)
}

// renderAppliedGroupsRow emits the "Currently applied groups:" row with stable
// ID for OOB swap targeting. Returns the total which the caller uses for the
// total-line (== unique enabled count).
func renderAppliedGroupsRow(w io.Writer, vm policyVM) int {
	fmt.Fprint(w, `<div id="gw-policy-groups-row" class="gw-policy-applied">Currently applied groups:`)
	n := computeAppliedGroupPills(w, vm)
	if n == 0 {
		fmt.Fprint(w, ` <span class="sub">none</span>`)
	}
	fmt.Fprint(w, `</div>`)
	return n
}

// renderTotalLine emits the wrapper div with id="gw-policy-total-row".
// The ID makes it OOB-swappable by toggleFrame.
func renderTotalLine(w io.Writer, total int) {
	switch {
	case total == 0:
		fmt.Fprint(w, `<div id="gw-policy-total-row" class="gw-policy-total"></div>`)
	case total == 1:
		fmt.Fprint(w, `<div id="gw-policy-total-row" class="gw-policy-total">1 frame enabled</div>`)
	default:
		fmt.Fprintf(w, `<div id="gw-policy-total-row" class="gw-policy-total">%d frames enabled</div>`, total)
	}
}

// renderEnabledSummary writes a compact, read-only summary of the active repo's
// enabled frames grouped Category → Subcategory → frame ID. Gives the operator
// a quick at-a-glance "what's actually on for this repo" view next to the Edit
// picker; the full editable browse tree (with checkboxes) lives at the page
// bottom and is the place to mutate state. Skips Platform/Framework
// cross-listing - frames already counted under their primary category.
func renderEnabledSummary(w io.Writer, vm policyVM) {
	if len(vm.Enabled) == 0 {
		fmt.Fprint(w, `<details class="frame gw-enabled-summary"><summary class="gw-section-head">Currently enabled frames</summary><p class="sub">No frames enabled for this repo yet. Apply a kit or tick frames in the Frame selection section below.</p></details>`)
		return
	}
	enabledSet := map[string]bool{}
	for _, id := range vm.Enabled {
		enabledSet[id] = true
	}
	fmt.Fprintf(w, `<details class="frame gw-enabled-summary"><summary class="gw-section-head">Currently enabled frames (%d)</summary>`, len(vm.Enabled))
	any := false
	for _, c := range vm.Categories {
		// v2 single-axis placement: each frame appears under exactly one
		// axis (no cross-listings). The legacy v1 skip of platform+framework
		// here would now drop platform-tagged frames entirely.
		type subRow struct {
			display string
			ids     []string
			depth   int
		}
		var subs []subRow
		catCount := 0
		var walk func(parentDisplay string, sub policySubcategory, depth int)
		walk = func(parentDisplay string, sub policySubcategory, depth int) {
			display := sub.Display
			if parentDisplay != "" {
				display = parentDisplay + " / " + sub.Display
			}
			var ids []string
			for _, fr := range sub.Frames {
				if enabledSet[fr.ID] {
					ids = append(ids, fr.ID)
				}
			}
			if len(ids) > 0 {
				subs = append(subs, subRow{display: display, ids: ids, depth: depth})
				catCount += len(ids)
			}
			for _, child := range sub.Children {
				walk(sub.Display, child, depth+1)
			}
		}
		for _, sub := range c.Subcategories {
			walk("", sub, 0)
		}
		if catCount == 0 {
			continue
		}
		any = true
		fmt.Fprintf(w, `<div class="gw-summary-cat"><div class="gw-summary-cat-head">%s <span class="gw-summary-count">%d</span></div>`, htmlEsc(c.Display), catCount)
		for _, s := range subs {
			fmt.Fprintf(w, `<div class="gw-summary-sub gw-summary-sub-d%d"><div class="gw-summary-sub-name">%s</div><ul class="gw-summary-frames">`, s.depth, htmlEsc(s.display))
			for _, id := range s.ids {
				fmt.Fprintf(w, `<li><code>%s</code></li>`, htmlEsc(id))
			}
			fmt.Fprint(w, `</ul></div>`)
		}
		fmt.Fprint(w, `</div>`)
	}
	if !any {
		fmt.Fprint(w, `<p class="sub">Enabled frame IDs don't match any known stdlib category: likely legacy <code>@</code>-prefixed entries. Apply a kit below to seed a clean per-repo selection.</p>`)
	}
	fmt.Fprint(w, `</details>`)
}

// renderFrameSelection writes the kit chips rows, quick-start row, and browse tree.
func renderFrameSelection(w io.Writer, vm policyVM) error {
	fmt.Fprintf(w, `<section class="gw-policy">
  <h2 class="gw-section-head">Frame selection</h2>
  <p class="gw-policy-context">Editing existing repo: <strong>%s</strong></p>

  <div class="gw-policy-kits">
    `, htmlEsc(vm.Repo))

	renderAppliedKitsRow(w, vm)
	fmt.Fprint(w, `
    `)
	total := renderAppliedGroupsRow(w, vm)

	fmt.Fprint(w, `

    <div class="gw-policy-quickstart">Quick start:`)
	// Custom-kit creation button first, then built-in kit apply buttons.
	// Linter authoring stays on the Custom linters tab to keep the mental
	// model clean - Frames tab manages frames, Linters tab manages linters.
	fmt.Fprintf(w, `
      <button class="gw-kit-new" hx-get="/policy/kits/new-form" hx-vals='{"repo":%q}' hx-target="#gw-userkit-formslot" hx-swap="innerHTML">+ New custom kit</button>`, vm.Repo)
	for _, k := range vm.BuiltinKits {
		disabled := ""
		if k.FullyApplied {
			disabled = ` disabled aria-pressed="true"`
		}
		fmt.Fprintf(w, `
      <button class="gw-kit-apply" hx-post="/policy/kits/apply" hx-vals='{"name":%q,"repo":%q}'%s>Apply %s</button>`,
			k.Name, vm.Repo, disabled, htmlEsc(k.Display))
	}
	fmt.Fprint(w, `
    </div>
    <div id="gw-userkit-formslot"></div>`)
	// Total frame count on its own line below quick-start: groups row sum == unique enabled count.
	renderTotalLine(w, total)
	fmt.Fprint(w, `
  </div>`)
	fmt.Fprint(w, `
  <div class="gw-policy-browse">`)

	// Custom user kits FIRST so freshly-created kits are immediately visible
	// at the top of the browse area, not buried below the 9 stdlib categories.
	// Wrapped under a "Custom Kits" subhead so the two domains (operator-
	// authored kits vs stdlib categories) are visually distinct.
	if len(vm.UserKits) > 0 {
		fmt.Fprint(w, `
    <h3 class="gw-browse-section-head">Custom Kits</h3>`)
	}
	for _, uk := range vm.UserKits {
		confirmMsg := "Delete the custom kit '" + uk.Name + "' and remove its frames from this repo's gating policy?"
		fmt.Fprintf(w, `
    <details class="gw-cat gw-user-cat">
      <summary>
        <span>%s <span class="sub">(custom kit)</span></span>
        <button type="button" class="gw-userkit-delete" hx-post="/policy/userkits/clear" hx-vals='{"name":%q,"repo":%q}' hx-confirm="%s" onclick="event.stopPropagation()">Delete</button>
      </summary>
      <ul class="gw-frames">`, htmlEsc(uk.Name), uk.Name, vm.Repo, htmlEsc(confirmMsg))
		for _, fr := range uk.Frames {
			checked := ""
			if fr.Enabled {
				checked = " checked"
			}
			fmt.Fprintf(w, `
        <li>
          <label>
            <input type="checkbox" name="enabled"%s
              hx-post="/policy/frames/toggle"
              hx-swap="none"
              hx-vals='{"id":%q,"repo":%q}'>
            <code>%s</code>
          </label>
        </li>`, checked, fr.ID, vm.Repo, htmlEsc(fr.ID))
		}
		fmt.Fprint(w, `
      </ul>
    </details>`)
	}

	if len(vm.Categories) > 0 {
		fmt.Fprint(w, `
    <h3 class="gw-browse-section-head">System Default Groups</h3>`)
	}
	var renderSub func(sub policySubcategory)
	renderSub = func(sub policySubcategory) {
		fmt.Fprintf(w, `
      <details class="gw-sub">
        <summary>%s</summary>
        <ul class="gw-frames">`, htmlEsc(sub.Display))
		for _, fr := range sub.Frames {
			checked := ""
			if fr.Enabled {
				checked = " checked"
			}
			fmt.Fprintf(w, `
          <li id=%q>
            <label>
              <input type="checkbox" name="enabled"%s
                hx-post="/policy/frames/toggle"
                hx-swap="none"
                hx-vals='{"id":%q,"repo":%q}'>
              <code>%s</code>
              <span class="gw-fr-sev gw-sev-%s">%s</span>
            </label>
          </li>`,
				"fr-"+frameDOMID(fr.ID), checked, fr.ID, vm.Repo,
				htmlEsc(fr.ID), strings.ToLower(fr.Severity), fr.Severity)
		}
		fmt.Fprint(w, `
        </ul>`)
		for _, child := range sub.Children {
			renderSub(child)
		}
		fmt.Fprint(w, `
      </details>`)
	}
	for _, c := range vm.Categories {
		fmt.Fprintf(w, `
    <details class="gw-cat">
      <summary>%s</summary>`, htmlEsc(c.Display))
		for _, sub := range c.Subcategories {
			renderSub(sub)
		}
		fmt.Fprint(w, `
    </details>`)
	}

	fmt.Fprint(w, `
  </div>
</section>`)
	return nil
}

// computeAppliedKitPills emits one pill per applied kit (custom user kits first,
// then built-in kits) showing the count of THAT KIT's frames currently enabled.
// Pills with count 0 are omitted. The sum may exceed unique-enabled-count if
// applied kits overlap - the total-line uses the groups row, not this.
func computeAppliedKitPills(w io.Writer, vm policyVM) int {
	enabledSet := map[string]bool{}
	for _, id := range vm.Enabled {
		enabledSet[id] = true
	}
	total := 0
	// Custom user kits first.
	for _, uk := range vm.UserKits {
		n := 0
		for _, fr := range uk.Frames {
			if enabledSet[fr.ID] {
				n++
			}
		}
		if n > 0 {
			// Same destructive shape as the in-tree Delete button: drops the
			// kit definition AND unticks frames. hx-confirm makes that
			// explicit when the operator clicks the pill ×.
			confirmMsg := "Delete the custom kit '" + uk.Name + "' and remove its frames from this repo's gating policy?"
			fmt.Fprintf(w, `
      <span class="gw-kit-chip gw-custom-pill" data-applied-userkit=%q>
        %s <span class="gw-kit-count">%d</span>
        <button class="gw-kit-clear" hx-post="/policy/userkits/clear" hx-vals='{"name":%q,"repo":%q}' hx-confirm="%s" aria-label="Clear %s">×</button>
      </span>`, uk.Name, htmlEsc(uk.Name), n, uk.Name, vm.Repo, htmlEsc(confirmMsg), htmlEsc(uk.Name))
			total += n
		}
	}
	// Built-in applied kits next.
	ks, _ := kits.LoadStdlib()
	for _, kitName := range vm.AppliedKits {
		display := kitName
		for _, bk := range vm.BuiltinKits {
			if bk.Name == kitName {
				display = bk.Display
				break
			}
		}
		var kitFrames []string
		if k, ok := ks.Get(kitName); ok {
			kitFrames = k.Frames
		}
		n := 0
		for _, fid := range kitFrames {
			if enabledSet[fid] {
				n++
			}
		}
		if n > 0 {
			fmt.Fprintf(w, `
      <span class="gw-kit-chip" data-applied-kit=%q>
        %s <span class="gw-kit-count">%d</span>
        <button class="gw-kit-clear" hx-post="/policy/kits/clear" hx-vals='{"name":%q,"repo":%q}' aria-label="Clear %s">×</button>
      </span>`, kitName, htmlEsc(display), n, kitName, vm.Repo, htmlEsc(display))
			total += n
		}
	}
	return total
}

// computeAppliedGroupPills emits one pill per category that has at least one
// enabled frame - counting ALL enabled frames in that category regardless of
// kit membership. Returns the sum of pill counts == unique enabled frame
// count (every frame has exactly one primary category). Platform/Framework
// cross-listings are skipped (they duplicate frames already counted under
// the frame's primary category).
func computeAppliedGroupPills(w io.Writer, vm policyVM) int {
	enabledSet := map[string]bool{}
	for _, id := range vm.Enabled {
		enabledSet[id] = true
	}
	total := 0
	var countEnabled func(sub policySubcategory) int
	countEnabled = func(sub policySubcategory) int {
		n := 0
		for _, fr := range sub.Frames {
			if enabledSet[fr.ID] {
				n++
			}
		}
		for _, child := range sub.Children {
			n += countEnabled(child)
		}
		return n
	}
	for _, c := range vm.Categories {
		// v2 single-axis placement: count all axes including platform and
		// framework. The legacy v1 skip here would silently drop platform
		// enabled-frame counts from the per-axis pill.
		n := 0
		for _, sub := range c.Subcategories {
			n += countEnabled(sub)
		}
		if n > 0 {
			fmt.Fprintf(w, `
      <span class="gw-kit-chip gw-cat-pill" data-applied-category=%q>
        %s <span class="gw-kit-count">%d</span>
        <button class="gw-kit-clear" hx-post="/policy/category/clear" hx-vals='{"category":%q,"repo":%q}' aria-label="Clear %s">×</button>
      </span>`, c.ID, htmlEsc(c.Display), n, c.ID, vm.Repo, htmlEsc(c.Display))
			total += n
		}
	}
	return total
}

// renderPolicyHTTP is the HTTP entry point: renders content into a buffer, then
// wraps it in the gateway shell and sends the response.
func renderPolicyHTTP(w http.ResponseWriter, vm policyVM, opts policyPageOpts) {
	var buf bytes.Buffer
	if err := renderPolicyPage(&buf, vm, opts); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderGwShell(w, gwLayout{Title: "policy - " + vm.Repo, CSRFToken: opts.CSRFToken, Chrome: opts.Chrome, Content: template.HTML(buf.String())})
}

// renderFrameRow renders a single frame row partial after a severity change.
func renderFrameRow(w http.ResponseWriter, repo, frameID, sev string, saved bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if saved {
		_, _ = w.Write([]byte(`<span class="ok">saved ` + string(gwicons.HTML("ok")) + ` ` + frameID + ` ` + sev + `</span>`))
	}
}

// renderRepoHeader renders the repo-header partial.
func renderRepoHeader(w http.ResponseWriter, repo string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<div id="repo-header"><h2>` + html.EscapeString(repo) + `</h2></div>`))
}

func kitDisplay(vm policyVM, name string) string {
	for _, k := range vm.BuiltinKits {
		if k.Name == name {
			return k.Display
		}
	}
	return name
}

func frameDOMID(id string) string {
	return strings.NewReplacer("/", "-").Replace(id)
}

func htmlEsc(s string) string {
	return html.EscapeString(s)
}

type policyHandlers struct {
	policyRoot string
	token      string
}

func validSeverity(s string) bool { return s == "BLOCK" || s == "WARN" || s == "INFO" }

func (h policyHandlers) severity(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo, frame, sev := r.FormValue("repo"), r.FormValue("frame"), r.FormValue("severity")
	fp, err := gateway.LoadFramePolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, "no such repo", http.StatusBadRequest)
		return
	}
	if !validSeverity(sev) {
		http.Error(w, "invalid severity", http.StatusBadRequest)
		return
	}
	// Validate that the frame is in the repo's enabled list.
	if !enabledMatch(frame, fp.Enabled) {
		http.Error(w, "frame not in this repo's policy", http.StatusBadRequest)
		return
	}
	if err := fp.WithSeverity(frame, sev).Save(h.policyRoot, repo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "frame-severity", Repo: repo, OK: true,
		Payload: map[string]any{"frame": frame, "severity": sev},
	})
	renderFrameRow(w, repo, frame, sev, true)
}

func (h policyHandlers) repo(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	repo := r.FormValue("repo")
	enabled := r.FormValue("enabled") == "1"
	if err := (gateway.FilePolicyStore{Root: h.policyRoot}).SetEnabled(repo, enabled); err != nil {
		http.Error(w, "no such repo", http.StatusBadRequest)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event: "repo-toggle", Repo: repo, OK: true,
		Payload: map[string]any{"enabled": enabled},
	})
	renderRepoHeader(w, repo)
}

func (h policyHandlers) toggleFrame(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	enabled, err := readEnabledFromConfig(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Toggle: add if absent, remove if present.
	add := !containsStr(enabled, id)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updated, _, err := rewriteEnabledList(string(data), id, add)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := atomicWriteFile(cfgPath, []byte(updated)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// OOB swap: re-render both pill rows + total so counts update without a full
	// page refresh (which would close the user's open <details>).
	fp, _ := gateway.LoadFramePolicy(h.policyRoot, repo)
	vm := buildPolicyView(h.policyRoot, repo, fp.Enabled)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// OOB 1: kits row
	fmt.Fprint(w, `<div id="gw-policy-kits-row" hx-swap-oob="outerHTML" class="gw-policy-applied">Currently applied kits:`)
	kn := computeAppliedKitPills(w, vm)
	if kn == 0 {
		fmt.Fprint(w, ` <span class="sub">none</span>`)
	}
	fmt.Fprint(w, `</div>`)

	// OOB 2: groups row
	fmt.Fprint(w, `<div id="gw-policy-groups-row" hx-swap-oob="outerHTML" class="gw-policy-applied">Currently applied groups:`)
	gn := computeAppliedGroupPills(w, vm)
	if gn == 0 {
		fmt.Fprint(w, ` <span class="sub">none</span>`)
	}
	fmt.Fprint(w, `</div>`)

	// OOB 3: total (groups row sum == unique enabled count)
	fmt.Fprint(w, `<div id="gw-policy-total-row" hx-swap-oob="outerHTML" class="gw-policy-total">`)
	switch {
	case gn == 1:
		fmt.Fprint(w, `1 frame enabled`)
	case gn > 1:
		fmt.Fprintf(w, `%d frames enabled`, gn)
	}
	fmt.Fprint(w, `</div>`)
}

func (h policyHandlers) applyKit(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	name := r.PostForm.Get("name")
	ks, err := kits.LoadStdlib()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	k, ok := ks.Get(name)
	if !ok {
		http.Error(w, "unknown kit", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc := string(data)
	for _, id := range k.Frames {
		doc, _, err = rewriteEnabledList(doc, id, true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := atomicWriteFile(cfgPath, []byte(doc)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := addAppliedKit(cfgPath, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func (h policyHandlers) userKitForm(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	allFrames, _ := stdlib.Load()
	fmt.Fprintf(w, `<form class="gw-userkit-form" hx-post="/policy/kits/create" hx-target="closest .gw-policy-kits" hx-swap="outerHTML">
  <input type="hidden" name="repo" value=%q>
  <label>Name: <input type="text" name="name" required></label>
  <div class="gw-userkit-pick"><strong>Frames:</strong>`, repo)
	for _, f := range allFrames {
		fmt.Fprintf(w, `
    <label><input type="checkbox" name="frames" value=%q> <code>%s</code></label>`,
			f.ID(), htmlEsc(f.ID()))
	}
	fmt.Fprintf(w, `
  </div>
  <button type="submit">Create kit</button>
  <button type="button" onclick="document.getElementById('gw-userkit-formslot').innerHTML=''">Cancel</button>
</form>`)
}

func (h policyHandlers) createUserKit(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostForm.Get("name"))
	frames := r.PostForm["frames"]
	if name == "" || len(frames) == 0 {
		http.Error(w, "name and at least one frame required", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := addUserKit(cfgPath, name, frames); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (h policyHandlers) deleteUserKitHandler(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	name := r.PostForm.Get("name")
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if err := deleteUserKit(cfgPath, name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("HX-Refresh", "true")
}

func (h policyHandlers) clearKit(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	name := r.PostForm.Get("name")
	ks, _ := kits.LoadStdlib()
	k, ok := ks.Get(name)
	if !ok {
		http.Error(w, "unknown kit", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	doc := string(data)
	for _, id := range k.Frames {
		var iterErr error
		doc, _, iterErr = rewriteEnabledList(doc, id, false)
		if iterErr != nil {
			http.Error(w, iterErr.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := atomicWriteFile(cfgPath, []byte(doc)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := removeAppliedKit(cfgPath, name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// clearCustomKit unticks every frame belonging to the named custom kit and
// drops the [[ui.user_kits]] entry in a single atomic write.
func (h policyHandlers) clearCustomKit(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	name := r.PostForm.Get("name")
	if name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	cfg, err := readFullConfig(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var kitFrames []string
	remaining := cfg.UI.UserKits[:0]
	for _, uk := range cfg.UI.UserKits {
		if uk.Name == name {
			kitFrames = uk.Frames
		} else {
			remaining = append(remaining, uk)
		}
	}
	if kitFrames == nil {
		http.Error(w, "unknown custom kit", http.StatusBadRequest)
		return
	}
	cfg.UI.UserKits = remaining
	dropSet := map[string]bool{}
	for _, fid := range kitFrames {
		dropSet[fid] = true
	}
	out := cfg.Frames.Enabled[:0]
	for _, fid := range cfg.Frames.Enabled {
		if !dropSet[fid] {
			out = append(out, fid)
		}
	}
	cfg.Frames.Enabled = out
	if err := writeFullConfig(cfgPath, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

// clearCategory unticks every enabled frame whose primary category matches catID.
func (h policyHandlers) clearCategory(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	catID := r.PostForm.Get("category")
	if catID == "" {
		http.Error(w, "missing category", http.StatusBadRequest)
		return
	}
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	cfg, err := readFullConfig(cfgPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allFrames, err := stdlib.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	drop := map[string]bool{}
	for _, f := range allFrames {
		if string(f.Frontmatter.Category) == catID {
			drop[f.ID()] = true
		}
	}
	out := cfg.Frames.Enabled[:0]
	for _, fid := range cfg.Frames.Enabled {
		if !drop[fid] {
			out = append(out, fid)
		}
	}
	cfg.Frames.Enabled = out
	if err := writeFullConfig(cfgPath, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("HX-Refresh", "true")
	w.WriteHeader(http.StatusOK)
}

func readEnabledFromConfig(cfgPath string) ([]string, error) {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Frames struct {
			Enabled []string `toml:"enabled"`
		} `toml:"frames"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return cfg.Frames.Enabled, nil
}

func addAppliedKit(cfgPath, name string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var cfg struct {
		UI struct {
			AppliedKits []string `toml:"applied_kits"`
		} `toml:"ui"`
	}
	_ = toml.Unmarshal(data, &cfg)
	for _, existing := range cfg.UI.AppliedKits {
		if existing == name {
			return nil
		}
	}
	cfg.UI.AppliedKits = append(cfg.UI.AppliedKits, name)
	return rewriteAppliedKits(cfgPath, cfg.UI.AppliedKits)
}

func removeAppliedKit(cfgPath, name string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var cfg struct {
		UI struct {
			AppliedKits []string `toml:"applied_kits"`
		} `toml:"ui"`
	}
	_ = toml.Unmarshal(data, &cfg)
	out := cfg.UI.AppliedKits[:0]
	for _, n := range cfg.UI.AppliedKits {
		if n != name {
			out = append(out, n)
		}
	}
	return rewriteAppliedKits(cfgPath, out)
}

var appliedKitsRe = regexp.MustCompile(`(?m)^applied_kits\s*=\s*\[[^\]]*\]`)

func rewriteAppliedKits(cfgPath string, names []string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("applied_kits = [")
	for i, n := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", n)
	}
	b.WriteString("]")
	newLine := b.String()

	out := string(data)
	if appliedKitsRe.MatchString(out) {
		out = appliedKitsRe.ReplaceAllString(out, newLine)
	} else if strings.Contains(out, "[ui]") {
		out = strings.Replace(out, "[ui]\n", "[ui]\n"+newLine+"\n", 1)
	} else {
		out += "\n[ui]\n" + newLine + "\n"
	}
	return atomicWriteFile(cfgPath, []byte(out))
}

func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func containsStr(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
