// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"net/http"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/version"
)

//go:embed static/gwshell.js
var gwShellJS []byte

func serveGwShellJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	_, _ = w.Write(gwShellJS)
}

// chromeData drives the shared rail + top bar. UI-only; no write surface.
type chromeData struct {
	Build         string   // version.Resolved()
	Mode          string   // off | observe | enforce | "-" (read-only badge)
	Repos         []string // for the global repo switch
	ActiveRepo    string   // "" = all repos
	ActiveSection string   // feed | stats | reports | frames | policy | ssh-keys
	AuthEnabled   bool     // true when --auth != off; toggles the Sign-out button in gwtop
}

// gwLayout is one rendered gateway page: a content fragment wrapped in the shell.
type gwLayout struct {
	Title     string
	CSRFToken string // non-empty → body gets the htmx hx-headers CSRF (allow-edits pages)
	Chrome    chromeData
	Content   template.HTML // the page body, pre-rendered by the caller
}

// gwShellStyle is GATEWAY-ONLY CSS, appended after the shared dashStyle. It must
// never live in dashStyle (that const is shared with the local `nimblegate
// dashboard`, which must render unchanged).
const gwShellStyle = `<style>
 .gw-shell{display:grid;grid-template-columns:48px 1fr;grid-template-rows:auto 1fr;min-height:100vh}
 .gw-shell[data-rail="expanded"]{grid-template-columns:184px 1fr}
 .gw-shell[data-rail="hidden"]{grid-template-columns:0 1fr}
 .gw-rail{grid-row:1 / span 2;position:sticky;top:0;height:100vh;background:var(--gw-bg-input);border-right:1px solid var(--gw-border-soft);overflow-y:auto;overflow-x:hidden;display:flex;flex-direction:column}
 .gw-railhead{display:flex;align-items:center;gap:10px;padding:16px 14px 12px;font-weight:600;color:var(--gw-text);border-bottom:1px solid var(--gw-border-subtle);margin-bottom:6px;white-space:nowrap}
 .gw-railhead .ico{color:var(--gw-accent)}
 .gw-railhead .ico svg{width:20px;height:20px}
 .gw-railitem{display:flex;align-items:center;gap:10px;padding:11px 14px;color:var(--gw-text-muted);text-decoration:none;white-space:nowrap}
 .gw-railitem:hover{background:var(--gw-bg-control);color:var(--gw-text)}
 .gw-railitem.active{color:var(--gw-accent);background:var(--gw-bg-panel);box-shadow:inset 2px 0 var(--gw-accent)}
 .gw-rail .ico{flex:0 0 20px;display:flex;align-items:center;justify-content:center}
 .gw-rail .ico svg{width:18px;height:18px;display:block}
 .gw-ico{width:1em;height:1em;vertical-align:-0.14em;flex:none}
 .gw-rail .label{opacity:0;transition:opacity .12s}
 .gw-shell[data-rail="expanded"] .gw-rail .label{opacity:1}
 .gw-bottom{margin-top:auto}
 .gw-railtoggle{display:flex;align-items:center;gap:10px;cursor:pointer;color:var(--gw-text-fainter);background:none;border:0;padding:11px 14px;text-align:left;font:inherit;white-space:nowrap}
 .gw-railtoggle:hover{color:var(--gw-text-soft)}
 .gw-railtoggle .ico svg{transition:transform .15s}
 .gw-shell[data-rail="collapsed"] .gw-railtoggle .ico svg{transform:rotate(180deg)}
 .gw-main{grid-column:2;min-width:0;display:flex;flex-direction:column}
 .gw-top{display:flex;align-items:center;gap:14px;padding:9px 18px;border-bottom:1px solid var(--gw-border-soft);background:var(--gw-bg-page)}
 .gw-top .gw-repolabel{font-size:12px;color:var(--gw-text-muted);font-weight:500;margin-right:-6px}
 .gw-top .spacer{flex:1}
 .gw-top .modebadge{font-size:11px;padding:2px 9px;border-radius:10px;background:var(--gw-bg-control);color:var(--gw-text-muted)}
 .gw-menu{display:none;align-items:center;justify-content:center;background:none;border:0;color:var(--gw-text-muted);cursor:pointer;padding:4px;margin-right:2px}
 .gw-menu svg{width:22px;height:22px;display:block}
 .gw-menu:hover{color:var(--gw-text)}
 .gw-backdrop{display:none}
 /* gw-content owns the 18px L/R page gutter so every descendant (page header, tables, forms, .frame panels, /ssh-keys-style div wrappers) starts at the same column. Top 0 so .gw-pagehead's own 14px top margin handles the gap to the chrome bar; bottom 18px so the last element has breathing room above the page edge. Switching menu items now keeps content in the same column instead of jumping between 18 / 24 / 42 px depending on which wrapper a page used. */
 .gw-content{min-width:0;padding:0 18px 18px}
 .gw-content header{display:none}
 /* Shared page header - every top-level page starts with .gw-pagehead + .gw-pagedesc so the title/description bar stays in the same spot when the menu switches pages. L/R margin is 0 because gw-content already provides the 18px gutter. */
 .gw-content .gw-pagehead{margin:14px 0 4px;font-size:16px;font-weight:600;color:var(--gw-text)}
 .gw-content .gw-pagedesc{margin:0 0 14px;color:var(--gw-text-muted);font-size:13px;line-height:1.55}
 /* Back-to-back <p class="gw-pagedesc"> get a tight join - the second
    paragraph reads as the same descriptive block, not a new section.
    /feed splits its description across two paragraphs for readability. */
 .gw-content .gw-pagedesc+.gw-pagedesc{margin-top:-8px}
 /* Stats summary line (currently used by /feed for the repo/accept/reject
    counts and the top-block line). Lighter weight than .gw-pagedesc but
    still on its own line with proper breathing room from neighbors. */
 .gw-content .gw-feed-stats{margin:6px 0;color:var(--gw-text-soft);font-size:12px;line-height:1.7}
 .gw-content .gw-feed-stats+.gw-feed-stats{margin-top:2px}
 .gw-content .gw-pagedesc code{font-size:12px}
 /* dashStyle's section{margin:18px 24px} would double up against gw-content's 18px L/R padding (total 42px). Zero the section margins inside gateway pages; gw-content handles the gutter. */
 .gw-content section{margin:0}
 .gw-content select,.gw-top select{background:var(--gw-bg-control);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:5px 9px;font:inherit;cursor:pointer;max-width:fit-content}
 .gw-content select:hover,.gw-top select:hover{border-color:var(--gw-border-hover)}
 .gw-content select:focus,.gw-top select:focus{outline:none;border-color:var(--gw-accent)}
 .gw-content label{display:inline-flex;align-items:center;gap:6px;color:var(--gw-text-soft);font-size:13px;margin-right:6px}
 .gw-content input[type=checkbox]{appearance:none;-webkit-appearance:none;width:15px;height:15px;border:1px solid var(--gw-border);border-radius:4px;background:var(--gw-bg-control);cursor:pointer;position:relative;vertical-align:-2px}
 .gw-content input[type=checkbox]:hover{border-color:var(--gw-border-hover)}
 .gw-content input[type=checkbox]:checked{background:var(--gw-accent);border-color:var(--gw-accent)}
 .gw-content input[type=checkbox]:checked::after{content:"";position:absolute;left:4px;top:1px;width:3px;height:7px;border:solid var(--gw-bg-page);border-width:0 2px 2px 0;transform:rotate(45deg)}
 .gw-setrow{display:flex;align-items:center;gap:14px;margin:12px 0}
 .gw-setrow label{min-width:150px;color:var(--gw-text-soft)}
 .gw-content .gw-fbar{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 12px}
 .gw-content form.gw-filters{display:flex;align-items:center;gap:10px;flex-wrap:wrap;margin:0 0 12px}
 .gw-content form.gw-filters label{margin-right:0}
 .gw-content .gw-statusfilter{display:flex;align-items:center;gap:8px;padding-left:14px;border-left:1px solid var(--gw-border)}
 .gw-content .gw-searchbox{background:var(--gw-bg-control);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:5px 9px;font:inherit;min-width:200px}
 .gw-content .gw-searchbox:focus{outline:none;border-color:var(--gw-accent)}
 .gw-content .gw-sevchip,.gw-content .gw-feedchip,.gw-content .gw-evchip{cursor:pointer;border:1px solid transparent;font:inherit}
 .gw-content .gw-sevchip[aria-pressed="false"],.gw-content .gw-feedchip[aria-pressed="false"],.gw-content .gw-evchip[aria-pressed="false"]{opacity:.4}
 .gw-content .gw-stat{display:inline-block;min-width:92px;white-space:nowrap}
 .gw-content table.fr td.loc{padding:6px 4px;white-space:nowrap;vertical-align:top}
 .gw-content table.fr td.gw-statcell{vertical-align:top}
 .gw-content #feed table.fr{table-layout:fixed}
 .gw-content #feed table.fr col.col-loc{width:14%}
 .gw-content #feed table.fr col.col-msg{width:19%}
 .gw-content #feed table.fr col.col-stat{width:58%}
 .gw-content #feed table.fr col.col-reset{width:9%}
 @media(max-width:600px){
   .gw-content #feed table.fr col.col-loc{width:20%}
   .gw-content #feed table.fr col.col-msg{width:40%}
   .gw-content #feed table.fr col.col-stat{width:30%}
   .gw-content #feed table.fr col.col-reset{width:10%}
 }
 .gw-content table.fr td.gw-resetcell{vertical-align:middle;text-align:right;white-space:nowrap;padding:6px 8px}
 .gw-content .gw-finds{display:flex;flex-direction:column;align-items:flex-start;gap:3px;margin-top:4px}
 .gw-content .gw-find{display:flex;flex-direction:column;align-items:flex-start;gap:2px}
 .gw-content table.fr td.gw-statcell .dmsg{white-space:normal;overflow-wrap:anywhere}
 .gw-content table.fr td.gw-msgcell{vertical-align:top}
 .gw-content .gw-repo,.gw-content .gw-ref{display:block}
 .gw-content .gw-ref,.gw-content .gw-rmsg{color:var(--gw-text-faint)}
 .gw-content button.gw-ref{border:0;background:transparent;font:inherit;cursor:pointer;padding:0;text-align:left}
 .gw-content button.gw-ref:hover{color:var(--gw-accent)}
 .gw-content button.gw-ref[aria-expanded="true"]{color:var(--gw-accent)}
 .gw-content .gw-sha{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:11px;color:var(--gw-text-fainter);padding:1px 5px;border-radius:3px;background:var(--gw-bg-control);margin-left:4px}
 .gw-content .gw-rmsg{display:none;font-size:11px;white-space:normal;overflow-wrap:anywhere;max-width:340px}
 .gw-content .gw-msgcell button.gw-ref[aria-expanded="true"] ~ .gw-rmsg{display:block}
 .gw-content .gw-find .dmsg{display:none}
 .gw-content .gw-find .fnd[aria-expanded="true"] ~ .dmsg{display:block}
 .gw-content button.fnd{border:0;font:inherit;cursor:pointer}
 .gw-content button.fnd:hover{filter:brightness(1.15)}
 body[data-tc="off"] .gw-content time.gw-ts{color:var(--gw-accent)}
 .gw-content tr.gw-daysep td{border-top:1px solid currentColor;padding:10px 10px 4px;font-size:11px;letter-spacing:.5px;opacity:.85}
 /* accordion marker - universal across the gateway dashboard, matches stats */
 .gw-content details>summary{cursor:pointer;font-weight:600;color:var(--gw-text);list-style:none;user-select:none;padding:2px 0}
 .gw-content details>summary::-webkit-details-marker{display:none}
 /* Chevron markers on every details>summary. Unicode ▸/▾ rendered inconsistently across fonts (heavier on some platforms, mismatched baseline on others); replace with inline SVG via data URI so every browser sees the same shape at the same size. Accent 2 (#79c0ff) hard-coded because data URIs don't resolve currentColor. */
 .gw-content details>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 4 11 8 6 12'/></svg>");margin-right:8px;display:inline-block;vertical-align:0.05em}
 .gw-content details[open]>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='4 6 8 11 12 6'/></svg>")}
 /* details.frame uses dashStyle .frame chrome - see dashboard.go const dashStyle */
 .gw-content details.frame{margin:12px 0}
 /* sub-details inside a .frame: no panel chrome, lighter summary */
 .gw-content .frame details{margin:8px 0;padding:0;background:none;border:0;border-radius:0}
 .gw-content .frame details>summary{font-size:13px;color:var(--gw-text-soft);font-weight:500}
 /* inline event-payload expander in the /events table */
 .gw-content .gw-event-details>summary{cursor:pointer;list-style:none;font:inherit;font-weight:normal;color:var(--gw-text-soft);padding:0}
 .gw-content .gw-event-details>summary::-webkit-details-marker{display:none}
 .gw-content .gw-event-details>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 4 11 8 6 12'/></svg>");display:inline-block;width:12px;margin-right:0;vertical-align:0.05em}
 .gw-content .gw-event-details[open]>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='4 6 8 11 12 6'/></svg>")}
 .gw-event-raw{margin:6px 0 0;padding:8px 10px;background:var(--gw-bg-input);border:1px solid var(--gw-border-soft);border-radius:3px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:11px;color:var(--gw-text-soft);white-space:pre-wrap;overflow-x:auto}
 /* base button - consistent across every form on the gateway dashboard */
 .gw-content button{background:var(--gw-bg-control);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:5px 12px;font:inherit;cursor:pointer}
 .gw-content button:hover{border-color:var(--gw-accent)}
 /* Primary-action button (type=submit) - Accent 2 background + Surface 0 text
    from the 5-step brand palette, Accent 1 on hover. Same look as the /login
    Sign-In button + every per-section primary button (Add check, Save
    estimates, Update credential, Review/Confirm whitelist) so the visual
    language is consistent. The previous --gw-submit-bg (#1b6fd6) was a different
    blue that didn't sit in the brand palette. */
 .gw-content button[type=submit]{background:var(--gw-accent);color:var(--gw-bg-input);border-color:var(--gw-accent);font-weight:500}
 .gw-content button[type=submit]:hover{background:#5e93c4;border-color:#5e93c4}
 .gw-content button.danger{background:var(--gw-danger-bg);color:var(--gw-text);border-color:var(--gw-danger-bg);margin-top:6px}
 .gw-content button.danger:hover{background:var(--gw-danger-bg-hover);border-color:var(--gw-danger-bg-hover)}
 /* text inputs - used in add-repo + future forms */
 .gw-content input[type=text],.gw-content input[type=password]{background:var(--gw-bg-input);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:5px 9px;font:inherit}
 .gw-content input[type=text]:focus,.gw-content input[type=password]:focus{outline:none;border-color:var(--gw-accent)}
 /* add-repo form layout */
 .gw-content .gw-add-repo form{display:flex;flex-direction:column;gap:10px;margin-top:10px}
 .gw-content .gw-add-repo label{display:flex;flex-direction:column;align-items:stretch;gap:4px;color:var(--gw-text-soft);font-size:13px;margin:0;min-width:0}
 .gw-content .gw-add-repo label:has(input[type=checkbox]){flex-direction:row;align-items:center;gap:6px}
 .gw-content .gw-add-repo fieldset{border:1px solid var(--gw-border);border-radius:6px;padding:8px 12px;margin:0;display:flex;flex-wrap:wrap;gap:10px}
 .gw-content .gw-add-repo fieldset legend{color:var(--gw-text-muted);font-size:12px;padding:0 4px}
 .gw-content .gw-add-repo button[type=submit]{align-self:flex-start;padding:6px 14px}
 .gw-content .gw-add-repo .sub{color:var(--gw-text-muted);font-size:12px;margin:0}
 /* scan recommendation banner (uses .frame chrome + left accent) */
 .gw-content .gw-scan-banner{border-left:3px solid var(--gw-accent)}
 .gw-content .gw-group{display:inline-block;background:var(--gw-bg-input);border:1px solid var(--gw-border);border-radius:4px;padding:2px 8px;margin:2px 4px 2px 0;font-size:12px;color:var(--gw-text-soft)}
 .gw-content .gw-scan-actions{display:flex;gap:8px;margin-top:10px;flex-wrap:wrap}
 /* archived repos panel rows - name left, then actions, left-aligned (not
    spread); buttons sized to match the active-repos action buttons so Restore
    and Delete permanently are the same height. */
 .gw-content .gw-archived-row{display:flex;align-items:center;gap:8px;padding:6px 0;border-bottom:1px solid var(--gw-border-subtle)}
 .gw-content .gw-archived-row:last-child{border-bottom:0}
 .gw-content .gw-archived-row>span{min-width:160px;font-size:13px}
 .gw-content .gw-archived-row form{margin:0}
 .gw-content .gw-archived-row form>button{box-sizing:border-box;font-size:12px;line-height:1.4;padding:3px 10px;border-width:1px;border-radius:3px;margin:0}
 /* danger zone - Archive this repo. Chevron recolored to the danger marker
    by overriding the data-URI; the rest of the rule chain above already
    handles open/closed shape. */
 .gw-content .gw-danger>summary{color:var(--gw-danger-summary)}
 .gw-content .gw-danger>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%23ff6666' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 4 11 8 6 12'/></svg>")}
 .gw-content .gw-danger[open]>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%23ff6666' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='4 6 8 11 12 6'/></svg>")}
 .gw-content .gw-danger p{color:var(--gw-text-soft);font-size:13px;margin:8px 0}
 /* Whitelist add-form (appears on /stats Recurring findings) + remove confirm.
    Same column-flex layout, gap, and brand-input styling as the add-repo form
    on /policy so the two never feel like different UIs. The .wlconfirm banner
    rendered after Review is the palette's elevated-surface (Surface 1) tone. */
 .gw-content details.wl{margin:6px 0}
 .gw-content details.wl>summary{font-size:12px;color:var(--gw-text-muted);font-weight:500}
 .gw-content .wl form{display:flex;flex-direction:column;gap:10px;margin-top:8px;padding:10px 12px;background:var(--gw-bg-input);border:1px solid var(--gw-border);border-radius:6px;min-width:260px}
 .gw-content .wl label{display:flex;flex-direction:column;align-items:stretch;gap:4px;color:var(--gw-text-soft);font-size:13px;margin:0}
 .gw-content .wl input[type=text]{width:100%;box-sizing:border-box}
 .gw-content .wl .wlscope{display:inline-block;font-size:12px;color:var(--gw-text-muted);padding:2px 8px;border:1px solid var(--gw-border);border-radius:4px;background:var(--gw-bg-page);align-self:flex-start}
 .gw-content .wl .wlscope.warn{color:var(--gw-warn-text);border-color:var(--gw-warn-bg);background:var(--gw-warn-bg)}
 .gw-content .wl .hint{font-size:12px;color:var(--gw-text-fainter);margin:0}
 .gw-content .wl .hint code{font-size:11px;background:var(--gw-bg-control);padding:1px 4px;border-radius:3px}
 .gw-content .wl button[type=button]{background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);font-weight:500;align-self:flex-start;padding:6px 14px}
 .gw-content .wl button[type=button]:hover{background:#5e93c4;border-color:#5e93c4}
 /* Confirm banner that swaps in after "Review" / "Remove" - elevated surface tone
    + primary action and a neutral Cancel sibling. */
 .gw-content .wlconfirm{display:flex;flex-direction:column;gap:8px;margin-top:6px;padding:10px 12px;background:var(--gw-bg-panel);border:1px solid var(--gw-accent);border-radius:6px;font-size:12px;color:var(--gw-text-soft)}
 .gw-content .wlconfirm code{background:var(--gw-bg-input);padding:1px 5px;border-radius:3px;font-size:11px;color:var(--gw-text)}
 .gw-content .wlconfirm form{display:inline}
 /* Selectors use structural position (button inside the form = primary,
    sibling button = Cancel) instead of attribute matches so the rule names
    don't contain "hx-post" - TestRenderPolicyPage asserts no hx-post strings
    appear when --allow-edits is off, and CSS selectors count as strings. */
 .gw-content .wlconfirm form button{background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);font-weight:500;padding:5px 12px}
 .gw-content .wlconfirm form button:hover{background:#5e93c4;border-color:#5e93c4}
 .gw-content .wlconfirm>button{background:transparent;color:var(--gw-text-muted);border:1px solid var(--gw-border);padding:5px 12px}
 .gw-content .wlconfirm>button:hover{border-color:var(--gw-border-hover);color:var(--gw-text-soft)}
 /* Linter-row delete confirm: override the default wlconfirm column-flex
    layout so the prompt + buttons sit inline next to the row being deleted
    instead of stretching to full content width. Compact pill, centered,
    auto width - looks like a proper inline confirmation banner rather
    than a full-width column. */
 .gw-content .wlconfirm.gw-lint-confirm{display:inline-flex;flex-direction:row;flex-wrap:wrap;align-items:center;gap:8px;width:auto;margin-left:8px;padding:6px 12px}
 .gw-content .wlconfirm.gw-lint-confirm form{display:inline}
 .gw-content .wlconfirm.gw-lint-confirm code{margin:0 2px}
 /* Custom-linters authoring form on /policy (rendered by gatewayauthoring.go).
    Same column-flex layout + brand inputs as .gw-add-repo / .wl so all three
    form surfaces on the page feel like the same UI. Two-button row at the
    bottom: Add is the primary palette-accent action, Preview is the neutral
    secondary sibling. */
 /* The inner H3 "Checks" + H4 "Add a check" were removed from this section to
    match the pattern used by every other section on /policy (details>summary
    is the label, body is the form). Keep text-align:left as a defensive guard
    in case the section ever gains inner text headings again. */
 .gw-content .gw-authoring{text-align:left}
 .gw-content .gw-authoring form{display:flex;flex-direction:column;gap:10px;margin-top:8px;padding:12px 14px;background:var(--gw-bg-input);border:1px solid var(--gw-border);border-radius:6px}
 .gw-content .gw-authoring label{display:flex;flex-direction:column;align-items:stretch;gap:4px;color:var(--gw-text-soft);font-size:13px;margin:0}
 .gw-content .gw-authoring input[type=text]{width:100%;box-sizing:border-box}
 .gw-content .gw-authoring select[name=severity]{max-width:100px;align-self:flex-start}
 .gw-content .gw-authoring .gw-authoring-actions{display:flex;gap:8px;margin-top:4px;flex-wrap:wrap}
 .gw-content .gw-authoring .gw-authoring-actions button{background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);font-weight:500;padding:6px 14px}
 .gw-content .gw-authoring .gw-authoring-actions button:hover{background:#5e93c4;border-color:#5e93c4}
 .gw-content .gw-authoring .gw-authoring-actions button.secondary{background:transparent;color:var(--gw-text-soft);border-color:var(--gw-border)}
 .gw-content .gw-authoring .gw-authoring-actions button.secondary:hover{border-color:var(--gw-accent);color:var(--gw-accent);background:transparent}
 /* Time-prevented estimates editor on /policy. Three-column grid:
    [Tier label] [number input] [source hint]. Each <label> uses
    display:contents so its children become direct grid children - keeping
    accessibility (label wraps input) without breaking the grid layout. */
 .gw-content .gw-time-estimates{margin:10px 0}
 .gw-content .gw-time-estimates>summary{font-size:13px;color:var(--gw-text)}
 .gw-content .gw-te-hint{color:var(--gw-text-muted);font-size:12px;margin:6px 0 12px;line-height:1.5}
 .gw-content .gw-te-hint a{color:var(--gw-accent)}
 .gw-content .gw-te-grid{display:grid;grid-template-columns:auto 110px 1fr;gap:8px 14px;align-items:center;padding:12px 14px;background:var(--gw-bg-input);border:1px solid var(--gw-border);border-radius:6px}
 .gw-content .gw-te-row{display:contents}
 .gw-content .gw-te-row>.gw-te-tier{color:var(--gw-text-soft);font-size:13px}
 .gw-content .gw-te-row>input[type=number]{width:100%;box-sizing:border-box;background:var(--gw-bg-page);color:var(--gw-text);border:1px solid var(--gw-border);border-radius:6px;padding:5px 9px;font:inherit}
 .gw-content .gw-te-row>input[type=number]:focus{outline:none;border-color:var(--gw-accent)}
 .gw-content .gw-te-row>.gw-te-source{color:var(--gw-text-fainter);font-size:12px}
 .gw-content .gw-te-save{grid-column:1 / -1;justify-self:start;margin-top:8px;background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);font-weight:500;padding:6px 16px}
 .gw-content .gw-te-save:hover{background:#5e93c4;border-color:#5e93c4}
 /* Credential-rotate form on /policy. Same column-flex pattern as the other
    forms on the page; the password input stretches to fill the form's available
    width (giving it the "wider on desktop" feel) and the button sits below
    with consistent gap so the mobile view stacks cleanly. */
 .gw-content .gw-credform{display:flex;flex-direction:column;gap:10px;margin-top:8px;padding:12px 14px;background:var(--gw-bg-input);border:1px solid var(--gw-border);border-radius:6px}
 .gw-content .gw-credform label{display:flex;flex-direction:column;align-items:stretch;gap:4px;color:var(--gw-text-soft);font-size:13px;margin:0}
 .gw-content .gw-credform input[type=password]{width:100%;box-sizing:border-box}
 .gw-content .gw-credform .gw-credform-note{margin:4px 0 0;color:var(--gw-text-fainter);font-size:12px;line-height:1.5}
 .gw-content .gw-credform .gw-credform-note code{background:var(--gw-bg-control);padding:1px 5px;border-radius:3px;font-size:11px}
 .gw-content .gw-credform button[type=submit]{align-self:flex-start;background:var(--gw-accent);color:var(--gw-bg-input);border:1px solid var(--gw-accent);font-weight:500;padding:6px 16px}
 .gw-content .gw-credform button[type=submit]:hover{background:#5e93c4;border-color:#5e93c4}
 @media(max-width:600px){
   .gw-content .gw-credform{padding:10px 12px}
   .gw-content .gw-credform button[type=submit]{align-self:stretch}
 }
 @media(max-width:760px){
   .gw-shell,.gw-shell[data-rail="expanded"]{grid-template-columns:0 1fr}
   .gw-rail{position:fixed;z-index:5;top:0;left:0;height:100vh;width:184px;transform:translateX(-100%);transition:transform .15s}
   .gw-shell[data-rail="expanded"] .gw-rail{transform:none}
   .gw-shell[data-rail="expanded"] .gw-rail .label{opacity:1}
   .gw-menu{display:flex}
   .gw-top{flex-wrap:wrap;row-gap:6px}
   .gw-top .ver{display:none}
   .gw-shell[data-rail="expanded"] .gw-backdrop{display:block;position:fixed;inset:0;background:rgba(0,0,0,.5);z-index:4}
 }

 /* /policy frame selection - kit chips + quick-start + browse tree.
    The tree uses native <details>/<summary> so it survives JS-off; CSS adds the
    indent rails + chevrons + nesting that make the Category → Subcategory →
    Frame hierarchy visually obvious. */
 /* Section heads - consistent 16px headline shared by every top-level
    section on /policy (Frame selection h2 + Add/Edit repo summaries +
    Currently enabled frames summary + Selected repo trailer header). Same
    typographic weight + color as .gw-pagehead so the page reads as one
    set of section bands rather than a mix of browser-default <h2> sizes
    and unstyled <summary> labels. */
 .gw-content .gw-section-head{margin:14px 0 4px;font-size:16px;font-weight:600;color:var(--gw-text);cursor:pointer}
 .gw-content .gw-section-head strong{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--gw-accent)}
 /* h2 / h3 with .gw-section-head are standalone section labels, not
    clickable <summary> elements - drop the pointer cursor that the
    summary form needs. h3 used as sub-section heading inside repo blocks. */
 .gw-content h2.gw-section-head,.gw-content h3.gw-section-head{cursor:default}
 .gw-content h3.gw-section-head{margin:18px 0 6px;font-size:15px}
 .gw-content summary.gw-section-head{cursor:pointer}
 .gw-content .gw-stats-repo{margin:6px 0 4px;font-size:18px;font-weight:600;color:var(--gw-text);font-family:ui-monospace,SFMono-Regular,Menlo,monospace}

 /* Status fieldset on the add-new-repo form - thin border + inline legend
    groups the two checkboxes (enabled + observe-only) as their own setting
    cluster, visually distinct from the freeform inputs above and below. */
 .gw-status-fieldset{margin:10px 0;padding:8px 12px 10px;border:1px solid var(--gw-border-soft);border-radius:4px}
 .gw-status-fieldset>legend{padding:0 6px;font-size:12px;color:var(--gw-text-muted);font-weight:600;letter-spacing:.5px;text-transform:uppercase}
 .gw-status-fieldset>label{margin-right:14px}
 .gw-add-note{margin:10px 0;padding:8px 10px;background:var(--gw-bg-panel);border-left:3px solid var(--gw-accent);border-radius:0 3px 3px 0;color:var(--gw-text-soft);font-size:12px;line-height:1.5}
 .gw-add-note b{color:var(--gw-text)}

 /* Enabled-frames summary in the Edit existing repo section - read-only
    Category → Subcategory → frame ID at-a-glance view. The editable browse
    tree at the page bottom is the place to mutate. */
 .gw-enabled-summary{margin:12px 0}
 .gw-enabled-summary>summary{cursor:pointer;font-weight:600;color:var(--gw-text)}
 .gw-summary-cat{margin:8px 0 0;padding:6px 10px;border-left:2px solid var(--gw-border);background:var(--gw-bg-panel);border-radius:0 3px 3px 0}
 .gw-summary-cat-head{font-weight:600;color:var(--gw-text);font-size:13px;margin-bottom:4px}
 .gw-summary-count{display:inline-block;min-width:14px;padding:0 4px;background:var(--gw-bg-control);border-radius:8px;color:var(--gw-text-soft);font-size:11px;text-align:center;margin-left:4px}
 .gw-summary-sub{margin:2px 0 4px 12px}
 .gw-summary-sub-name{font-size:12px;color:var(--gw-text-soft);margin-bottom:2px}
 .gw-summary-frames{list-style:none;margin:0 0 0 12px;padding:0}
 .gw-summary-frames li{padding:1px 0}
 .gw-summary-frames code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:11px;color:var(--gw-text-muted)}

 .gw-policy-context{margin:0 0 10px;padding:6px 10px;background:var(--gw-info-bg);border-left:3px solid var(--gw-accent);color:var(--gw-text);font-size:13px;border-radius:0 3px 3px 0}
 .gw-policy-context strong{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}
 .gw-policy-empty{margin-top:18px}
 .gw-policy-empty .sub{padding:10px;background:var(--gw-bg-panel);border:1px dashed var(--gw-border);border-radius:4px;color:var(--gw-text-muted)}
 .gw-policy-kits{display:flex;flex-direction:column;gap:8px;margin:12px 0}
 .gw-policy-applied,.gw-policy-quickstart{display:flex;flex-wrap:wrap;align-items:center;gap:6px;font-size:13px;color:var(--gw-text-soft)}
 #gw-policy-groups-row{margin-top:4px}
 .gw-policy-applied .sub,.gw-policy-quickstart .sub{color:var(--gw-text-muted)}
 .gw-policy-stats{margin-left:auto;color:var(--gw-text-muted);font-size:12px}
 .gw-kit-count{display:inline-block;min-width:14px;padding:0 4px;background:var(--gw-bg-control);border-radius:8px;color:var(--gw-text-soft);font-size:11px;text-align:center;margin-left:2px}
 .gw-cat-pill{border-style:dashed;color:var(--gw-text-soft)}
 .gw-custom-pill{border-color:var(--gw-accent)}
 .gw-policy-total{margin-top:6px;font-size:12px;color:var(--gw-text-muted)}
 .gw-kit-chip{display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border:1px solid var(--gw-border);border-radius:12px;background:var(--gw-bg-panel);font-size:12px;color:var(--gw-text)}
 .gw-kit-chip .gw-kit-clear{background:transparent;border:0;cursor:pointer;color:var(--gw-text-muted);font-size:14px;line-height:1;padding:0 2px}
 .gw-kit-chip .gw-kit-clear:hover{color:var(--gw-text)}
 .gw-kit-apply,.gw-kit-new{background:var(--gw-bg-control);border:1px solid var(--gw-border);color:var(--gw-text);padding:3px 10px;border-radius:4px;font-size:12px;cursor:pointer}
 .gw-kit-apply:hover:not([disabled]),.gw-kit-new:hover{background:var(--gw-bg-panel);border-color:var(--gw-border-hover)}
 .gw-kit-apply[disabled]{opacity:.55;cursor:default}

 .gw-policy-browse{margin-top:16px;border-top:1px solid var(--gw-border-soft);padding:14px 16px 0}
 .gw-cat{margin:12px 0;border:1px solid var(--gw-border);border-radius:4px;background:var(--gw-bg-panel)}
 .gw-cat>summary{cursor:pointer;padding:10px 14px 10px 18px;font-weight:600;font-size:14px;list-style:none;user-select:none;color:var(--gw-text)}
 .gw-cat>summary::-webkit-details-marker{display:none}
 .gw-cat>summary::marker{content:""}
 .gw-cat>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 4 11 8 6 12'/></svg>");display:inline-block;width:14px;vertical-align:0.05em}
 .gw-cat[open]>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='4 6 8 11 12 6'/></svg>")}
 .gw-cat[open]{background:var(--gw-bg-input)}
 .gw-sub{margin:4px 12px 4px 24px;padding-left:10px;border-left:2px solid var(--gw-border-soft)}
 .gw-sub>summary{cursor:pointer;padding:3px 6px;font-size:13px;color:var(--gw-text-soft);list-style:none;user-select:none}
 .gw-sub>summary::-webkit-details-marker{display:none}
 .gw-sub>summary::marker{content:""}
 .gw-sub>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='6 4 11 8 6 12'/></svg>");display:inline-block;width:12px;vertical-align:0.05em}
 .gw-sub[open]>summary::before{content:url("data:image/svg+xml;utf8,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' width='10' height='10' fill='none' stroke='%2379c0ff' stroke-width='2.4' stroke-linecap='round' stroke-linejoin='round'><polyline points='4 6 8 11 12 6'/></svg>")}
 .gw-frames{list-style:none;margin:2px 0 6px 16px;padding:0}
 .gw-frames li{padding:2px 0}
 .gw-frames label{display:inline-flex;align-items:center;gap:6px;cursor:pointer;font-size:12px;color:var(--gw-text)}
 .gw-frames code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;color:var(--gw-text)}
 .gw-fr-sev{font-size:10px;padding:1px 6px;border-radius:3px;background:var(--gw-bg-control);color:var(--gw-text-muted);margin-left:auto;letter-spacing:.5px}
 .gw-fr-sev.gw-sev-block{background:var(--gw-block-bg);color:var(--gw-block-text)}
 .gw-fr-sev.gw-sev-warn{background:var(--gw-warn-bg);color:var(--gw-warn-text)}
 .gw-fr-sev.gw-sev-info{background:var(--gw-info-bg);color:var(--gw-text-soft)}
 /* Custom kits use the same neutral border as stdlib category cards - the
    visual distinction is the "(custom kit)" sub-label + the in-row Delete
    button + the "Custom Kits" headline grouping, not an accent border. */
 .gw-user-cat>summary{display:flex;align-items:center;gap:8px}
 .gw-user-cat>summary .sub{color:var(--gw-text-muted);font-weight:400;font-size:12px;margin-left:6px}
 .gw-userkit-delete{margin-left:auto;background:var(--gw-danger-bg);border:1px solid var(--gw-danger-border);color:var(--gw-danger-text);font-size:11px;padding:3px 10px;border-radius:3px;cursor:pointer}
 .gw-userkit-delete:hover{background:var(--gw-danger-bg-hover);color:var(--gw-danger-summary)}

 /* Headlines inside the browse tree split user-defined Custom Kits from the
    stdlib System Default Groups so the operator sees the two domains as
    distinct sets, not a single jumbled list. */
 .gw-browse-section-head{margin:8px 0 4px;color:var(--gw-text-muted);font-size:12px;font-weight:600;text-transform:uppercase;letter-spacing:.6px}

 .gw-userkit-form{margin:8px 0;padding:10px;border:1px solid var(--gw-border);border-radius:4px;background:var(--gw-bg-panel)}
 .gw-userkit-form>label{display:block;margin-bottom:8px;font-size:13px;color:var(--gw-text)}
 .gw-userkit-form input[type="text"]{width:240px;padding:4px 6px;background:var(--gw-bg-input);border:1px solid var(--gw-border);color:var(--gw-text);font-size:13px;border-radius:3px}
 .gw-userkit-pick{margin:6px 0;max-height:240px;overflow-y:auto;padding:6px;border:1px solid var(--gw-border-soft);border-radius:3px;background:var(--gw-bg-input)}
 .gw-userkit-pick label{display:block;font-size:12px;padding:1px 4px;color:var(--gw-text-soft)}
 .gw-userkit-form button{margin-right:6px}

 /* /repos page */
 .gw-repos-table{width:100%;border-collapse:collapse;margin:10px 0}
 .gw-repos-table th{text-align:left;color:var(--gw-text-muted);font-size:12px;font-weight:600;padding:6px 10px;border-bottom:1px solid var(--gw-border-soft)}
 .gw-repos-table td{padding:7px 10px;border-bottom:1px solid var(--gw-border-subtle);vertical-align:middle;font-size:13px}
 .gw-repos-table tr:hover td{background:var(--gw-bg-panel)}
 .gw-repos-table .gw-repos-url,.gw-repos-table .gw-repos-refs{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:11px;color:var(--gw-text-soft)}
 /* Stack per-row actions vertically so Edit policy / Rotate credential /
    Archive don't render as one bunched blob in the cell. Each action
    becomes its own block-level item with consistent spacing. */
 .gw-repos-table .gw-repos-actions{vertical-align:top}
 .gw-repos-table .gw-repos-actions>a,
 .gw-repos-table .gw-repos-actions>details,
 .gw-repos-table .gw-repos-actions>form{display:block;margin:0 0 4px;max-width:fit-content}
 .gw-repos-table .gw-repos-actions>*:last-child{margin-bottom:0}
 /* Uniform control height: the Edit link and every action button (Sync,
    Switch to observe/enforce, Archive) share font-size, line-height, padding,
    and border so they render the SAME height regardless of label length or
    whether the control is an <a> or a <button>. Per-control rules below set
    only color. */
 .gw-repos-table .gw-repos-actions>a,
 .gw-repos-table .gw-repos-actions>form>button{box-sizing:border-box;font-size:12px;line-height:1.4;padding:3px 10px;border-width:1px;border-radius:3px;margin-top:0}
 /* Edit policy renders as a button-styled link (same shape as the action
    buttons) so the row controls read consistently. Accent color signals the
    primary "go to editing" navigation. */
 .gw-repos-table .gw-repos-action-edit{display:inline-block;background:var(--gw-bg-control);border:1px solid var(--gw-border);color:var(--gw-accent);text-decoration:none}
 .gw-repos-table .gw-repos-action-edit:hover{background:var(--gw-bg-panel);border-color:var(--gw-border-hover)}
 .gw-repos-table .gw-repos-actions>details>summary{cursor:pointer;font-size:12px;color:var(--gw-text-soft)}
 .gw-repo-badge{display:inline-block;font-size:10px;padding:1px 7px;border-radius:10px;margin-right:3px;letter-spacing:.3px}
 .gw-repo-badge.on{background:var(--gw-ok-bg-soft,rgba(0,200,100,.12));color:var(--gw-ok-text,#4cbb87);border:1px solid var(--gw-ok-accent,#4cbb87)}
 .gw-repo-badge.observe{background:var(--gw-warn-bg);color:var(--gw-warn-text);border:1px solid var(--gw-warn-bg)}
 .gw-repo-badge.cred{background:var(--gw-bg-control);color:var(--gw-text-soft);border:1px solid var(--gw-border)}
 /* cred-na: "credential n/a (SSH)" - SSH relay uses the gateway's global
    identity, no per-repo credential needed. Renders neutral (not warning)
    so operators don't misread it as a problem state. */
 .gw-repo-badge.cred-na{background:var(--gw-bg-control);color:var(--gw-text-muted);border:1px dashed var(--gw-border)}
 .gw-repo-badge.off{background:var(--gw-bg-control);color:var(--gw-text-muted);border:1px solid var(--gw-border-subtle)}
 /* /repos page - "Issues to address" banner table. Same shape as
    .gw-repos-table but extra horizontal padding so the columns breathe. */
 .gw-repo-issues-table{width:100%;border-collapse:collapse;margin:10px 0}
 .gw-repo-issues-table th{text-align:left;color:var(--gw-text-muted);font-size:12px;font-weight:600;padding:6px 16px 6px 0;border-bottom:1px solid var(--gw-border-soft)}
 .gw-repo-issues-table td{text-align:left;padding:7px 16px 7px 0;border-bottom:1px solid var(--gw-border-subtle);vertical-align:top;font-size:13px}
 .gw-repo-issues-table th:last-child,.gw-repo-issues-table td:last-child{padding-right:0}
 .gw-repo-issues-table tr:hover td{background:var(--gw-bg-panel)}
 .gw-repo-issue-sev{display:inline-block;font-size:10px;padding:1px 7px;border-radius:10px;letter-spacing:.3px}
 .gw-repo-issue-sev.block{background:var(--gw-err-bg,rgba(220,80,80,.14));color:var(--gw-err-text,#e07a7a);border:1px solid var(--gw-err-accent,#e07a7a)}
 .gw-repo-issue-sev.warn{background:var(--gw-warn-bg);color:var(--gw-warn-text);border:1px solid var(--gw-warn-bg)}
 .gw-justregistered{margin:0 0 12px;padding:8px 12px;background:var(--gw-ok-bg-soft,rgba(0,200,100,.08));border-left:3px solid var(--gw-ok-accent,#4cbb87);border-radius:0 3px 3px 0;color:var(--gw-ok-text,#4cbb87);font-size:13px}
 .gw-justregistered a{color:var(--gw-ok-accent,#4cbb87)}
 .gw-repo-picker{margin:0 0 14px}
 .gw-repo-picker select{font-size:13px}
 /* Help drawer (sidepanel) */
 .gw-help{position:fixed;top:0;right:0;width:320px;height:100vh;background:var(--gw-bg-page);border-left:1px solid var(--gw-border);box-shadow:-4px 0 12px rgba(0,0,0,.18);transform:translateX(100%);transition:transform 180ms ease-out;z-index:50;overflow-y:auto;overscroll-behavior:contain}
 .gw-help[data-open="1"]{transform:translateX(0)}
 .help-head{display:flex;align-items:center;justify-content:space-between;padding:14px 18px 10px 18px;border-bottom:1px solid var(--gw-border-soft);position:sticky;top:0;background:var(--gw-bg-page);z-index:1}
 .help-head h1{margin:0;font-size:16px;font-weight:600;color:var(--gw-text)}
 .help-close{background:none;border:0;cursor:pointer;font:inherit;font-size:18px;line-height:1;color:var(--gw-text-muted);padding:2px 6px}
 .help-close:hover{color:var(--gw-text)}
 .help-body{padding:14px 18px 24px 18px;font-size:14px;line-height:1.55;color:var(--gw-text-soft)}
 .help-body h2{font-size:11px;text-transform:uppercase;letter-spacing:.06em;color:var(--gw-text-muted);margin:16px 0 6px 0;font-weight:600}
 .help-body h3{font-size:14px;margin:12px 0 4px 0;color:var(--gw-text);font-weight:600}
 .help-body p{margin:6px 0 10px 0}
 .help-body ul,.help-body ol{padding-left:20px;margin:6px 0 10px 0}
 .help-body li{margin:3px 0}
 .help-body code{background:var(--gw-bg-control);padding:1px 5px;border-radius:3px;font-size:12px;color:var(--gw-text)}
 .help-body pre{background:var(--gw-bg-control);padding:8px 10px;border-radius:4px;overflow-x:auto;font-size:12px;line-height:1.45}
 .help-body pre code{background:none;padding:0}
 .help-body a{color:var(--gw-accent)}
 .help-body strong{color:var(--gw-text)}
 .gw-help-toggle{background:none;border:1px solid var(--gw-border);color:var(--gw-text-muted);padding:4px 10px;border-radius:4px;cursor:pointer;font:inherit;font-size:12px;margin-left:6px}
 .gw-help-toggle:hover{color:var(--gw-text);border-color:var(--gw-border-hover)}
 @media (max-width:720px){.gw-help{width:100vw;box-shadow:none}}
</style>`

