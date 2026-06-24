// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gwicons"
)

// renderNotificationRailSection writes the collapsible Notification rail
// editor into w. Per spec §7.4 it maps 1:1 to the [notification.*] TOML
// section. The view is initially collapsed because most users on single-bot
// defaults won't expand it.
//
// view is the parsed-and-defaulted state loaded from gateway.toml. saveErr is
// non-empty when a POST failed validation (we re-render with errors inline
// instead of overwriting). If the form has just been saved, saveOK = true and
// we surface a small "Saved." banner above the inputs.
// notifRailRenderOpts customizes where the form posts back to + which page
// the save handler redirects to after success. Both default to /policy when
// zero-valued (backwards-compat with the original /policy embedding).
type notifRailRenderOpts struct {
	ReturnTo string // URL path to redirect to after save (e.g. "/auto-pr/config" or "/policy")
	Embed    string // "details" = wrap in <details><summary> (legacy Policy), "panel" = bare panel (new Auto-PR Setup tab)
}

func renderNotificationRailSection(w io.Writer, repo string, view notifRailView, allowEdits bool, csrfToken, saveErr string, saveOK bool) {
	renderNotificationRailSectionWith(w, repo, view, allowEdits, csrfToken, saveErr, saveOK, notifRailRenderOpts{})
}

// notifRailFormStyle is the per-field layout for the notification-rail form.
// Each text/number/textarea/select field renders on its own line with the
// label above; checkbox/radio rows stay inline. Each <details> block (multi-bot
// rotation, loop guardrails, delivery) gets framed padding so its nested fields
// are visually grouped instead of bleeding into the parent form.
const notifRailFormStyle = `<style>
.gw-notifrail-form{font-size:13px;margin-top:12px}
.gw-notifrail-form > label,
.gw-notifrail-form details > label{display:block;margin:14px 0}
.gw-notifrail-form label > input[type="text"],
.gw-notifrail-form label > input[type="password"],
.gw-notifrail-form label > input[type="number"],
.gw-notifrail-form label > textarea{display:block;margin-top:6px;width:100%;max-width:520px;padding:7px 9px;font-size:13px;font-family:inherit;background:var(--gw-bg-soft);border:1px solid var(--gw-border);border-radius:4px;color:var(--gw-text);box-sizing:border-box}
.gw-notifrail-form label > textarea{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;resize:vertical;min-height:60px}
.gw-notifrail-form label > input[type="text"]:focus,
.gw-notifrail-form label > input[type="password"]:focus,
.gw-notifrail-form label > input[type="number"]:focus,
.gw-notifrail-form label > textarea:focus{outline:none;border-color:var(--gw-accent);background:var(--gw-bg-panel)}
.gw-notifrail-form label > input[type="checkbox"]{margin-right:6px;vertical-align:middle}
.gw-notifrail-form fieldset.gw-notifrail-auth{border:1px solid var(--gw-border);border-radius:4px;padding:10px 14px 12px;margin:14px 0;max-width:520px}
.gw-notifrail-form fieldset.gw-notifrail-auth legend{font-size:12px;color:var(--gw-text-muted);padding:0 6px;font-weight:500}
.gw-notifrail-form fieldset.gw-notifrail-auth label{display:inline-block;margin:4px 18px 4px 0;font-size:13px}
.gw-notifrail-form fieldset.gw-notifrail-auth label > input[type="radio"]{margin-right:5px;vertical-align:middle}
.gw-notifrail-form button:not([type="submit"]){margin-top:8px;margin-right:6px;padding:5px 11px;border:1px solid var(--gw-border);border-radius:4px;background:var(--gw-bg-soft);color:var(--gw-text-muted);font-size:12px;font-family:inherit;cursor:pointer}
.gw-notifrail-form button:not([type="submit"]):hover{background:var(--gw-bg-hover);color:var(--gw-text)}
.gw-notifrail-form details{margin:20px 0;padding:14px 16px;border:1px solid var(--gw-border-soft);border-radius:6px;background:var(--gw-bg-soft)}
.gw-notifrail-form details > summary{cursor:pointer;font-size:13px;font-weight:500;color:var(--gw-text);outline:none;list-style:revert}
.gw-notifrail-form details[open] > summary{margin-bottom:10px;padding-bottom:8px;border-bottom:1px solid var(--gw-border-soft)}
.gw-notifrail-form details > label:first-of-type{margin-top:0}
.gw-notifrail-form .gw-notifrail-actions{margin-top:24px;padding-top:16px;border-top:1px solid var(--gw-border-soft);display:flex;gap:10px;flex-wrap:wrap}
.gw-notifrail-form .gw-notifrail-actions button{padding:8px 20px;font-size:13px;font-weight:600;border:1px solid var(--gw-accent);background:var(--gw-accent);color:var(--gw-bg);border-radius:4px;cursor:pointer;font-family:inherit;margin:0}
.gw-notifrail-form .gw-notifrail-actions button[name="reset"]{background:var(--gw-bg-soft);border-color:var(--gw-border);color:var(--gw-text-muted);font-weight:500}
.gw-notifrail-form .gw-notifrail-actions button:hover{filter:brightness(1.1)}
.gw-notifrail-form .warn,.gw-notifrail-form .ok{margin:12px 0;padding:8px 12px;border-radius:4px;font-size:13px}
.gw-notifrail-form .ok{background:rgba(0,180,80,0.12);color:#5ee68e;border:1px solid rgba(94,230,142,0.3)}
</style>`

