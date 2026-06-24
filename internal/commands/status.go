// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"nimblegate/internal/auditanalyze"
	"nimblegate/internal/config"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/incident"
	"nimblegate/internal/paths"
	"nimblegate/internal/state"
	"nimblegate/internal/stdlib"
)

type auditLine struct {
	Timestamp string `json:"ts"`
	Trigger   string `json:"trigger"`
	Frame     string `json:"frame"`
	Result    string `json:"result"`
	Override  bool   `json:"override"`
	Reason    string `json:"reason"`
}

type frameStat struct {
	frame    string
	pass     int
	warn     int
	info     int
	block    int
	err      int
	override int
	lastTS   string
}

// statusFilter narrows which audit entries summarizeAuditLog counts.
// A zero-value filter matches everything.
type statusFilter struct {
	// trigger restricts entries to a single trigger value (cli, pre-commit,
	// git-wrap, etc.). Empty = no restriction.
	trigger string

	// since restricts entries to those with a timestamp >= since.
	// Zero time = no restriction.
	since time.Time
}

func (f statusFilter) match(entry auditLine) bool {
	if f.trigger != "" && entry.Trigger != f.trigger {
		return false
	}
	if !f.since.IsZero() {
		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err == nil && t.Before(f.since) {
			return false
		}
	}
	return true
}

// lifecycleCounts is a small histogram of frames by lifecycle, used by
// Status to surface a one-line "loaded vs gated" summary.
type lifecycleCounts struct {
	Active     int
	Candidate  int
	Proposed   int
	Deprecated int
	Archived   int
}

// computeLifecycleCounts walks stdlib + project frames and tallies them
// by effective lifecycle. Used by `nimblegate status` for the summary
// line introduced 2026-05-20 with Phase 1 Slice 4.
func computeLifecycleCounts() lifecycleCounts {
	var c lifecycleCounts
	stdFrames, err := stdlib.Load()
	if err == nil {
		for _, f := range stdFrames {
			tally(&c, f.Frontmatter.EffectiveLifecycle())
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		if root, err := paths.FindProjectRoot(cwd); err == nil {
			projFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
			for _, f := range projFrames {
				tally(&c, f.Frontmatter.EffectiveLifecycle())
			}
		}
	}
	return c
}

func tally(c *lifecycleCounts, lc frames.Lifecycle) {
	switch lc {
	case frames.LifecycleActive:
		c.Active++
	case frames.LifecycleCandidate:
		c.Candidate++
	case frames.LifecycleProposed:
		c.Proposed++
	case frames.LifecycleDeprecated:
		c.Deprecated++
	case frames.LifecycleArchived:
		c.Archived++
	}
}

func (c lifecycleCounts) total() int {
	return c.Active + c.Candidate + c.Proposed + c.Deprecated + c.Archived
}

func (c lifecycleCounts) gated() int {
	return c.Active + c.Candidate
}

func Status(args []string) int {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	triggerFlag := fs.String("trigger", "", "filter to one trigger (cli, pre-commit, git-wrap, watcher, server)")
	sinceFlag := fs.String("since", "", "only count entries newer than this duration ago (e.g. 7d, 24h, 30m)")
	_ = fs.Parse(args)

	// Pause banner before everything else - paused state changes how the
	// rest of the output should be read (frame counts still load, but
	// nothing is firing). Resolve project root opportunistically; missing
	// root just means we only show global pause state.
	emitPauseBanner(os.Stdout)

	// Phase 1 Slice 4: one-line lifecycle summary at the top so the user
	// can see at a glance "X frames loaded, Y gating, Z in history."
	lc := computeLifecycleCounts()
	if lc.total() > 0 {
		fmt.Printf("Frames: %d loaded: %d gating (active %d + candidate %d)",
			lc.total(), lc.gated(), lc.Active, lc.Candidate)
		if lc.Proposed+lc.Deprecated+lc.Archived > 0 {
			fmt.Printf("; %d non-gating (proposed %d, deprecated %d, archived %d)",
				lc.Proposed+lc.Deprecated+lc.Archived, lc.Proposed, lc.Deprecated, lc.Archived)
		}
		fmt.Println()
	}

	filter := statusFilter{trigger: *triggerFlag}
	if *sinceFlag != "" {
		dur, err := parseSinceDuration(*sinceFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate status: invalid --since %q: %v\n", *sinceFlag, err)
			return 2
		}
		filter.since = time.Now().Add(-dur)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate status: %v\n", err)
		return 2
	}
	logPath := paths.AuditLogPath(root)
	// Opportunistic compaction: merge quiescent per-process part files
	// into audit.log before reading. Best-effort - errors are swallowed
	// so a compaction hiccup doesn't break `status`. Uses the default
	// quiescence window.
	_, _ = engine.CompactAudit(root, engine.DefaultCompactQuiescence)

	// Check whether ANY audit-log file exists on disk (consolidated +
	// active part files). RotatedFiles returns logPath in the list even
	// when missing, so we have to filter.
	hasAny := false
	for _, p := range engine.RotatedFiles(logPath) {
		if _, err := os.Stat(p); err == nil {
			hasAny = true
			break
		}
	}
	if !hasAny {
		fmt.Println("(no audit log yet; run `nimblegate check` or commit something first)")
		return 0
	}
	// Read across all rotated log files AND active part files (RotatedFiles
	// returns both, chronologically) so the summary captures everything.
	_, err = summarizeAuditLogs(engine.RotatedFiles(logPath), os.Stdout, filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate status: %v\n", err)
		return 2
	}
	// Nudge for uncaptured bypasses in the last 7 days. Mechanical: bypass
	// count minus bypass-sourced incident count, both within the window.
	// Window is fixed (not tied to --since) so the nudge doesn't disappear
	// when the user narrows the filter to one trigger.
	emitIncidentNudge(os.Stdout, root, engine.RotatedFiles(logPath), 7*24*time.Hour, time.Now())

	// One-line teaser for estimated time prevented. Silent when 0h so
	// fresh projects don't see a useless "0h prevented" line.
	emitTimePreventedTeaser(os.Stdout, root, engine.RotatedFiles(logPath), 7*24*time.Hour, time.Now())
	return 0
}

// emitTimePreventedTeaser prints a single line: "Estimated time prevented
// (last 7d): X.Xh". Silent when the figure is 0h (e.g. a project that
// just initialized has nothing to brag about yet).
//
// Uses the same data path as `audit analyze`, but only computes the
// aggregate - no top-bypassed / hotspots / stale-frame rendering here.
func emitTimePreventedTeaser(w io.Writer, projectRoot string, logPaths []string, window time.Duration, now time.Time) {
	entries, err := auditanalyze.ReadEntries(logPaths)
	if err != nil || len(entries) == 0 {
		return
	}
	stdlibFrames, _ := stdlib.Load()
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(projectRoot))
	frameByID := map[string]frames.Frontmatter{}
	for _, f := range stdlibFrames {
		frameByID[f.ID()] = f.Frontmatter
	}
	for _, f := range projectFrames {
		frameByID[f.ID()] = f.Frontmatter
	}
	cfg, _ := config.LoadProject(paths.ConfigPath(projectRoot))
	r := auditanalyze.Analyze(entries, now, window, cfg.Frames.Enabled, frameByID, cfg, auditanalyze.DefaultConfig())
	if r.TotalHoursPrevented <= 0 {
		return
	}
	days := int(window / (24 * time.Hour))
	fmt.Fprintf(w, "\nEstimated time prevented (last %dd): %.1fh  (run `nimblegate audit analyze` for breakdown)\n",
		days, r.TotalHoursPrevented)
}

