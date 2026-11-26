// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package auditanalyze runs retrospective pattern detection over the
// nimblegate audit log. It surfaces three classes of finding:
//
//  1. Top bypassed frames - frames with the most --force-yes overrides in
//     the window. Suggests "is this frame too strict?" / "should we add a
//     whitelist entry?"
//  2. Reason-text hotspots - frequent tokens across bypass reasons. When
//     4/5 bypasses of a single frame all mention "test" or "vendor", the
//     remediation is a scoped whitelist.
//  3. Stale frames - frames enabled in appframes.toml but with zero audit
//     entries in the window. Earned-out or never-applies candidates.
//
// Time-prevented stats are computed alongside: each distinct BLOCK finding
// in the window (counted once however many runs reported it) is worth the
// frame's per-hit time estimate. Bypasses, WARN/INFO results and findings a
// whitelist entry suppressed do NOT count; whitelisted findings are reported
// as their own count instead.
//
// All output is mechanical (counts + multiplications). No prediction, no
// AI judgment. The user can audit every number from the audit log + frame
// frontmatter + project config.
package auditanalyze

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// Entry is the typed in-memory shape of one audit-log line. Matches the
// auditEntry struct in engine/audit.go (we deliberately keep our own type
// to avoid an internal-package import cycle from this analyzer back into
// engine).
type Entry struct {
	Timestamp time.Time
	Trigger   string
	Frame     string
	Result    string
	Target    string
	Override  bool
	Reason    string
	// Findings are the engine.FindingID values of the result's hits; empty
	// for hit-less results and for entries written before the field existed.
	Findings []string
	// Suppressed marks a whitelist-suppression line; File and Label name
	// the suppressed hit and Result carries the severity it would have had.
	Suppressed bool
	File       string
	Label      string
}

// FrameStat aggregates one frame's audit-log presence in the window.
type FrameStat struct {
	FrameID        string
	BlockCount     int
	WarnCount      int
	InfoCount      int
	PassCount      int
	BypassCount    int            // override=true entries
	DistinctBlocks int            // distinct non-whitelisted BLOCK findings
	BypassReasons  []string       // raw reasons (one per bypass)
	ReasonHotspots []HotspotToken // tokens >= MinTokenLen with freq >= MinReasonHotspotHits
	HoursPerHit    float64        // resolved time estimate
	HoursSource    frames.TimeEstimateSource
	HoursPrevented float64 // DistinctBlocks * HoursPerHit
	LastSeen       time.Time
}

// HotspotToken describes one repeated token across a frame's bypass reasons.
type HotspotToken struct {
	Token string
	Count int
}

// StaleFrame describes a frame enabled in config but absent from the window.
type StaleFrame struct {
	FrameID string
	Tier    int
}

// Report is the structured shape returned by Analyze and consumed by the
// CLI / status teaser. Keep it stable - it's the JSON shape.
type Report struct {
	WindowStart     time.Time
	WindowEnd       time.Time
	WindowDays      int
	EntriesAnalyzed int

	// FrameStats indexed by frame ID. Includes ALL frames seen in the
	// window (bypassed or not). Ranking happens at presentation time.
	FrameStats map[string]*FrameStat

	// StaleFrames lists frames enabled in config but absent from the window.
	StaleFrames []StaleFrame

	// Aggregate time-prevented stats.
	TotalHoursPrevented  float64
	HoursPreventedByTier map[int]float64

	// FrameHits maps tier → number of distinct frames in that tier with >=1 counted BLOCK finding.
	FramesHitByTier map[int]int

	// WhitelistedFindings counts distinct BLOCK findings a whitelist entry
	// suppressed in the window; they are excluded from the hours above.
	WhitelistedFindings int
}