func renderNotificationRailSectionWith(w io.Writer, repo string, view notifRailView, allowEdits bool, csrfToken, saveErr string, saveOK bool, opts notifRailRenderOpts) {
	if repo == "" {
		return
	}
	returnTo := opts.ReturnTo
	if returnTo == "" {
		returnTo = "/policy"
	}
	asPanel := opts.Embed == "panel"

	open := saveErr != "" // expand the section when an error needs the operator's eye
	openAttr := ""
	if open || asPanel {
		openAttr = " open"
	}

	fmt.Fprint(w, notifRailFormStyle)
	if asPanel {
		fmt.Fprint(w, `<section class="frame gw-notifrail">`)
		fmt.Fprint(w, `<h3 class="gw-section-head">Notification rail · per-repo setup</h3>`)
	} else {
		fmt.Fprintf(w, `<details class="frame gw-notifrail"%s><summary class="gw-section-head">Notification rail</summary>`, openAttr)
	}
	fmt.Fprint(w, `<p class="sub">Webhook + PR-comment delivery for rejected pushes. Leave at defaults if you're happy with the single-bot defaults.</p>`)

	if saveErr != "" {
		fmt.Fprintf(w, `<div class="warn" data-notifrail-error="1">%s %s</div>`, gwicons.HTML("warn"), html.EscapeString(saveErr))
	} else if saveOK {
		fmt.Fprintf(w, `<div class="ok" data-notifrail-saved="1">%s Saved.</div>`, gwicons.HTML("ok"))
	}

	if !allowEdits {
		fmt.Fprint(w, `<p class="sub">Read-only mode. Start the dashboard with <code>--allow-edits</code> to enable changes.</p>`)
		if asPanel {
			fmt.Fprint(w, `</section>`)
		} else {
			fmt.Fprint(w, `</details>`)
		}
		return
	}

	// hx-post (not native method/action): csrfOK checks the X-CSRF-Token header,
	// which hx-headers only attaches to htmx-driven requests. A native submit
	// dropped the header and every save 403'd with "csrf".
	fmt.Fprintf(w, `<form class="gw-notifrail-form" hx-post="/policy/notification/save" hx-headers='{"X-CSRF-Token":"%s"}'>`, html.EscapeString(csrfToken))
	fmt.Fprintf(w, `<input type="hidden" name="repo" value="%s">`, html.EscapeString(repo))
	fmt.Fprintf(w, `<input type="hidden" name="return_to" value="%s">`, html.EscapeString(returnTo))

	// Enable + observe toggles
	fmt.Fprintf(w, `<label><input type="checkbox" name="enabled" value="1"%s> Enable notifications for this repo</label>`, checked(view.Enabled))
	fmt.Fprintf(w, `<label><input type="checkbox" name="observe_pr_comments" value="1"%s> Also send notifications in observe mode</label>`, checked(view.ObservePRComments))

	// Webhook URL + auth-mode + secret
	fmt.Fprintf(w, `<label>Webhook URL <input type="text" name="webhook_url" value="%s" placeholder="https://hooks.example.com/…"></label>`, html.EscapeString(view.WebhookURL))
	fmt.Fprint(w, `<fieldset class="gw-notifrail-auth"><legend>Auth mode</legend>`)
	for _, mode := range []struct{ value, label string }{
		{"hmac", "HMAC (recommended)"},
		{"bearer", "Bearer"},
		{"none", "None"},
	} {
		sel := ""
		if view.AuthMode == mode.value {
			sel = " checked"
		}
		fmt.Fprintf(w, `<label><input type="radio" name="auth_mode" value="%s"%s> %s</label>`, mode.value, sel, mode.label)
	}
	fmt.Fprint(w, `</fieldset>`)

	// Secret is masked by default; the [Reveal] toggle is client-side
	// (data-* attribute the JS reads on click). A separate Generate button
	// posts to the random-secret endpoint and OOB-swaps a freshly-generated
	// 32-byte hex secret into the input.
	maskedSecret := ""
	if view.HasSecret {
		maskedSecret = strings.Repeat("•", 16)
	}
	fmt.Fprintf(w, `<label>Secret <input type="password" name="secret" id="notifrail-secret" value="%s" placeholder="HMAC signing key or Bearer token"><button type="button" data-notifrail-reveal>Reveal</button><button type="button" hx-post="/policy/notification/generate-secret" hx-headers='{"X-CSRF-Token":"%s"}' hx-vals='{"repo":"%s"}' hx-target="#notifrail-secret" hx-swap="outerHTML">Generate random</button></label>`,
		html.EscapeString(maskedSecret), html.EscapeString(csrfToken), html.EscapeString(repo))
	fmt.Fprintf(w, `<label>Auth header <input type="text" name="auth_header" value="%s" placeholder="optional override (default: X-Hub-Signature-256 or Authorization)"></label>`, html.EscapeString(view.AuthHeader))

	// Mention block
	fmt.Fprintf(w, `<label>Default mention <input type="text" name="mention_default" value="%s" placeholder="@nimblegate-bot"></label>`, html.EscapeString(view.MentionDefault))
	fmt.Fprintf(w, `<label><input type="checkbox" name="auto_tag_assignees" value="1"%s> Auto-tag PR assignees + reviewers</label>`, checked(view.AutoTagAssignees))

	// Multi-bot rotation (collapsed; opt-in)
	rotationOpen := ""
	if len(view.RotationBots) > 1 {
		rotationOpen = " open"
	}
	fmt.Fprintf(w, `<details%s><summary>Multi-bot rotation (opt-in)</summary>`, rotationOpen)
	fmt.Fprintf(w, `<label>Bots (ordered, one per line) <textarea name="rotation_bots" rows="3" placeholder="@nimblegate-bot&#10;@nimblegate-bot-2">%s</textarea></label>`, html.EscapeString(strings.Join(view.RotationBots, "\n")))
	fmt.Fprintf(w, `<label>Attempts per bot <input type="number" name="attempts_per_bot" min="1" value="%d"></label>`, view.AttemptsPerBot)
	fmt.Fprintf(w, `<label><input type="checkbox" name="rotate_on_repeat_finding" value="1"%s> Rotate immediately on same finding</label>`, checked(view.RotateOnRepeatFinding))
	fmt.Fprintf(w, `<label>Fallback human <input type="text" name="fallback_human" value="%s" placeholder="@operator"></label>`, html.EscapeString(view.FallbackHuman))
	fmt.Fprint(w, `</details>`)

	// Loop guardrails (collapsed; "defaults are fine")
	fmt.Fprint(w, `<details><summary>Loop guardrails (defaults, usually leave alone)</summary>`)
	fmt.Fprintf(w, `<label>Max attempts <input type="number" name="loop_max_attempts" min="1" value="%d"></label>`, view.LoopMaxAttempts)
	fmt.Fprintf(w, `<label>Cooldown threshold count <input type="number" name="cooldown_threshold_count" min="1" value="%d"></label>`, view.CooldownThresholdCount)
	fmt.Fprintf(w, `<label>Cooldown threshold window <input type="text" name="cooldown_threshold_window" value="%s" placeholder="5m"></label>`, html.EscapeString(view.CooldownThresholdWindow))
	fmt.Fprintf(w, `<label>Cooldown duration <input type="text" name="cooldown_duration" value="%s" placeholder="10m"></label>`, html.EscapeString(view.CooldownDuration))
	fmt.Fprint(w, `</details>`)

	// Delivery (collapsed)
	fmt.Fprint(w, `<details><summary>Delivery (defaults)</summary>`)
	fmt.Fprintf(w, `<label>Max attempts <input type="number" name="delivery_max_attempts" min="1" value="%d"></label>`, view.DeliveryMaxAttempts)
	fmt.Fprintf(w, `<label>Backoff schedule (comma-separated) <input type="text" name="backoff_schedule" value="%s" placeholder="1m, 5m, 30m, 2h"></label>`, html.EscapeString(strings.Join(view.BackoffSchedule, ", ")))
	fmt.Fprint(w, `</details>`)

	fmt.Fprint(w, `<div class="gw-notifrail-actions"><button type="submit">Save</button> <button type="submit" name="reset" value="1">Reset section to defaults</button></div>`)
	if asPanel {
		fmt.Fprint(w, `</form></section>`)
	} else {
		fmt.Fprint(w, `</form></details>`)
	}
}

