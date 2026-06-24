// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/auditanalyze"
	"nimblegate/internal/config"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// auditAnalyzeJSON is the structured shape emitted by `audit analyze --json`.
// Kept distinct from internal types so the JSON contract is independent of
// internal package churn.
type auditAnalyzeJSON struct {
	Window struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Days  int    `json:"days"`
	} `json:"window"`
	EntriesAnalyzed     int                 `json:"entries_analyzed"`
	TotalHoursPrevented float64             `json:"total_hours_prevented"`
	HoursByTier         map[string]float64  `json:"hours_prevented_by_tier"`
	FramesHitByTier     map[string]int      `json:"frames_hit_by_tier"`
	TopBypassed         []bypassedFrameJSON `json:"top_bypassed"`
	StaleFrames         []staleFrameJSON    `json:"stale_frames"`
	AllFrames           []frameStatJSON     `json:"all_frames,omitempty"`
}

type bypassedFrameJSON struct {
	FrameID     string        `json:"frame_id"`
	BypassCount int           `json:"bypass_count"`
	Reasons     []string      `json:"reasons,omitempty"`
	Hotspots    []hotspotJSON `json:"hotspots,omitempty"`
	HoursPerHit float64       `json:"hours_per_hit"`
	HoursSource string        `json:"hours_source"`
}

type hotspotJSON struct {
	Token string `json:"token"`
	Count int    `json:"count"`
}

type staleFrameJSON struct {
	FrameID string `json:"frame_id"`
	Tier    int    `json:"tier"`
}

type frameStatJSON struct {
	FrameID        string  `json:"frame_id"`
	Tier           int     `json:"tier"`
	BlockCount     int     `json:"block_count"`
	WarnCount      int     `json:"warn_count"`
	InfoCount      int     `json:"info_count"`
	PassCount      int     `json:"pass_count"`
	BypassCount    int     `json:"bypass_count"`
	HoursPerHit    float64 `json:"hours_per_hit"`
	HoursSource    string  `json:"hours_source"`
	HoursPrevented float64 `json:"hours_prevented"`
}

// auditAnalyze implements `nimblegate audit analyze`.
func auditAnalyze(args []string) int {
	fs := flag.NewFlagSet("audit analyze", flag.ExitOnError)
	windowFlag := fs.String("window", "30d", "lookback window (e.g. 7d, 24h, 30d)")
	frameFlag := fs.String("frame", "", "focus on a single frame ID")
	minBypassFlag := fs.Int("min-bypass", 2, "minimum bypass count to surface as top-bypassed")
	asJSON := fs.Bool("json", false, "emit JSON for scripting / future UI")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	dur, err := parseSinceDuration(*windowFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit analyze: invalid --window %q: %v\n", *windowFlag, err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit analyze: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	stdlibFrames, _ := stdlib.Load()
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))

	frameByID := map[string]frames.Frontmatter{}
	for _, f := range stdlibFrames {
		frameByID[f.ID()] = f.Frontmatter
	}
	for _, f := range projectFrames {
		// Project frame shadows stdlib at the same ID.
		frameByID[f.ID()] = f.Frontmatter
	}

	cfg, _ := config.LoadProject(paths.ConfigPath(root))
	enabled := cfg.Frames.Enabled

	// Opportunistic compaction before analyzing - keeps audit.parts/ tidy
	// and gives a consolidated read view. Best-effort.
	_, _ = engine.CompactAudit(root, engine.DefaultCompactQuiescence)

	logPath := paths.AuditLogPath(root)
	logs := engine.RotatedFiles(logPath)
	entries, err := auditanalyze.ReadEntries(logs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit analyze: read audit log: %v\n", err)
		return 2
	}

	report := auditanalyze.Analyze(entries, time.Now().UTC(), dur, enabled, frameByID, cfg, auditanalyze.DefaultConfig())

	if *frameFlag != "" {
		return renderSingleFrame(os.Stdout, report, *frameFlag, *asJSON)
	}

	if *asJSON {
		return renderAnalyzeJSON(os.Stdout, report, *minBypassFlag)
	}
	return renderAnalyzeHuman(os.Stdout, report, *minBypassFlag)
}