// emitIncidentNudge prints a one-line reminder when there are bypass audit
// entries in the lookback window that don't appear to have been captured
// as incidents.
//
// "Captured" is matched mechanically: a bypass incident's frontmatter has
// source=bypass and a date within the window. We do NOT match individual
// entries to individual incident files - too brittle once a user starts
// editing titles. The check is "did N+ bypasses produce ≥1 incident in the
// same window?" which catches the common failure mode (zero incidents
// despite repeated bypasses).
func emitIncidentNudge(w io.Writer, projectRoot string, logPaths []string, window time.Duration, now time.Time) {
	cutoff := now.Add(-window)
	bypassCount := 0
	for _, p := range logPaths {
		bypassCount += countBypassesSince(p, cutoff)
	}
	if bypassCount == 0 {
		return
	}
	incDir := filepath.Join(paths.AppframesDir(projectRoot), incident.IncidentsDirName)
	incs, _ := incident.LoadFromDir(incDir)
	captured := 0
	for _, inc := range incs {
		if inc.Frontmatter.Source != incident.SourceBypass {
			continue
		}
		d, err := time.Parse("2006-01-02", inc.Frontmatter.Date)
		if err != nil {
			continue
		}
		if d.Before(cutoff) {
			continue
		}
		captured++
	}
	uncaptured := bypassCount - captured
	if uncaptured <= 0 {
		return
	}
	days := int(window / (24 * time.Hour))
	fmt.Fprintf(w, "\n⚠  %d bypass(es) in last %dd not yet captured as incidents\n", uncaptured, days)
	fmt.Fprintf(w, "   capture with: `nimblegate incident new --title \"...\" --from-frame <id> --from-reason \"...\"`\n")
}

// countBypassesSince returns the number of audit-log lines with override=true
// (i.e. --force-yes records) whose timestamp is >= cutoff. Lines that fail to
// parse are skipped, matching the rest of the status reader.
func countBypassesSince(logPath string, cutoff time.Time) int {
	f, err := os.Open(logPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var entry auditLine
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue
		}
		if !entry.Override {
			continue
		}
		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			continue
		}
		count++
	}
	return count
}