func checked(b bool) string {
	if b {
		return " checked"
	}
	return ""
}

// notifRailView is the form-side view of the [notification.*] section. All
// fields are strings or ints because that's how a form serializes them.
// loadNotifRailView reads the gateway.toml and projects to this shape; save
// goes the other way.
type notifRailView struct {
	Enabled                 bool
	ObservePRComments       bool
	WebhookURL              string
	AuthMode                string
	HasSecret               bool // true if a secret is on file (don't echo it back)
	AuthHeader              string
	MentionDefault          string
	AutoTagAssignees        bool
	RotationBots            []string
	AttemptsPerBot          int
	RotateOnRepeatFinding   bool
	FallbackHuman           string
	LoopMaxAttempts         int
	CooldownThresholdCount  int
	CooldownThresholdWindow string
	CooldownDuration        string
	DeliveryMaxAttempts     int
	BackoffSchedule         []string
}

// defaultNotifRailView returns the spec §7.1 defaults projected into the form
// view shape. Used both when a repo has no [notification] section yet and
// when the operator clicks "Reset section to defaults."
func defaultNotifRailView() notifRailView {
	return notifRailView{
		AuthMode:                "hmac",
		MentionDefault:          "@nimblegate-bot",
		AutoTagAssignees:        true,
		AttemptsPerBot:          2,
		LoopMaxAttempts:         5,
		CooldownThresholdCount:  3,
		CooldownThresholdWindow: "5m",
		CooldownDuration:        "10m",
		DeliveryMaxAttempts:     20,
		BackoffSchedule:         []string{"1m", "5m", "30m", "2h"},
	}
}

