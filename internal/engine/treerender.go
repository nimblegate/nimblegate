// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"html/template"
	"strings"
)

// RenderTreeHTML produces an HTML representation of the v2 selection tree
// suitable for embedding in the dashboard /policy page (spec §7.5.2). The
// output is a self-contained `<div class="nimblegate-tree">` block - no
// stylesheet or script dependencies; the dashboard supplies CSS for state
// classes (gw-tree-active / gw-tree-partial / gw-tree-partial-warn /
// gw-tree-excluded / gw-tree-inactive).
//
// State classes mirror NodeState.String() with a "gw-tree-" prefix. Each
// node carries data-* attributes for its frame count + missing IDs so the
// dashboard can show tooltips without a server roundtrip.
//
// Returns template.HTML - caller is trusted to embed verbatim (no
// user-controlled content; everything passes through escaping below).
func RenderTreeHTML(roots []TreeNode) template.HTML {
	if len(roots) == 0 {
		return template.HTML(`<div class="nimblegate-tree nimblegate-tree-empty">No axis selections active. Set [framework]/[platform]/[domains] in appframes.toml to populate.</div>`)
	}
	var b strings.Builder
	b.WriteString(`<div class="nimblegate-tree">`)
	for _, root := range roots {
		writeTreeNode(&b, root, 0)
	}
	b.WriteString(`</div>`)
	return template.HTML(b.String())
}

