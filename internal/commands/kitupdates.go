// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway"
	"nimblegate/internal/kits"
	"nimblegate/internal/stdlib"
)

// kitNewFrames is one applied kit that gained frames since the repo applied
// it: frames in the kit that are neither enabled nor already offered.
type kitNewFrames struct {
	Kit     string // kit name, e.g. "core"
	Display string // e.g. "Core"
	Frames  []kitNewFrame
}

type kitNewFrame struct {
	ID    string
	Title string // the frame doc's heading, e.g. "No secret in client env"
}

// readSeenFrames reads [ui] seen_frames: kit frames the operator has already
// been offered (by applying the kit, adding them, or dismissing the notice).
func readSeenFrames(cfgPath string) []string {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil
	}
	var cfg struct {
		UI struct {
			SeenFrames []string `toml:"seen_frames"`
		} `toml:"ui"`
	}
	_ = toml.Unmarshal(data, &cfg)
	return cfg.UI.SeenFrames
}

// markFramesSeen adds ids to [ui] seen_frames, keeping the list sorted and
// free of duplicates.
func markFramesSeen(cfgPath string, ids []string) error {
	set := map[string]bool{}
	for _, id := range readSeenFrames(cfgPath) {
		set[id] = true
	}
	changed := false
	for _, id := range ids {
		if !set[id] {
			set[id] = true
			changed = true
		}
	}
	if !changed {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return rewriteUIList(cfgPath, "seen_frames", out)
}

// rewriteUIList replaces (or adds) one `key = [...]` list under [ui],
// leaving the rest of the file as it is.
func rewriteUIList(cfgPath, key string, values []string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(key + " = [")
	for i, v := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", v)
	}
	b.WriteString("]")
	line := b.String()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + `\s*=\s*\[[^\]]*\]`)
	out := string(data)
	switch {
	case re.MatchString(out):
		out = re.ReplaceAllLiteralString(out, line)
	case strings.Contains(out, "\n[ui]\n") || strings.HasPrefix(out, "[ui]\n"):
		out = strings.Replace(out, "[ui]\n", "[ui]\n"+line+"\n", 1)
	default:
		out = strings.TrimRight(out, "\n") + "\n\n[ui]\n" + line + "\n"
	}
	return atomicWriteFile(cfgPath, []byte(out))
}

// newKitFrames lists, per applied built-in kit, the kit frames this repo has
// never been offered. A frame the operator unticked after applying is in
// seen_frames, so it is not offered again.
func newKitFrames(policyRoot, repo string) []kitNewFrames {
	cfgPath := filepath.Join(policyRoot, repo, "appframes.toml")
	applied := readAppliedKits(policyRoot, repo)
	if len(applied) == 0 {
		return nil
	}
	ks, err := kits.LoadStdlib()
	if err != nil {
		return nil
	}
	fp, err := gateway.LoadFramePolicy(policyRoot, repo)
	if err != nil {
		return nil
	}
	known := map[string]bool{}
	for _, id := range fp.Enabled {
		known[id] = true
	}
	for _, id := range readSeenFrames(cfgPath) {
		known[id] = true
	}
	var titles map[string]string
	var out []kitNewFrames
	offered := map[string]bool{}
	for _, name := range applied {
		k, ok := ks.Get(name)
		if !ok {
			continue
		}
		var add []kitNewFrame
		for _, id := range k.Frames {
			if known[id] || offered[id] {
				continue
			}
			if titles == nil {
				titles = frameTitles()
			}
			offered[id] = true
			add = append(add, kitNewFrame{ID: id, Title: titles[id]})
		}
		if len(add) > 0 {
			out = append(out, kitNewFrames{Kit: k.Name, Display: k.Display, Frames: add})
		}
	}
	return out
}

func countNewKitFrames(groups []kitNewFrames) int {
	n := 0
	for _, g := range groups {
		n += len(g.Frames)
	}
	return n
}

func newKitFrameIDs(groups []kitNewFrames) []string {
	var ids []string
	for _, g := range groups {
		for _, f := range g.Frames {
			ids = append(ids, f.ID)
		}
	}
	return ids
}