// emitPauseBanner prints a prominent warning when nimblegate is currently
// paused (global or project scope). Silent when not paused so the banner
// doesn't add noise to the normal case.
func emitPauseBanner(w io.Writer) {
	store, err := state.NewStore()
	if err != nil {
		return
	}
	var root string
	if cwd, err := os.Getwd(); err == nil {
		if r, err := paths.FindProjectRoot(cwd); err == nil {
			root = r
		}
	}
	st, err := store.IsPaused(root)
	if err != nil {
		fmt.Fprintf(w, "⚠  nimblegate pause state unreadable: %v\n", err)
		// fall through - st still reflects whatever was successfully read
	}
	if !st.AnyPaused() {
		return
	}
	if st.GlobalPaused {
		line := fmt.Sprintf("⚠  NIMBLEGATE PAUSED (global): since %s",
			st.GlobalPausedAt.Local().Format("2006-01-02 15:04"))
		if st.GlobalReason != "" {
			line += fmt.Sprintf(", reason: %s", st.GlobalReason)
		}
		fmt.Fprintln(w, line)
		fmt.Fprintln(w, "   resume with: nimblegate resume --global")
	}
	if st.ProjectPaused {
		line := fmt.Sprintf("⚠  NIMBLEGATE PAUSED (project): since %s",
			st.ProjectPausedAt.Local().Format("2006-01-02 15:04"))
		if st.ProjectReason != "" {
			line += fmt.Sprintf(", reason: %s", st.ProjectReason)
		}
		fmt.Fprintln(w, line)
		fmt.Fprintln(w, "   resume with: nimblegate resume --project")
	}
	fmt.Fprintln(w)
}

// parseSinceDuration extends time.ParseDuration with "d" (days). Returns
// the parsed Duration or an error.
func parseSinceDuration(s string) (time.Duration, error) {
	if len(s) > 1 && s[len(s)-1] == 'd' {
		days, err := time.ParseDuration(s[:len(s)-1] + "h")
		if err != nil {
			return 0, fmt.Errorf("expected NUMd (e.g. 7d), got %q", s)
		}
		return days * 24, nil
	}
	return time.ParseDuration(s)
}

// summarizeAuditLogs is the multi-file form of summarizeAuditLog. It
// aggregates per-frame stats across every path in order, applying the same
// filter. Single-file callers use summarizeAuditLog directly (tests).
func summarizeAuditLogs(paths []string, w io.Writer, filter statusFilter) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}
	if len(paths) == 1 {
		return summarizeAuditLog(paths[0], w, filter)
	}
	stats := map[string]*frameStat{}
	overrides := []auditLine{}
	rows := 0
	for _, p := range paths {
		n, err := accumulateFromFile(p, filter, stats, &overrides)
		if err != nil {
			return rows, err
		}
		rows += n
	}
	renderStats(w, stats, overrides)
	return rows, nil
}

// summarizeAuditLog reads logPath and writes a per-frame summary to w.
// Returns the number of audit rows processed (after filter).
func summarizeAuditLog(logPath string, w io.Writer, filter statusFilter) (int, error) {
	stats := map[string]*frameStat{}
	overrides := []auditLine{}
	rows, err := accumulateFromFile(logPath, filter, stats, &overrides)
	if err != nil {
		return rows, err
	}
	renderStats(w, stats, overrides)
	return rows, nil
}

// accumulateFromFile scans one audit-log file line by line, applies the
// filter, and updates the running stats + overrides slice in place. Returns
// rows processed.
func accumulateFromFile(logPath string, filter statusFilter, stats map[string]*frameStat, overrides *[]auditLine) (int, error) {
	f, err := os.Open(logPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	rows := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var entry auditLine
		if err := json.Unmarshal(sc.Bytes(), &entry); err != nil {
			continue
		}
		if !filter.match(entry) {
			continue
		}
		rows++
		st, ok := stats[entry.Frame]
		if !ok {
			st = &frameStat{frame: entry.Frame}
			stats[entry.Frame] = st
		}
		st.lastTS = entry.Timestamp
		if entry.Override {
			st.override++
			*overrides = append(*overrides, entry)
			continue
		}
		switch entry.Result {
		case "PASS":
			st.pass++
		case "WARN":
			st.warn++
		case "INFO":
			st.info++
		case "BLOCK":
			st.block++
		case "ERROR":
			st.err++
		}
	}
	return rows, sc.Err()
}

// renderStats writes the per-frame table and override list to w.
func renderStats(w io.Writer, stats map[string]*frameStat, overrides []auditLine) {
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	now := time.Now()
	fmt.Fprintln(w, "FRAME                                          PASS WARN INFO BLOCK ERR OVR  LAST")
	for _, k := range keys {
		s := stats[k]
		fmt.Fprintf(w, "%-46s %4d %4d %4d %5d %3d %3d  %s\n",
			frames.SanitizeForOutput(s.frame), s.pass, s.warn, s.info, s.block, s.err, s.override,
			formatRelativeTimestamp(s.lastTS, now))
	}

	if len(overrides) > 0 {
		fmt.Fprintln(w, "\nOVERRIDES (--force-yes invocations):")
		for _, o := range overrides {
			fmt.Fprintf(w, "  %s  %s  %s\n",
				formatRelativeTimestamp(o.Timestamp, now),
				frames.SanitizeForOutput(o.Frame),
				frames.SanitizeForOutput(o.Reason))
		}
	}
}