// Config bundles the runtime knobs.
type Config struct {
	// MinReasonHotspotHits is the threshold above which a reason-token
	// becomes a hotspot. Default 2 (i.e. token appearing in 2+ reasons).
	MinReasonHotspotHits int

	// MinTokenLen is the minimum token length to count toward hotspots.
	// Default 4. Filters noise like "the", "of", short connectives.
	MinTokenLen int

	// MaxHotspotTokensPerFrame caps the number of tokens surfaced per frame.
	// Default 5.
	MaxHotspotTokensPerFrame int
}

// DefaultConfig returns the recommended knob values for production use.
func DefaultConfig() Config {
	return Config{
		MinReasonHotspotHits:     2,
		MinTokenLen:              4,
		MaxHotspotTokensPerFrame: 5,
	}
}

// ReadEntries parses every line of the audit-log files in logPaths
// (oldest-first when the caller passes the result of engine.RotatedFiles).
// Lines that fail to parse are silently skipped - matches the resilient
// behavior of `nimblegate status`.
func ReadEntries(logPaths []string) ([]Entry, error) {
	var out []Entry
	for _, p := range logPaths {
		f, err := os.Open(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("auditanalyze: open %s: %w", p, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			var raw struct {
				TS       string   `json:"ts"`
				Kind     string   `json:"kind"`
				Trigger  string   `json:"trigger"`
				Frame    string   `json:"frame"`
				Result   string   `json:"result"`
				Target   string   `json:"target"`
				Override bool     `json:"override"`
				Reason   string   `json:"reason"`
				Findings []string `json:"findings"`
				File     string   `json:"file"`
				Label    string   `json:"label"`
				Severity string   `json:"severity"`
			}
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, raw.TS)
			if err != nil {
				continue
			}
			if raw.Kind == "whitelist-suppression" {
				out = append(out, Entry{
					Timestamp:  t,
					Trigger:    raw.Trigger,
					Frame:      raw.Frame,
					Result:     raw.Severity,
					Suppressed: true,
					File:       raw.File,
					Label:      raw.Label,
				})
				continue
			}
			out = append(out, Entry{
				Timestamp: t,
				Trigger:   raw.Trigger,
				Frame:     raw.Frame,
				Result:    raw.Result,
				Target:    raw.Target,
				Override:  raw.Override,
				Reason:    raw.Reason,
				Findings:  raw.Findings,
			})
		}
		_ = f.Close()
	}
	return out, nil
}