// writeTreeNode renders a single node + its subtree recursively. depth
// drives indentation (used for visual hierarchy in dashboard CSS).
func writeTreeNode(b *strings.Builder, n TreeNode, depth int) {
	stateClass := "gw-tree-" + n.State.String()
	leaf := len(n.Children) == 0
	tag := "div"
	if leaf {
		tag = "span"
	}
	b.WriteString(`<`)
	b.WriteString(tag)
	b.WriteString(` class="nimblegate-tree-node `)
	b.WriteString(stateClass)
	if leaf {
		b.WriteString(` nimblegate-tree-leaf`)
	}
	b.WriteString(`" data-depth="`)
	b.WriteString(itoa(depth))
	b.WriteString(`" data-total="`)
	b.WriteString(itoa(n.Total))
	b.WriteString(`" data-active="`)
	b.WriteString(itoa(n.Active))
	b.WriteString(`"`)
	if len(n.MissingIDs) > 0 {
		b.WriteString(` data-missing="`)
		b.WriteString(template.HTMLEscapeString(strings.Join(n.MissingIDs, ",")))
		b.WriteString(`"`)
	}
	b.WriteString(`>`)

	// Label + count badge
	b.WriteString(`<span class="nimblegate-tree-label">`)
	b.WriteString(template.HTMLEscapeString(n.Label))
	b.WriteString(`</span>`)
	if n.Total > 0 {
		b.WriteString(` <span class="nimblegate-tree-count">`)
		b.WriteString(itoa(n.Active))
		b.WriteString(`/`)
		b.WriteString(itoa(n.Total))
		b.WriteString(`</span>`)
	}
	b.WriteString(` <span class="nimblegate-tree-state">`)
	b.WriteString(template.HTMLEscapeString(stateLabel(n.State)))
	b.WriteString(`</span>`)

	if !leaf {
		b.WriteString(`<div class="nimblegate-tree-children">`)
		for _, c := range n.Children {
			writeTreeNode(b, c, depth+1)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</`)
	b.WriteString(tag)
	b.WriteString(`>`)
}

// stateLabel returns a human-readable badge string per NodeState. Mirrors
// the dashboard glyph convention from spec §7.5.2.
func stateLabel(s NodeState) string {
	switch s {
	case NodeFullyActive:
		return "✓ active"
	case NodePartial:
		return "◐ partial"
	case NodePartialWarn:
		return "⚠ partial (>50% stripped)"
	case NodeExcluded:
		return "⛔ excluded"
	case NodeInactive:
		return "○ inactive"
	}
	return "?"
}

// RenderKitInferenceHTML produces the "this config matches kit X" sidebar
// per spec §7.5.1. Fully-matched kits show prominently; partial matches
// are collapsed by default (CSS in the dashboard handles disclosure).
//
// onlyShowMatched=true filters out MatchNone entries (the common case for
// the dashboard sidebar). Pass false for diagnostic dumps.
func RenderKitInferenceHTML(matches []KitMatch, onlyShowMatched bool) template.HTML {
	if len(matches) == 0 {
		return template.HTML(`<div class="nimblegate-kits nimblegate-kits-empty">No kits to compare.</div>`)
	}
	var b strings.Builder
	b.WriteString(`<div class="nimblegate-kits">`)
	any := false
	for _, km := range matches {
		if onlyShowMatched && km.Status == MatchNone {
			continue
		}
		any = true
		writeKitMatch(&b, km)
	}
	if !any {
		b.WriteString(`<div class="nimblegate-kits-empty">No kits match the current configuration.</div>`)
	}
	b.WriteString(`</div>`)
	return template.HTML(b.String())
}

// writeKitMatch renders one kit's match card.
func writeKitMatch(b *strings.Builder, km KitMatch) {
	statusClass := "gw-kit-" + km.Status.String()
	b.WriteString(`<div class="nimblegate-kit-card `)
	b.WriteString(statusClass)
	b.WriteString(`" data-kit-id="`)
	b.WriteString(template.HTMLEscapeString(km.KitID))
	b.WriteString(`" data-active="`)
	b.WriteString(itoa(km.Active))
	b.WriteString(`" data-total="`)
	b.WriteString(itoa(km.Total))
	b.WriteString(`">`)

	b.WriteString(`<div class="nimblegate-kit-header"><span class="nimblegate-kit-id">`)
	b.WriteString(template.HTMLEscapeString(km.KitID))
	b.WriteString(`</span>`)
	if km.Display != "" {
		b.WriteString(` <span class="nimblegate-kit-display">`)
		b.WriteString(template.HTMLEscapeString(km.Display))
		b.WriteString(`</span>`)
	}
	if km.Semver != "" {
		b.WriteString(` <span class="nimblegate-kit-semver">v`)
		b.WriteString(template.HTMLEscapeString(km.Semver))
		b.WriteString(`</span>`)
	}
	b.WriteString(` <span class="nimblegate-kit-status">`)
	b.WriteString(template.HTMLEscapeString(kitStatusLabel(km.Status)))
	b.WriteString(`</span>`)
	b.WriteString(`</div>`)

	b.WriteString(`<div class="nimblegate-kit-counts">`)
	b.WriteString(itoa(km.Active))
	b.WriteString(` of `)
	b.WriteString(itoa(km.Total))
	b.WriteString(` frames active`)
	b.WriteString(`</div>`)

	if len(km.Missing) > 0 && km.Status != MatchFully {
		b.WriteString(`<details class="nimblegate-kit-missing"><summary>`)
		b.WriteString(itoa(len(km.Missing)))
		b.WriteString(` frame(s) the kit would add</summary><ul>`)
		for _, fid := range km.Missing {
			b.WriteString(`<li>`)
			b.WriteString(template.HTMLEscapeString(fid))
			b.WriteString(`</li>`)
		}
		b.WriteString(`</ul></details>`)
	}
	b.WriteString(`</div>`)
}

// kitStatusLabel returns a human-readable label for the kit's match status.
func kitStatusLabel(s MatchStatus) string {
	switch s {
	case MatchFully:
		return "✓ matches"
	case MatchPartial:
		return "◐ partial match"
	case MatchNone:
		return "○ no match"
	}
	return "?"
}

// itoa is a tiny non-allocating integer-to-string helper for the HTML builder.
// strconv.Itoa would work but adds an import for a single use site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
