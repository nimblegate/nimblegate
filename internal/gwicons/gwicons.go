// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package gwicons is the dashboard's shared status-icon set: small inline SVGs
// (1em, currentColor, stroke-based - matching the nav rail) used by the gateway
// templates (via a template func) AND the help sidepanel (via `:icon-<name>:`
// placeholder substitution), so a status icon looks identical wherever it
// appears. CLI/terminal output keeps its plain-text symbols - SVG is HTML-only.
package gwicons

import (
	"html/template"
	"strings"
)

// svg wraps a path body in the shared <svg> chrome. fill="none" + stroke makes
// every icon inherit the surrounding text color, so the existing .acc / .rej /
// .gw-health-status-* color rules still apply.
func svg(body string) string {
	return `<svg class="gw-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` + body + `</svg>`
}

// set maps a status-icon name to its inline SVG. Names are stable - they're
// referenced from templates ({{icon "accept"}}) and help (`:icon-accept:`).
var set = map[string]string{
	"accept":  svg(`<circle cx="12" cy="12" r="10"/><path d="m8.5 12 2.5 2.5 4.5-4.5"/>`),                                                                                                         // check-circle (✓ accept)
	"reject":  svg(`<circle cx="12" cy="12" r="10"/><line x1="5.6" y1="5.6" x2="18.4" y2="18.4"/>`),                                                                                               // ban / no-entry (⛔ reject)
	"warn":    svg(`<path d="M10.3 3.9 1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><line x1="12" y1="9" x2="12" y2="13.5"/><line x1="12" y1="17" x2="12.01" y2="17"/>`), // triangle-alert (⚠)
	"ok":      svg(`<circle cx="12" cy="12" r="10"/><path d="m8.5 12 2.5 2.5 4.5-4.5"/>`),                                                                                                         // check-circle (● ok status)
	"notif":   svg(`<rect x="2.5" y="4.5" width="19" height="15" rx="2"/><path d="m3 6 9 6 9-6"/>`),                                                                                               // envelope (📨 delivered)
	"pending": svg(`<circle cx="12" cy="12" r="9"/><path d="M12 7.5V12l3 2"/>`),                                                                                                                   // clock (🕐 pending)
	"loop":    svg(`<path d="M21 12a9 9 0 1 1-2.6-6.4"/><polyline points="21 3.5 21 9 15.5 9"/>`),                                                                                                 // refresh-cw (⟳ active loop)
}

// HTML returns the named icon as render-safe template HTML for dashboard
// templates (empty template.HTML for an unknown name).
func HTML(name string) template.HTML { return template.HTML(set[name]) }

// SVG returns the raw inline SVG markup for a name ("" if unknown).
func SVG(name string) string { return set[name] }

// Expand replaces every `:icon-<name>:` placeholder in s with its inline SVG.
// Used by the help renderer to insert trusted icon markup into already-rendered
// markdown (so the markdown source stays clean and no raw-HTML pass is needed).
func Expand(s string) string {
	if !strings.Contains(s, ":icon-") {
		return s
	}
	for name, markup := range set {
		s = strings.ReplaceAll(s, ":icon-"+name+":", markup)
	}
	return s
}