// Analyze runs the patterns over entries with the given window. enabledFrames
// is the project's flat list of enabled frame IDs (post-group-expansion);
// it's used to detect stale frames. frameByID maps every loaded frame ID to
// its parsed frontmatter so we can resolve effective time estimates.
//
// projectCfg supplies [time-estimates] tier overrides.
func Analyze(entries []Entry, now time.Time, window time.Duration, enabledFrames []string, frameByID map[string]frames.Frontmatter, projectCfg config.ProjectConfig, opt Config) Report {
	if opt.MinReasonHotspotHits <= 0 {
		opt.MinReasonHotspotHits = DefaultConfig().MinReasonHotspotHits
	}
	if opt.MinTokenLen <= 0 {
		opt.MinTokenLen = DefaultConfig().MinTokenLen
	}
	if opt.MaxHotspotTokensPerFrame <= 0 {
		opt.MaxHotspotTokensPerFrame = DefaultConfig().MaxHotspotTokensPerFrame
	}

	windowStart := now.Add(-window)
	r := Report{
		WindowStart:          windowStart,
		WindowEnd:            now,
		WindowDays:           int(window / (24 * time.Hour)),
		FrameStats:           map[string]*FrameStat{},
		HoursPreventedByTier: map[int]float64{},
		FramesHitByTier:      map[int]int{},
	}

	// Suppressions are written after the raw result they exempt, so collect
	// them first. A legacy suppression line has no severity; count it.
	whitelisted := map[string]bool{}
	for _, e := range entries {
		if e.Suppressed && !e.Timestamp.Before(windowStart) && (e.Result == "" || e.Result == "BLOCK") {
			whitelisted[engine.FindingID(e.Frame, e.File, e.Label)] = true
		}
	}
	r.WhitelistedFindings = len(whitelisted)
	blocked := map[string]map[string]bool{}

	// Per-frame counters from in-window entries.
	for _, e := range entries {
		if e.Timestamp.Before(windowStart) || e.Suppressed {
			continue
		}
		r.EntriesAnalyzed++
		st := r.FrameStats[e.Frame]
		if st == nil {
			st = &FrameStat{FrameID: e.Frame}
			r.FrameStats[e.Frame] = st
		}
		if e.Timestamp.After(st.LastSeen) {
			st.LastSeen = e.Timestamp
		}
		if e.Override {
			st.BypassCount++
			if e.Reason != "" {
				st.BypassReasons = append(st.BypassReasons, e.Reason)
			}
			continue
		}
		switch e.Result {
		case "BLOCK":
			st.BlockCount++
			ids := e.Findings
			if len(ids) == 0 {
				ids = []string{engine.FindingID(e.Frame, "", e.Reason)}
			}
			for _, id := range ids {
				if whitelisted[id] {
					continue
				}
				if blocked[e.Frame] == nil {
					blocked[e.Frame] = map[string]bool{}
				}
				blocked[e.Frame][id] = true
			}
		case "WARN":
			st.WarnCount++
		case "INFO":
			st.InfoCount++
		case "PASS":
			st.PassCount++
		}
	}

	// Resolve per-frame stats. Hotspots are computed for every frame seen
	// in the log (including synthetic IDs like "git-wrap/override" that
	// don't correspond to a loaded frame) since clustered bypass reasons
	// are useful even without frame metadata. Time-prevented requires a
	// loaded frame; orphans skip that part.
	for id, st := range r.FrameStats {
		st.DistinctBlocks = len(blocked[id])
		if len(st.BypassReasons) > 0 {
			st.ReasonHotspots = topTokens(st.BypassReasons, opt.MinTokenLen, opt.MinReasonHotspotHits, opt.MaxHotspotTokensPerFrame)
		}
		fm, ok := frameByID[id]
		if !ok {
			continue
		}
		tierOverride, tierSet := projectCfg.TimeEstimates.Lookup(fm.EffectiveTier())
		hours, src := fm.EffectiveTimeCostHoursPrevented(tierOverride, tierSet)
		st.HoursPerHit = hours
		st.HoursSource = src
		st.HoursPrevented = float64(st.DistinctBlocks) * hours
		r.TotalHoursPrevented += st.HoursPrevented
		r.HoursPreventedByTier[fm.EffectiveTier()] += st.HoursPrevented
		if st.DistinctBlocks > 0 {
			r.FramesHitByTier[fm.EffectiveTier()]++
		}
	}

	// Stale frames: enabled but absent from window.
	for _, id := range enabledFrames {
		if strings.HasSuffix(id, "/*") || strings.HasPrefix(id, "@") {
			continue
		}
		if _, ok := r.FrameStats[id]; ok {
			continue
		}
		tier := 3
		if fm, ok := frameByID[id]; ok {
			tier = fm.EffectiveTier()
		}
		r.StaleFrames = append(r.StaleFrames, StaleFrame{FrameID: id, Tier: tier})
	}
	sort.Slice(r.StaleFrames, func(i, j int) bool {
		if r.StaleFrames[i].Tier != r.StaleFrames[j].Tier {
			return r.StaleFrames[i].Tier < r.StaleFrames[j].Tier
		}
		return r.StaleFrames[i].FrameID < r.StaleFrames[j].FrameID
	})

	return r
}

// TopBypassed returns the frames with bypass counts at or above minBypasses,
// sorted by bypass count descending (ties broken by frame ID asc).
func (r Report) TopBypassed(minBypasses int) []*FrameStat {
	var out []*FrameStat
	for _, s := range r.FrameStats {
		if s.BypassCount >= minBypasses {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BypassCount != out[j].BypassCount {
			return out[i].BypassCount > out[j].BypassCount
		}
		return out[i].FrameID < out[j].FrameID
	})
	return out
}