func renderAnalyzeHuman(w io.Writer, r auditanalyze.Report, minBypass int) int {
	fmt.Fprintf(w, "nimblegate audit analyze: last %dd (%s → %s)\n",
		r.WindowDays,
		r.WindowStart.Format("2006-01-02"),
		r.WindowEnd.Format("2006-01-02"))
	fmt.Fprintf(w, "  entries analyzed: %d\n\n", r.EntriesAnalyzed)

	// Time-prevented summary.
	fmt.Fprintln(w, "Estimated time prevented (window):")
	fmt.Fprintf(w, "  Total: %s\n", formatHours(r.TotalHoursPrevented))
	if r.TotalHoursPrevented > 0 {
		tiers := make([]int, 0, len(r.HoursPreventedByTier))
		for t := range r.HoursPreventedByTier {
			tiers = append(tiers, t)
		}
		sort.Ints(tiers)
		for _, t := range tiers {
			h := r.HoursPreventedByTier[t]
			if h == 0 {
				continue
			}
			fmt.Fprintf(w, "    Tier %d (%d frames hit): %s\n", t, r.FramesHitByTier[t], formatHours(h))
		}
		fmt.Fprintln(w, "  (each frame's per-hit estimate comes from its frontmatter, the project's")
		fmt.Fprintln(w, "   [time-estimates] section, or the built-in tier default, `nimblegate info <id>`)")
	}
	fmt.Fprintln(w)

	// Top-bypassed.
	top := r.TopBypassed(minBypass)
	if len(top) == 0 {
		fmt.Fprintf(w, "Top bypassed frames (>= %d bypasses): none\n\n", minBypass)
	} else {
		fmt.Fprintf(w, "Top bypassed frames (>= %d bypasses):\n", minBypass)
		for _, st := range top {
			fmt.Fprintf(w, "  %-44s  %d×  reasons: %s\n",
				frames.SanitizeForOutput(st.FrameID),
				st.BypassCount,
				formatHotspots(st.ReasonHotspots),
			)
			suggestion := bypassSuggestion(st)
			if suggestion != "" {
				fmt.Fprintf(w, "    → %s\n", suggestion)
			}
		}
		fmt.Fprintln(w)
	}

	// Stale frames.
	if len(r.StaleFrames) > 0 {
		fmt.Fprintf(w, "Stale frames (enabled, zero hits in %dd):\n", r.WindowDays)
		for _, s := range r.StaleFrames {
			fmt.Fprintf(w, "  %s  (tier %d)\n", frames.SanitizeForOutput(s.FrameID), s.Tier)
		}
		fmt.Fprintf(w, "  → caveat: audit log only reflects this window; older history may show different signal.\n")
		fmt.Fprintln(w)
	}

	return 0
}