// loadNotifRailView reads gateway.toml for repo and projects the
// [notification] section onto the form-side view. Missing section = defaults.
// Errors fall through to defaults rather than blocking the page render.
func loadNotifRailView(policyRoot, repo string) notifRailView {
	view := defaultNotifRailView()
	path := filepath.Join(policyRoot, repo, "gateway.toml")
	var raw struct {
		Notification *struct {
			Enabled           bool `toml:"enabled"`
			ObservePRComments bool `toml:"observe-pr-comments"`
			Webhook           *struct {
				URL        string `toml:"url"`
				AuthMode   string `toml:"auth-mode"`
				Secret     string `toml:"secret"`
				AuthHeader string `toml:"auth-header"`
			} `toml:"webhook"`
			Mention *struct {
				Default            string `toml:"default"`
				IncludePRAssignees bool   `toml:"include-pr-assignees"`
				Rotation           *struct {
					Bots                  []string `toml:"bots"`
					AttemptsPerBot        int      `toml:"attempts-per-bot"`
					RotateOnRepeatFinding bool     `toml:"rotate-on-repeat-finding"`
					FallbackHuman         string   `toml:"fallback-human"`
				} `toml:"rotation"`
			} `toml:"mention"`
			Loop *struct {
				MaxAttempts             int    `toml:"max-attempts"`
				CooldownThresholdCount  int    `toml:"cooldown-threshold-count"`
				CooldownThresholdWindow string `toml:"cooldown-threshold-window"`
				CooldownDuration        string `toml:"cooldown-duration"`
			} `toml:"loop"`
			Delivery *struct {
				MaxAttempts     int      `toml:"max-attempts"`
				BackoffSchedule []string `toml:"backoff-schedule"`
			} `toml:"delivery"`
		} `toml:"notification"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil || raw.Notification == nil {
		return view
	}
	n := raw.Notification
	view.Enabled = n.Enabled
	view.ObservePRComments = n.ObservePRComments
	if n.Webhook != nil {
		view.WebhookURL = n.Webhook.URL
		if n.Webhook.AuthMode != "" {
			view.AuthMode = n.Webhook.AuthMode
		}
		view.HasSecret = n.Webhook.Secret != ""
		view.AuthHeader = n.Webhook.AuthHeader
	}
	if n.Mention != nil {
		if n.Mention.Default != "" {
			view.MentionDefault = n.Mention.Default
		}
		view.AutoTagAssignees = n.Mention.IncludePRAssignees
		if n.Mention.Rotation != nil {
			view.RotationBots = n.Mention.Rotation.Bots
			if n.Mention.Rotation.AttemptsPerBot > 0 {
				view.AttemptsPerBot = n.Mention.Rotation.AttemptsPerBot
			}
			view.RotateOnRepeatFinding = n.Mention.Rotation.RotateOnRepeatFinding
			view.FallbackHuman = n.Mention.Rotation.FallbackHuman
		}
	}
	if n.Loop != nil {
		if n.Loop.MaxAttempts > 0 {
			view.LoopMaxAttempts = n.Loop.MaxAttempts
		}
		if n.Loop.CooldownThresholdCount > 0 {
			view.CooldownThresholdCount = n.Loop.CooldownThresholdCount
		}
		if n.Loop.CooldownThresholdWindow != "" {
			view.CooldownThresholdWindow = n.Loop.CooldownThresholdWindow
		}
		if n.Loop.CooldownDuration != "" {
			view.CooldownDuration = n.Loop.CooldownDuration
		}
	}
	if n.Delivery != nil {
		if n.Delivery.MaxAttempts > 0 {
			view.DeliveryMaxAttempts = n.Delivery.MaxAttempts
		}
		if len(n.Delivery.BackoffSchedule) > 0 {
			view.BackoffSchedule = n.Delivery.BackoffSchedule
		}
	}
	return view
}

// notifRailHandlers groups the POST endpoints. CSRF + repo validation gate
// every write; failures return inline errors (rendered back into the section)
// rather than overwriting a partial-form save.
type notifRailHandlers struct {
	policyRoot string
	token      string
}

func (h notifRailHandlers) save(w http.ResponseWriter, r *http.Request) {
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

	// Reset-to-defaults short-circuits validation by writing the spec defaults.
	if r.PostForm.Get("reset") == "1" {
		view := defaultNotifRailView()
		if err := writeNotifRailTOML(h.policyRoot, repo, view, ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		redirectAfterAction(w, r, safeReturn(r.PostForm.Get("return_to"), repo)+"&notifrail=saved")
		return
	}

	view, secret, errMsg := parseNotifRailForm(r.PostForm)
	if errMsg != "" {
		redirectAfterAction(w, r, safeReturn(r.PostForm.Get("return_to"), repo)+"&notifrail_err="+httpURLEncode(errMsg))
		return
	}
	if err := writeNotifRailTOML(h.policyRoot, repo, view, secret); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	redirectAfterAction(w, r, safeReturn(r.PostForm.Get("return_to"), repo)+"&notifrail=saved")
}

// safeReturn validates a posted return_to URL against an allowlist and appends
// ?repo=<repo>. Rejects anything outside the known notification-rail edit
// pages - protects against open-redirect via crafted form input.
func safeReturn(returnTo, repo string) string {
	switch returnTo {
	case "/auto-pr/config":
		return "/auto-pr/config?repo=" + repo
	case "/policy", "":
		return "/policy?repo=" + repo
	default:
		return "/policy?repo=" + repo
	}
}

func (h notifRailHandlers) generateSecret(w http.ResponseWriter, r *http.Request) {
	if !csrfOK(r, h.token) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	repo := r.FormValue("repo")
	if repo == "" || !validRepoName(repo) {
		http.Error(w, "missing or invalid repo", http.StatusBadRequest)
		return
	}
	secret := newHMACSecret()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Return the actual secret in the response - operator copies it out
	// from this single response, then a regular Save with the secret in
	// the form persists it. We don't GET-echo it again on later page loads.
	fmt.Fprintf(w, `<input type="password" name="secret" id="notifrail-secret" value="%s">`, html.EscapeString(secret))
}

// parseNotifRailForm pulls form values into a notifRailView + extracts the
// secret string (kept separate so callers can decide whether to persist it
// when the field is blank - blank means "leave existing on file untouched").
// Returns a non-empty errMsg on validation failure.
func parseNotifRailForm(form map[string][]string) (notifRailView, string, string) {
	get := func(k string) string {
		if v, ok := form[k]; ok && len(v) > 0 {
			return v[0]
		}
		return ""
	}
	getInt := func(k string, def int) int {
		s := get(k)
		if s == "" {
			return def
		}
		n := 0
		if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
			return def
		}
		return n
	}
	view := defaultNotifRailView()
	view.Enabled = get("enabled") == "1"
	view.ObservePRComments = get("observe_pr_comments") == "1"
	view.WebhookURL = strings.TrimSpace(get("webhook_url"))
	if m := get("auth_mode"); m != "" {
		view.AuthMode = m
	}
	view.AuthHeader = strings.TrimSpace(get("auth_header"))
	if d := get("mention_default"); d != "" {
		view.MentionDefault = d
	}
	view.AutoTagAssignees = get("auto_tag_assignees") == "1"
	if bots := get("rotation_bots"); bots != "" {
		var out []string
		for _, line := range strings.Split(bots, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
		view.RotationBots = out
	}
	view.AttemptsPerBot = getInt("attempts_per_bot", view.AttemptsPerBot)
	view.RotateOnRepeatFinding = get("rotate_on_repeat_finding") == "1"
	view.FallbackHuman = strings.TrimSpace(get("fallback_human"))
	view.LoopMaxAttempts = getInt("loop_max_attempts", view.LoopMaxAttempts)
	view.CooldownThresholdCount = getInt("cooldown_threshold_count", view.CooldownThresholdCount)
	if v := get("cooldown_threshold_window"); v != "" {
		view.CooldownThresholdWindow = v
	}
	if v := get("cooldown_duration"); v != "" {
		view.CooldownDuration = v
	}
	view.DeliveryMaxAttempts = getInt("delivery_max_attempts", view.DeliveryMaxAttempts)
	if sched := get("backoff_schedule"); sched != "" {
		var out []string
		for _, item := range strings.Split(sched, ",") {
			if it := strings.TrimSpace(item); it != "" {
				out = append(out, it)
			}
		}
		view.BackoffSchedule = out
	}

	secret := get("secret")
	// HMAC + no secret in form is OK at parse time - writeNotifRailTOML
	// preserves the on-file secret. The hard validation error fires only
	// when the on-file secret is also empty.
	_ = secret

	// Light syntactic validation we can catch here: durations must parse.
	if _, err := time.ParseDuration(view.CooldownThresholdWindow); err != nil {
		return view, secret, "cooldown threshold window must be a duration (e.g. '5m'): " + err.Error()
	}
	if _, err := time.ParseDuration(view.CooldownDuration); err != nil {
		return view, secret, "cooldown duration must be a duration (e.g. '10m'): " + err.Error()
	}
	for i, s := range view.BackoffSchedule {
		if _, err := time.ParseDuration(s); err != nil {
			return view, secret, fmt.Sprintf("backoff schedule item %d (%q) must be a duration: %s", i+1, s, err.Error())
		}
	}

	return view, secret, ""
}

// httpURLEncode escapes errMsg for use in a redirect query string. We don't
// import net/url at the top of the file just for this; a tiny inline escape
// is enough for our human-readable error messages.
func httpURLEncode(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteByte('+')
		case (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}

// writeNotifRailTOML re-renders gateway.toml with the new [notification]
// section, preserving non-notification keys. Atomic: temp + rename. secret
// is only written when non-empty so an unchanged input keeps the prior value.
//
// Note: we re-read the existing gateway.toml to preserve upstream-url /
// protected-refs / enabled / observe. We do NOT round-trip through the
// FilePolicyStore Load+Save path because that would surface hard-error
// validation failures from the loader before the operator gets to fix them.
func writeNotifRailTOML(policyRoot, repo string, view notifRailView, secret string) error {
	path := filepath.Join(policyRoot, repo, "gateway.toml")

	type webhookT struct {
		URL        string `toml:"url"`
		AuthMode   string `toml:"auth-mode"`
		Secret     string `toml:"secret"`
		AuthHeader string `toml:"auth-header"`
	}
	type rotationT struct {
		Bots                  []string `toml:"bots"`
		AttemptsPerBot        int      `toml:"attempts-per-bot"`
		RotateOnRepeatFinding bool     `toml:"rotate-on-repeat-finding"`
		FallbackHuman         string   `toml:"fallback-human"`
	}
	type mentionT struct {
		Default            string     `toml:"default"`
		IncludePRAssignees bool       `toml:"include-pr-assignees"`
		Rotation           *rotationT `toml:"rotation,omitempty"`
	}
	type loopT struct {
		MaxAttempts             int    `toml:"max-attempts"`
		CooldownThresholdCount  int    `toml:"cooldown-threshold-count"`
		CooldownThresholdWindow string `toml:"cooldown-threshold-window"`
		CooldownDuration        string `toml:"cooldown-duration"`
	}
	type deliveryT struct {
		MaxAttempts     int      `toml:"max-attempts"`
		BackoffSchedule []string `toml:"backoff-schedule"`
	}
	type notificationT struct {
		Enabled           bool       `toml:"enabled"`
		ObservePRComments bool       `toml:"observe-pr-comments"`
		Webhook           *webhookT  `toml:"webhook,omitempty"`
		Mention           *mentionT  `toml:"mention,omitempty"`
		Loop              *loopT     `toml:"loop,omitempty"`
		Delivery          *deliveryT `toml:"delivery,omitempty"`
	}
	type allT struct {
		UpstreamURL   string         `toml:"upstream-url,omitempty"`
		ProtectedRefs []string       `toml:"protected-refs,omitempty"`
		Enabled       bool           `toml:"enabled"`
		Observe       bool           `toml:"observe"`
		Notification  *notificationT `toml:"notification,omitempty"`
	}

	// Load the existing non-notification keys so we don't drop them.
	var prior allT
	if _, err := toml.DecodeFile(path, &prior); err != nil && !os.IsNotExist(err) {
		return err
	}

	// Decide whether to write a secret. Empty = keep existing on file.
	secretToPersist := secret
	if secretToPersist == "" {
		var rawPrev struct {
			Notification *struct {
				Webhook *struct {
					Secret string `toml:"secret"`
				} `toml:"webhook"`
			} `toml:"notification"`
		}
		if _, err := toml.DecodeFile(path, &rawPrev); err == nil && rawPrev.Notification != nil && rawPrev.Notification.Webhook != nil {
			secretToPersist = rawPrev.Notification.Webhook.Secret
		}
	}

	out := allT{
		UpstreamURL:   prior.UpstreamURL,
		ProtectedRefs: prior.ProtectedRefs,
		Enabled:       prior.Enabled,
		Observe:       prior.Observe,
		Notification: &notificationT{
			Enabled:           view.Enabled,
			ObservePRComments: view.ObservePRComments,
			Webhook: &webhookT{
				URL:        view.WebhookURL,
				AuthMode:   view.AuthMode,
				Secret:     secretToPersist,
				AuthHeader: view.AuthHeader,
			},
			Mention: &mentionT{
				Default:            view.MentionDefault,
				IncludePRAssignees: view.AutoTagAssignees,
				Rotation: &rotationT{
					Bots:                  view.RotationBots,
					AttemptsPerBot:        view.AttemptsPerBot,
					RotateOnRepeatFinding: view.RotateOnRepeatFinding,
					FallbackHuman:         view.FallbackHuman,
				},
			},
			Loop: &loopT{
				MaxAttempts:             view.LoopMaxAttempts,
				CooldownThresholdCount:  view.CooldownThresholdCount,
				CooldownThresholdWindow: view.CooldownThresholdWindow,
				CooldownDuration:        view.CooldownDuration,
			},
			Delivery: &deliveryT{
				MaxAttempts:     view.DeliveryMaxAttempts,
				BackoffSchedule: view.BackoffSchedule,
			},
		},
	}

	// Hard-error case: HMAC mode requires a non-empty secret. Mirror
	// spec §7.6 / policy.go Validate so the save round-trip refuses to
	// land a config that would block the rail at next pre-receive.
	if view.WebhookURL != "" && view.AuthMode == "hmac" && secretToPersist == "" {
		return fmt.Errorf("HMAC auth-mode requires a non-empty secret: pick another mode or click Generate random")
	}

	// Atomic write: temp + rename, so an interrupted write doesn't corrupt
	// gateway.toml (which would brick the repo's pre-receive on next push).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := toml.NewEncoder(f).Encode(out); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// newHMACSecret returns a 32-byte random hex string used as the HMAC signing
// key when the operator clicks Generate. Long enough to clear the SHA-256
// keyspace; short enough to fit on one terminal line.
func newHMACSecret() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