const gwDayCount = 7

// gwDayColors: rotating per-day colors (widely-spaced hues, lightness raised to
// clear WCAG 4.5:1 on var(--gw-bg-page)). Indexed by epochDay % gwDayCount.
var gwDayColors = buildDayColors()

func buildDayColors() [gwDayCount]string {
	var out [gwDayCount]string
	for i := 0; i < gwDayCount; i++ {
		hue := float64(i) * (360.0 / gwDayCount)
		const sat = 0.6
		light := 0.55
		for light < 0.92 {
			r, g, b := hslToRGB(hue, sat, light)
			if relLuminance(r, g, b) >= 0.22 {
				break
			}
			light += 0.02
		}
		r, g, b := hslToRGB(hue, sat, light)
		out[i] = fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return out
}

var gwDayColorStyle = func() string {
	var b strings.Builder
	b.WriteString("<style>")
	for i, c := range gwDayColors {
		fmt.Fprintf(&b, ".gw-content .gw-dc-%d{color:%s}", i, c)
	}
	b.WriteString("</style>")
	return b.String()
}()

var gwHourColors = buildHourColors()

func buildHourColors() [24]string {
	var out [24]string
	for h := 0; h < 24; h++ {
		hue := float64(h) * 15.0
		const sat = 0.55
		light := 0.55
		for light < 0.92 {
			r, g, b := hslToRGB(hue, sat, light)
			if relLuminance(r, g, b) >= 0.22 {
				break
			}
			light += 0.02
		}
		r, g, b := hslToRGB(hue, sat, light)
		out[h] = fmt.Sprintf("#%02x%02x%02x", r, g, b)
	}
	return out
}

func hslToRGB(h, s, l float64) (int, int, int) {
	c := (1 - math.Abs(2*l-1)) * s
	hp := h / 60.0
	x := c * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r1, g1, b1 float64
	switch {
	case hp < 1:
		r1, g1, b1 = c, x, 0
	case hp < 2:
		r1, g1, b1 = x, c, 0
	case hp < 3:
		r1, g1, b1 = 0, c, x
	case hp < 4:
		r1, g1, b1 = 0, x, c
	case hp < 5:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	m := l - c/2
	return int(math.Round((r1 + m) * 255)), int(math.Round((g1 + m) * 255)), int(math.Round((b1 + m) * 255))
}

func relLuminance(r, g, b int) float64 {
	f := func(v int) float64 {
		c := float64(v) / 255.0
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*f(r) + 0.7152*f(g) + 0.0722*f(b)
}

var gwColorStyle = func() string {
	var b strings.Builder
	b.WriteString("<style>")
	for h, c := range gwHourColors {
		fmt.Fprintf(&b, ".gw-content .gw-tc-%d{color:%s}", h, c)
	}
	b.WriteString("</style>")
	return b.String()
}()

var gwLayoutTmpl = func() *template.Template {
	t := template.New("gwshell")
	template.Must(t.New("gwrail").Parse(`<nav class="gw-rail">
<div class="gw-railhead"><span class="ico"><svg viewBox="0 0 64 64" fill="currentColor" aria-hidden="true"><circle cx="32.00" cy="4.00" r="2.50" opacity="0.65"/><circle cx="24.47" cy="5.93" r="2.77" opacity="0.74"/><circle cx="33.24" cy="7.86" r="1.94" opacity="0.47"/><circle cx="42.38" cy="9.79" r="3.03" opacity="0.82"/><circle cx="12.98" cy="11.72" r="2.37" opacity="0.61"/><circle cx="49.85" cy="13.66" r="2.05" opacity="0.51"/><circle cx="26.11" cy="15.59" r="3.36" opacity="0.92"/><circle cx="20.96" cy="17.52" r="1.66" opacity="0.38"/><circle cx="55.51" cy="19.45" r="2.84" opacity="0.76"/><circle cx="8.05" cy="21.38" r="2.89" opacity="0.77"/><circle cx="43.28" cy="23.31" r="1.55" opacity="0.35"/><circle cx="40.13" cy="25.24" r="3.52" opacity="0.97"/><circle cx="8.14" cy="27.17" r="1.96" opacity="0.48"/><circle cx="59.20" cy="29.10" r="2.27" opacity="0.58"/><circle cx="15.91" cy="31.03" r="3.40" opacity="0.94"/><circle cx="28.40" cy="32.97" r="1.41" opacity="0.30"/><circle cx="53.30" cy="34.90" r="3.21" opacity="0.87"/><circle cx="4.44" cy="36.83" r="2.54" opacity="0.66"/><circle cx="51.26" cy="38.76" r="1.75" opacity="0.41"/><circle cx="30.77" cy="40.69" r="3.54" opacity="0.98"/><circle cx="15.40" cy="42.62" r="1.72" opacity="0.40"/><circle cx="56.81" cy="44.55" r="2.63" opacity="0.69"/><circle cx="12.33" cy="46.48" r="3.04" opacity="0.82"/><circle cx="36.98" cy="48.41" r="1.63" opacity="0.37"/><circle cx="42.52" cy="50.34" r="3.22" opacity="0.88"/><circle cx="13.60" cy="52.28" r="2.27" opacity="0.58"/><circle cx="47.48" cy="54.21" r="2.22" opacity="0.56"/><circle cx="26.52" cy="56.14" r="3.01" opacity="0.81"/><circle cx="28.54" cy="58.07" r="2.12" opacity="0.53"/><circle cx="32.00" cy="60.00" r="2.50" opacity="0.65"/></svg></span><span class="label">nimblegate</span></div>
<a href="/{{if .ActiveRepo}}?repo={{.ActiveRepo}}{{end}}" class="gw-railitem{{if eq .ActiveSection "feed"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><line x1="3" y1="6" x2="3.01" y2="6"/><line x1="3" y1="12" x2="3.01" y2="12"/><line x1="3" y1="18" x2="3.01" y2="18"/></svg></span><span class="label">Feed</span></a>
<a href="/stats{{if .ActiveRepo}}?repo={{.ActiveRepo}}{{end}}" class="gw-railitem{{if eq .ActiveSection "stats"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="18" y1="20" x2="18" y2="10"/><line x1="12" y1="20" x2="12" y2="4"/><line x1="6" y1="20" x2="6" y2="14"/></svg></span><span class="label">Stats</span></a>
<a href="/reports" class="gw-railitem{{if eq .ActiveSection "reports"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="8" y1="13" x2="16" y2="13"/><line x1="8" y1="17" x2="16" y2="17"/></svg></span><span class="label">Reports</span></a>
<a href="/auto-pr" class="gw-railitem{{if eq .ActiveSection "auto-pr"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg></span><span class="label">Auto-PR</span></a>
<a href="/frames" class="gw-railitem{{if eq .ActiveSection "frames"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/><path d="m9 12 2 2 4-4"/></svg></span><span class="label">Frames</span></a>
<a href="/ssh-keys" class="gw-railitem{{if eq .ActiveSection "ssh-keys"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6"/><path d="m15.5 7.5 3 3L22 7l-3-3"/></svg></span><span class="label">Keys</span></a>
<a href="/repos" class="gw-railitem{{if eq .ActiveSection "repos"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 3h18v4H3z"/><path d="M3 11h18v4H3z"/><path d="M3 19h18v4H3z"/><rect x="3" y="3" width="18" height="4" rx="1"/><rect x="3" y="11" width="18" height="4" rx="1"/><rect x="3" y="19" width="4" height="4" rx="1"/></svg></span><span class="label">Repos</span></a>
<a href="/policy{{if .ActiveRepo}}?repo={{.ActiveRepo}}{{end}}" class="gw-railitem{{if eq .ActiveSection "policy"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="21" y1="4" x2="14" y2="4"/><line x1="10" y1="4" x2="3" y2="4"/><line x1="21" y1="12" x2="12" y2="12"/><line x1="8" y1="12" x2="3" y2="12"/><line x1="21" y1="20" x2="16" y2="20"/><line x1="12" y1="20" x2="3" y2="20"/><line x1="14" y1="2" x2="14" y2="6"/><line x1="8" y1="10" x2="8" y2="14"/><line x1="16" y1="18" x2="16" y2="22"/></svg></span><span class="label">Policy</span></a>
<a href="/events" class="gw-railitem{{if eq .ActiveSection "events"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg></span><span class="label">Events</span></a>
<!-- seam: New-check entry attaches here (P2 check-authoring editor) -->
<a href="/health" class="gw-railitem{{if eq .ActiveSection "health"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/></svg></span><span class="label">Health</span></a>
<a href="/settings" class="gw-railitem gw-bottom{{if eq .ActiveSection "settings"}} active{{end}}"><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg></span><span class="label">Settings</span></a>
<button class="gw-railtoggle" data-rail-toggle><span class="ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m11 17-5-5 5-5"/><path d="m18 17-5-5 5-5"/></svg></span><span class="label">collapse</span></button>
</nav>`))
	template.Must(t.New("gwtop").Parse(`<div class="gw-top"><button class="gw-menu" data-rail-open aria-label="open menu"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/></svg></button><label class="gw-repolabel" for="gw-topbar-repo">Repo:</label><select id="gw-topbar-repo" data-repo-switch><option value="">all repos</option>{{$cur := .ActiveRepo}}{{range .Repos}}<option value="{{.}}"{{if eq . $cur}} selected{{end}}>{{.}}</option>{{end}}</select><span class="modebadge" title="gateway mode (read-only)">{{.Mode}}</span><span class="spacer"></span><button type="button" class="gw-help-toggle" data-help-toggle aria-label="Toggle help">?</button>{{if .AuthEnabled}}<form method="post" action="/logout" style="margin:0;display:inline"><button type="submit" style="background:none;border:1px solid var(--gw-border);color:var(--gw-text-muted);padding:4px 10px;border-radius:4px;cursor:pointer;font:inherit;font-size:12px" title="Sign out">Sign out</button></form>{{end}}</div>`))
	template.Must(t.New("layout").Parse(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Title}}</title><link rel="icon" type="image/svg+xml" href="/static/favicon.svg">` + dashStyle + gwShellStyle + gwColorStyle + gwDayColorStyle + `
<script src="/static/htmx.min.js"></script><script src="/static/gwshell.js" defer></script></head>
<body{{if .CSRFToken}} hx-headers='{"X-CSRF-Token":"{{.CSRFToken}}"}'{{end}}>
<div class="gw-shell" data-rail="expanded">{{template "gwrail" .Chrome}}<div class="gw-backdrop" data-rail-close></div><div class="gw-main">{{template "gwtop" .Chrome}}<!-- seam: feed/paging controls -->
<div class="gw-content">{{.Content}}</div></div></div><aside class="gw-help" id="gw-help" hidden></aside><script>
(function(){
  var KEY="nbg-help-open";
  var aside=document.getElementById("gw-help");
  if(!aside) return;
  function pagePath(){ return location.pathname; }
  function fetchHelp(){
    var url="/help?page="+encodeURIComponent(pagePath());
    fetch(url,{credentials:"same-origin"}).then(function(r){return r.text();}).then(function(html){
      aside.innerHTML=html;
      bindClose();
    });
  }
  function bindClose(){
    var btn=aside.querySelector(".help-close");
    if(btn) btn.addEventListener("click",close);
  }
  function open(){
    if(!aside.dataset.loaded){ fetchHelp(); aside.dataset.loaded="1"; }
    aside.hidden=false;
    requestAnimationFrame(function(){ aside.setAttribute("data-open","1"); });
    try{ localStorage.setItem(KEY,"1"); }catch(e){}
  }
  function close(){
    aside.removeAttribute("data-open");
    setTimeout(function(){ aside.hidden=true; }, 200);
    try{ localStorage.setItem(KEY,"0"); }catch(e){}
  }
  function toggle(){ aside.hasAttribute("data-open")?close():open(); }
  document.addEventListener("click",function(e){
    var t=e.target.closest("[data-help-toggle]");
    if(t){ e.preventDefault(); toggle(); }
  });
  document.addEventListener("keydown",function(e){
    if(e.key==="Escape" && aside.hasAttribute("data-open")) close();
  });
  try{
    if(localStorage.getItem(KEY)==="1") open();
  }catch(e){}
})();
</script></body></html>`))
	return t
}()

func renderGwShell(w http.ResponseWriter, l gwLayout) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := gwLayoutTmpl.ExecuteTemplate(w, "layout", l); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// gatewayMode reports the read-only mode badge for a repo: off|observe|enforce,
// or "-" when viewing all repos or the policy can't be read. No write path.
func gatewayMode(policyRoot, repo string) string {
	if repo == "" {
		return "-"
	}
	p, err := (gateway.FilePolicyStore{Root: policyRoot}).Load(repo)
	if err != nil {
		return "-"
	}
	switch {
	case !p.Enabled:
		return "off"
	case p.Observe:
		return "observe"
	default:
		return "enforce"
	}
}

// buildChrome assembles the chrome view for a page. section is one of
// feed|stats|frames|policy|ssh-keys; activeRepo is the (validated) ?repo=
// value or "".
func buildChrome(section, activeRepo, policyRoot string) chromeData {
	return chromeData{
		Build:         version.Resolved(),
		Mode:          gatewayMode(policyRoot, activeRepo),
		Repos:         listGatewayRepos(policyRoot),
		ActiveRepo:    activeRepo,
		ActiveSection: section,
		AuthEnabled:   authEnabledForChrome,
	}
}

// authEnabledForChrome is a package-level flag set once at dashboard startup;
// the chrome render path is too widespread to thread the auth-mode flag
// through every call site, and the value is invariant for the process's
// lifetime so a package var is fine.
var authEnabledForChrome bool
