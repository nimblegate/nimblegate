// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
)

// loadActiveLoopsByRepo scans <policyRoot>/<repo>/pr-comment-state/*.json for
// every registered repo and returns the most-recently-updated PRState per
// repo, projected as an ActiveLoopView. Used by /feed to surface an inline
// "Active loop" row + Reset button.
//
// The spec writes "rows for PRs with active loop state" but the AuditRecord
// has no PR number, so per-row alignment is impossible. We compromise by
// keying on repo: each repo has at most one or two active loops at a time,
// and the operator's "I see a stuck loop" signal lands on the most recent
// rejected row for the affected repo, which is exactly where they're looking.
func loadActiveLoopsByRepo(policyRoot string) map[string]*gateway.ActiveLoopView {
	out := map[string]*gateway.ActiveLoopView{}
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "pr-comment-state"))
	for _, dir := range matches {
		repo := filepath.Base(filepath.Dir(dir))
		view := newestActiveLoop(policyRoot, repo, dir)
		if view != nil {
			out[repo] = view
		}
	}
	return out
}

// newestActiveLoop reads every *.json in stateDir and returns the most
// recently attempted PRState as an ActiveLoopView. Nil = no active loops.
func newestActiveLoop(policyRoot, repo, stateDir string) *gateway.ActiveLoopView {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return nil
	}
	var best *notification.PRState
	var bestPR int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		prStr := strings.TrimSuffix(e.Name(), ".json")
		pr, err := strconv.Atoi(prStr)
		if err != nil || pr <= 0 {
			continue
		}
		path := filepath.Join(stateDir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var s notification.PRState
		if err := json.Unmarshal(b, &s); err != nil {
			continue
		}
		if s.Loop.Exhausted {
			continue // exhausted loops are no longer "active"; nothing to reset that helps
		}
		if best == nil ||
			s.Loop.AttemptCount > best.Loop.AttemptCount ||
			(s.Loop.AttemptCount == best.Loop.AttemptCount && pr > bestPR) {
			cp := s
			best = &cp
			bestPR = pr
		}
	}
	if best == nil {
		return nil
	}
	return &gateway.ActiveLoopView{
		PRNumber:     bestPR,
		AttemptCount: best.Loop.AttemptCount,
		MaxAttempts:  best.Loop.MaxAttempts,
		CurrentBot:   best.Mention.CurrentBot,
		ResetURL:     fmt.Sprintf("/feed/reset-loop?repo=%s&pr=%d", url.QueryEscape(repo), bestPR),
	}
}

// applyActiveLoops attaches each repo's newest ActiveLoopView to the FIRST
// rejected row of that repo in the (already newest-first) view model. The
// operator sees a single "Active loop" indicator per repo, on the row where
// it's most actionable (most recent reject). Rows without a matching active
// loop are unchanged.
func applyActiveLoops(vm *gateway.ViewModel, loops map[string]*gateway.ActiveLoopView) {
	if len(loops) == 0 {
		return
	}
	used := map[string]bool{}
	for i := range vm.Rows {
		r := &vm.Rows[i]
		if r.Accept || used[r.Repo] {
			continue
		}
		if v, ok := loops[r.Repo]; ok && v != nil {
			r.ActiveLoop = v
			used[r.Repo] = true
		}
	}
}

// applyNotifOff annotates the first rejected row per repo that has an upstream
// but notifications off (and no active loop) with a nudge explaining no PR
// comment was posted. Mirrors applyActiveLoops: operator-side only, one note
// per repo. Closes the silent gap where the default-off rail produces no
// feedback and the operator wonders why the auto-PR loop did nothing.
func applyNotifOff(vm *gateway.ViewModel, policyRoot string) {
	used := map[string]bool{}
	for i := range vm.Rows {
		r := &vm.Rows[i]
		if r.Accept || used[r.Repo] {
			continue
		}
		if r.ActiveLoop != nil {
			used[r.Repo] = true // loop running = notifications on; no nudge for this repo
			continue
		}
		used[r.Repo] = true
		p, err := (gateway.FilePolicyStore{Root: policyRoot}).Load(r.Repo)
		if err != nil || p.UpstreamURL == "" {
			continue // no upstream = no PR host; the nudge would be meaningless
		}
		if p.Notification != nil && p.Notification.Enabled {
			continue // rail is on; the loop is wired
		}
		r.NotifOff = &gateway.NotifOffView{EnableURL: "/auto-pr/config?repo=" + url.QueryEscape(r.Repo)}
	}
}

// feedResetLoopHandler removes the PRState file for (repo, pr). POST-only
// (CSRF-checked when --allow-edits is on). No-op when the state file is
// absent - operator may have clicked twice or already accepted the PR.
func feedResetLoopHandler(policyRoot, csrfToken string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if csrfToken != "" && !csrfOK(r, csrfToken) {
			http.Error(w, "csrf", http.StatusForbidden)
			return
		}
		repo := r.URL.Query().Get("repo")
		if repo == "" {
			repo = r.FormValue("repo")
		}
		if repo == "" || !validRepoName(repo) {
			http.Error(w, "missing or invalid repo", http.StatusBadRequest)
			return
		}
		prStr := r.URL.Query().Get("pr")
		if prStr == "" {
			prStr = r.FormValue("pr")
		}
		pr, err := strconv.Atoi(prStr)
		if err != nil || pr <= 0 {
			http.Error(w, "missing or invalid pr", http.StatusBadRequest)
			return
		}
		// DeletePRState already swallows ENOENT (missing file = no-op) per
		// state.go, so any error we see here is a real I/O failure.
		if err := notification.DeletePRState(policyRoot, repo, pr); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Caller is expected to be htmx-driven; an empty 200 lets the row's
		// hx-swap clear the loop strip. Plain-browser clients get a brief
		// confirmation, then the next /feed refresh updates the view.
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<span data-loop-reset="1">reset</span>`)
	}
}
