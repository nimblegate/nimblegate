// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/gateway"
)

// defaultHoursForTier returns the conservative built-in per-hit hours estimate
// for the given tier (1..6). Mirrors what the engine uses when nothing else
// overrides; out-of-range tiers return 0.
func defaultHoursForTier(tier int) float64 {
	if tier < 1 || tier >= len(frames.DefaultTimeCostHoursPreventedByTier) {
		return 0
	}
	return frames.DefaultTimeCostHoursPreventedByTier[tier]
}

// timeEstimatesHandlers owns POST /policy/repo/time-estimates - the dashboard
// surface for editing the per-repo [time-estimates] override that the stats
// page reports. Read-side stays where it already is (gateway.LoadTimeEstimates +
// frames.EffectiveTimeCostHoursPrevented); this handler is the write half.
type timeEstimatesHandlers struct {
	policyRoot string
	token      string
}

// maxHoursPerHit caps each tier at 168h (one work-week). The defaults are
// 4 / 2 / 0.5 / 0.25 / 0.1 / 0.1; even very generous custom estimates
// won't approach a week. The cap rejects accidental absurd values
// (1000000) without restricting honest use.
const maxHoursPerHit = 168.0

// update is POST /policy/repo/time-estimates. Form body carries `repo` plus
// `tier-1` … `tier-6` each parsed as either a float (set override) or empty
// (clear override → revert to the built-in tier default).
func (h timeEstimatesHandlers) update(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	repo := r.FormValue("repo")
	if !validRepoName(repo) || repo == "_repos" {
		http.Error(w, "invalid repo", http.StatusBadRequest)
		return
	}
	// Confirm the repo is actually registered (avoids creating a stub
	// gateway.toml for a typo'd name).
	if _, err := (gateway.FilePolicyStore{Root: h.policyRoot}).Load(repo); err != nil {
		http.Error(w, "no such repo", http.StatusBadRequest)
		return
	}

	te, payload, err := parseTimeEstimatesForm(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := gateway.SaveTimeEstimates(h.policyRoot, repo, te); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event:   "time-estimates-update",
		Repo:    repo,
		OK:      true,
		Payload: payload,
	})
	redirectAfterAction(w, r, "/policy?repo="+repo)
}

// parseTimeEstimatesForm reads tier-1 … tier-6 from the form. Each field is
// optional; blank → leave that tier on the built-in default (nil pointer).
// Returns the resolved overrides plus a structured payload for the audit log.
// Rejects negative values + values above maxHoursPerHit with a clear error.
func parseTimeEstimatesForm(r *http.Request) (config.TimeEstimates, map[string]any, error) {
	var te config.TimeEstimates
	tiers := []**float64{&te.Tier1, &te.Tier2, &te.Tier3, &te.Tier4, &te.Tier5, &te.Tier6}
	payload := map[string]any{}
	for i, slot := range tiers {
		field := fmt.Sprintf("tier-%d", i+1)
		raw := strings.TrimSpace(r.FormValue(field))
		if raw == "" {
			// Blank = revert to default. Record in audit so the log explains
			// the reset (otherwise a no-change save and a clear-all save look
			// identical from the event payload).
			payload[field] = nil
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return config.TimeEstimates{}, nil, fmt.Errorf("tier-%d: not a number (%q)", i+1, raw)
		}
		if v < 0 {
			return config.TimeEstimates{}, nil, fmt.Errorf("tier-%d: must be ≥ 0 (got %g)", i+1, v)
		}
		if v > maxHoursPerHit {
			return config.TimeEstimates{}, nil, fmt.Errorf("tier-%d: must be ≤ %g (got %g)", i+1, maxHoursPerHit, v)
		}
		// Copy to a new var so &v is per-iteration (otherwise all six pointers
		// alias the loop variable and resolve to the last value).
		val := v
		*slot = &val
		payload[field] = v
	}
	return te, payload, nil
}

// effectiveTimeEstimates resolves the per-tier values for display: returns
// either the operator's override (if set) or the built-in default, with a
// flag for which case applies. Used by the policy-page template to render
// the current value + "(default)" hint next to each input.
type effectiveTimeEstimate struct {
	Tier      int     // 1..6
	Value     float64 // hours per hit
	IsDefault bool    // true → from built-in tier table; false → operator override
}

// resolveTimeEstimates returns the six per-tier values an operator would see
// on the policy page today: operator override where set, built-in default
// otherwise. Reads from the same path the stats page uses so the two pages
// agree on what's effective.
func resolveTimeEstimates(policyRoot, repo string) ([]effectiveTimeEstimate, error) {
	te, err := gateway.LoadTimeEstimates(policyRoot, repo)
	if err != nil && !errors.Is(err, errSkipNotFound) {
		return nil, err
	}
	overrides := []*float64{te.Tier1, te.Tier2, te.Tier3, te.Tier4, te.Tier5, te.Tier6}
	out := make([]effectiveTimeEstimate, 6)
	// Pull defaults from internal/frames; the table is 0-indexed with index 0
	// unused, tiers 1..6 at indices 1..6.
	for i := 0; i < 6; i++ {
		row := effectiveTimeEstimate{Tier: i + 1}
		if overrides[i] != nil {
			row.Value = *overrides[i]
			row.IsDefault = false
		} else {
			row.Value = defaultHoursForTier(i + 1)
			row.IsDefault = true
		}
		out[i] = row
	}
	return out, nil
}

// errSkipNotFound is a sentinel - LoadTimeEstimates already swallows
// not-found, so this is only here to make the resolveTimeEstimates signature
// honest about what errors it propagates.
var errSkipNotFound = errors.New("not-found (already handled)")
