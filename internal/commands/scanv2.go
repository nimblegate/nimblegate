// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

// V2 axis detection layered on top of the existing scanSignals walk.
// Pure helpers - same scanSignals input, returns confidence-graded
// per-axis recommendations per spec §6.1. The existing v1 recommend()
// stays the source for the dashboard scan recommendation; v2 callers
// (init prompt, dashboard /policy v2 panel) consume DetectAxes.

// Confidence mirrors spec §6.1's High / Medium / Fallback bands.
// ConfidenceNone means no signal fired for that axis.
type Confidence int

const (
	ConfidenceNone Confidence = iota
	ConfidenceFallback
	ConfidenceMedium
	ConfidenceHigh
)

// String returns the spec's label for the confidence band.
func (c Confidence) String() string {
	switch c {
	case ConfidenceHigh:
		return "high"
	case ConfidenceMedium:
		return "medium"
	case ConfidenceFallback:
		return "fallback"
	case ConfidenceNone:
		return "none"
	}
	return "unknown"
}

// AxisRecommendation is the v2 detection output: one framework + one
// platform pick (single-value axes per spec §2.1), each tagged with its
// confidence band. Conflicts lists axes where multiple high-confidence
// signals fired - E1-step-3 will turn those into an interactive prompt;
// for now they surface as a field so callers can decide their own UX.
type AxisRecommendation struct {
	Framework           string
	FrameworkConfidence Confidence
	Platform            string
	PlatformConfidence  Confidence
	// Conflicts holds axis names ("framework" / "platform") where two or
	// more independent high-confidence signals fired. Empty in the common
	// case; non-empty triggers the ambiguity prompt.
	Conflicts []string
	// CandidatesByAxis records every high-confidence candidate per axis.
	// Used by the ambiguity prompt to render the numbered choice list.
	CandidatesByAxis map[string][]string
}

// DetectAxes maps raw scanSignals to per-axis (framework, platform)
// picks with confidence per spec §6.1.
//
// Framework picks (in order - first high-confidence signal wins):
//
//	svelte  ← svelte.config.* OR any *.svelte  (high)
//	astro   ← astro.config.*                     (high)
//	go      ← go.mod                              (high)
//	html    ← *.html present, no framework signal (fallback)
//
// Platform picks:
//
//	cloudflare  ← wrangler.toml  (high)
//	static-host ← *.html, no platform signal  (fallback)
//
// React/Vue/Python signals aren't tracked by scanSignals yet - DetectAxes
// returns "" for those until the walk is extended. The v1 detection table
// in scan.go already drives the dashboard's v1 recommendation; this is the
// v2 sibling.
//
// Conflicts: framework conflicts fire when ≥2 of {svelte, astro, go} fire.
// Platform conflicts: only one platform signal (wrangler.toml) exists today;
// future signals (vercel.json, netlify.toml) will surface conflicts here.
func DetectAxes(s scanSignals) AxisRecommendation {
	rec := AxisRecommendation{CandidatesByAxis: map[string][]string{}}

	var fwHigh []string
	if s.SvelteConfig || s.SvelteCount > 0 {
		fwHigh = append(fwHigh, "svelte")
	}
	if s.AstroConfig || s.AstroCount > 0 {
		fwHigh = append(fwHigh, "astro")
	}
	if s.GoMod {
		fwHigh = append(fwHigh, "go")
	}
	rec.CandidatesByAxis["framework"] = append([]string{}, fwHigh...)

	switch {
	case len(fwHigh) == 1:
		rec.Framework = fwHigh[0]
		rec.FrameworkConfidence = ConfidenceHigh
	case len(fwHigh) > 1:
		// Conflict - let the caller pick. Leave Framework empty.
		rec.Conflicts = append(rec.Conflicts, "framework")
	case s.HTMLCount > 0:
		rec.Framework = "html"
		rec.FrameworkConfidence = ConfidenceFallback
	}

	var pfHigh []string
	if s.WranglerToml {
		pfHigh = append(pfHigh, "cloudflare")
	}
	rec.CandidatesByAxis["platform"] = append([]string{}, pfHigh...)

	switch {
	case len(pfHigh) == 1:
		rec.Platform = pfHigh[0]
		rec.PlatformConfidence = ConfidenceHigh
	case len(pfHigh) > 1:
		rec.Conflicts = append(rec.Conflicts, "platform")
	case s.HTMLCount > 0:
		rec.Platform = "static-host"
		rec.PlatformConfidence = ConfidenceFallback
	}

	return rec
}