// frameTitles maps frame ID to the first `# ` heading of its doc.
func frameTitles() map[string]string {
	out := map[string]string{}
	all, _ := stdlib.Load()
	for _, f := range all {
		for _, line := range strings.Split(f.Body, "\n") {
			if strings.HasPrefix(line, "# ") {
				out[f.ID()] = strings.TrimSpace(line[2:])
				break
			}
		}
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// renderKitUpdateNotice writes the "kit gained frames" notice on the Policy
// page. Read-only dashboards get the text without buttons.
func renderKitUpdateNotice(w io.Writer, vm policyVM) {
	if len(vm.NewKitFrames) == 0 {
		return
	}
	n := countNewKitFrames(vm.NewKitFrames)
	fmt.Fprint(w, `
    <div class="gw-kit-update" id="gw-kit-update">`)
	for _, g := range vm.NewKitFrames {
		fmt.Fprintf(w, `
      <p><strong>%s has %d new %s</strong> since this repo applied it. Nothing changes until you add %s.</p>
      <ul>`, htmlEsc(g.Display), len(g.Frames), plural(len(g.Frames), "frame", "frames"), plural(len(g.Frames), "it", "them"))
		for _, f := range g.Frames {
			title := ""
			if f.Title != "" {
				title = " - " + htmlEsc(f.Title)
			}
			fmt.Fprintf(w, `
        <li><a href="/frames?id=%s&amp;repo=%s"><code>%s</code></a>%s</li>`,
				url.QueryEscape(f.ID), url.QueryEscape(vm.Repo), htmlEsc(f.ID), title)
		}
		fmt.Fprint(w, `
      </ul>`)
	}
	if vm.ReadOnly {
		fmt.Fprint(w, `
      <p class="sub">Read-only dashboard: start it with --allow-edits to add or dismiss.</p>`)
	} else {
		fmt.Fprintf(w, `
      <form method="get" action="/policy/kits/preview-new" style="display:inline"><input type="hidden" name="repo" value="%s"><button type="submit" class="gw-kit-apply">Preview and add %d %s</button></form>
      <form hx-post="/policy/kits/dismiss-new" style="display:inline"><input type="hidden" name="repo" value="%s"><button type="submit" class="gw-kit-dismiss">Dismiss</button></form>`,
			htmlEsc(vm.Repo), n, plural(n, "frame", "frames"), htmlEsc(vm.Repo))
	}
	fmt.Fprint(w, `
    </div>`)
}

// newFramesConfirm previews the next push with only the new kit frames added.
func (h previewHandlers) newFramesConfirm(w http.ResponseWriter, r *http.Request) {
	repo := r.URL.Query().Get("repo")
	if !validRepoName(repo) {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	groups := newKitFrames(h.policyRoot, repo)
	if len(groups) == 0 {
		http.Redirect(w, r, "/policy?repo="+url.QueryEscape(repo), http.StatusSeeOther)
		return
	}
	fp, err := gateway.LoadFramePolicy(h.policyRoot, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ids := newKitFrameIDs(groups)
	after := append(append([]string{}, fp.Enabled...), ids...)
	n := len(ids)
	h.renderConfirm(w, repo, fmt.Sprintf("Add %d new kit %s", n, plural(n, "frame", "frames")), "", "/policy/kits/add-new",
		[]previewField{{"repo", repo}}, fp.Enabled, after)
}

// addNewKitFrames enables the new kit frames and records them as offered.
func (h policyHandlers) addNewKitFrames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	if !switchToObserveIfAsked(w, r, h.policyRoot, repo, "kit-new-frames") {
		return
	}
	ids := newKitFrameIDs(newKitFrames(h.policyRoot, repo))
	cfgPath := filepath.Join(h.policyRoot, repo, "appframes.toml")
	if len(ids) > 0 {
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		doc := string(data)
		for _, id := range ids {
			if doc, _, err = rewriteEnabledList(doc, id, true); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		if err := atomicWriteFile(cfgPath, []byte(doc)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if err := markFramesSeen(cfgPath, ids); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
			Event: "kit-new-frames-add", Repo: repo, OK: true,
			Payload: map[string]any{"frames": ids},
		})
	}
	redirectAfterAction(w, r, "/policy?repo="+url.QueryEscape(repo))
}

// dismissNewKitFrames records the new kit frames as offered without enabling
// them, so the notice goes away until a later release adds more.
func (h policyHandlers) dismissNewKitFrames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	r.ParseForm()
	repo := r.PostForm.Get("repo")
	if !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	ids := newKitFrameIDs(newKitFrames(h.policyRoot, repo))
	if len(ids) > 0 {
		if err := markFramesSeen(filepath.Join(h.policyRoot, repo, "appframes.toml"), ids); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
			Event: "kit-new-frames-dismiss", Repo: repo, OK: true,
			Payload: map[string]any{"frames": ids},
		})
	}
	redirectAfterAction(w, r, "/policy?repo="+url.QueryEscape(repo))
}