func renderAnalyzeJSON(w io.Writer, r auditanalyze.Report, minBypass int) int {
	out := auditAnalyzeJSON{
		EntriesAnalyzed:     r.EntriesAnalyzed,
		TotalHoursPrevented: r.TotalHoursPrevented,
		HoursByTier:         map[string]float64{},
		FramesHitByTier:     map[string]int{},
	}
	out.Window.Start = r.WindowStart.Format(time.RFC3339)
	out.Window.End = r.WindowEnd.Format(time.RFC3339)
	out.Window.Days = r.WindowDays
	for t, h := range r.HoursPreventedByTier {
		out.HoursByTier[fmt.Sprintf("tier-%d", t)] = h
	}
	for t, c := range r.FramesHitByTier {
		out.FramesHitByTier[fmt.Sprintf("tier-%d", t)] = c
	}
	for _, st := range r.TopBypassed(minBypass) {
		var hs []hotspotJSON
		for _, h := range st.ReasonHotspots {
			hs = append(hs, hotspotJSON{Token: h.Token, Count: h.Count})
		}
		out.TopBypassed = append(out.TopBypassed, bypassedFrameJSON{
			FrameID:     st.FrameID,
			BypassCount: st.BypassCount,
			Reasons:     st.BypassReasons,
			Hotspots:    hs,
			HoursPerHit: st.HoursPerHit,
			HoursSource: string(st.HoursSource),
		})
	}
	for _, s := range r.StaleFrames {
		out.StaleFrames = append(out.StaleFrames, staleFrameJSON{
			FrameID: s.FrameID, Tier: s.Tier,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit analyze: json encode: %v\n", err)
		return 2
	}
	return 0
}

func renderSingleFrame(w io.Writer, r auditanalyze.Report, frameID string, asJSON bool) int {
	st, ok := r.FrameStats[frameID]
	if !ok {
		fmt.Fprintf(os.Stderr, "nimblegate audit analyze: frame %q has no audit-log activity in the window\n", frameID)
		return 1
	}
	if asJSON {
		out := frameStatJSON{
			FrameID:        st.FrameID,
			BlockCount:     st.BlockCount,
			WarnCount:      st.WarnCount,
			InfoCount:      st.InfoCount,
			PassCount:      st.PassCount,
			BypassCount:    st.BypassCount,
			HoursPerHit:    st.HoursPerHit,
			HoursSource:    string(st.HoursSource),
			HoursPrevented: st.HoursPrevented,
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return 0
	}
	fmt.Fprintf(w, "Frame: %s\n", frames.SanitizeForOutput(st.FrameID))
	fmt.Fprintf(w, "  Window: last %dd\n", r.WindowDays)
	fmt.Fprintf(w, "  BLOCK: %d   WARN: %d   INFO: %d   PASS: %d   BYPASS: %d\n",
		st.BlockCount, st.WarnCount, st.InfoCount, st.PassCount, st.BypassCount)
	fmt.Fprintf(w, "  Hours per hit: %s  (source: %s)\n", formatHours(st.HoursPerHit), st.HoursSource)
	fmt.Fprintf(w, "  Hours prevented: %s  (BLOCK+WARN+INFO × hours/hit)\n", formatHours(st.HoursPrevented))
	if len(st.BypassReasons) > 0 {
		fmt.Fprintln(w, "  Bypass reasons:")
		for _, r := range st.BypassReasons {
			fmt.Fprintf(w, "    - %s\n", frames.SanitizeForOutput(r))
		}
	}
	if len(st.ReasonHotspots) > 0 {
		fmt.Fprintf(w, "  Reason hotspots: %s\n", formatHotspots(st.ReasonHotspots))
	}
	return 0
}

// bypassSuggestion returns an actionable suggestion string when one is
// obvious from the hotspot pattern, else "". Mechanical heuristics - match
// tokens against a known list of suggestion triggers.
func bypassSuggestion(st *auditanalyze.FrameStat) string {
	if len(st.ReasonHotspots) == 0 {
		return ""
	}
	topToken := st.ReasonHotspots[0].Token
	switch topToken {
	case "vendor", "vendored", "third-party":
		return "consider: whitelist entry path=\"vendor/**\" reason=\"vendored deps\""
	case "fixture", "fixtures", "test", "tests":
		return "consider: whitelist entry path=\"**/test/**\" pattern=\"<context>\" reason=\"test fixtures\""
	case "ci", "pipeline", "build":
		return "consider: whitelist entry path=\"ci/**\" or scope-narrow the frame's applies-to.files"
	case "generated":
		return "consider: whitelist entry path=\"<generated-dir>/**\" reason=\"machine-generated\""
	}
	return fmt.Sprintf("inspect: %d bypasses cluster on %q, consider a scoped whitelist or frame revision", st.ReasonHotspots[0].Count, topToken)
}

func formatHotspots(hs []auditanalyze.HotspotToken) string {
	if len(hs) == 0 {
		return "(none clustered)"
	}
	var parts []string
	for _, h := range hs {
		parts = append(parts, fmt.Sprintf("%s(%d)", h.Token, h.Count))
	}
	return strings.Join(parts, " ")
}

// formatHours renders a float hour count in the way users read it best:
// "0.5h", "2.0h", "12.5h". Sub-tenth precision is dropped (it's an estimate
// - false precision misleads).
func formatHours(h float64) string {
	if h == 0 {
		return "0h"
	}
	return fmt.Sprintf("%.1fh", h)
}
