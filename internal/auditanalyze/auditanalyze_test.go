// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package auditanalyze

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/frames"
)

func writeFixtureLog(t *testing.T, lines []string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.log")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// jsonEntry builds a JSONL line in the on-disk audit-log shape.
func jsonEntry(ts time.Time, trigger, frame, result string, override bool, reason string) string {
	return fmt.Sprintf(`{"ts":%q,"trigger":%q,"frame":%q,"result":%q,"override":%t,"reason":%q}`,
		ts.UTC().Format(time.RFC3339Nano), trigger, frame, result, override, reason)
}

func TestReadEntries_SkipsWhitelistSuppression(t *testing.T) {
	ts := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	p := writeFixtureLog(t, []string{
		jsonEntry(ts, "cli", "security/x", "BLOCK", false, "leak"),
		`{"ts":"2026-05-18T12:00:01Z","kind":"whitelist-suppression","trigger":"cli","frame":"security/x","file":"vendor/foo.js","label":"AKIA..."}`,
		jsonEntry(ts.Add(time.Second), "cli", "security/y", "PASS", false, ""),
	})
	entries, err := ReadEntries([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("got %d entries; want 2 (suppression line should be skipped)", len(entries))
	}
}

func TestAnalyze_TopBypassed(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	hour := time.Hour
	entries := []Entry{
		// security/no-hardcoded-credentials bypassed 3 times.
		{Timestamp: now.Add(-1 * hour), Override: true, Frame: "security/no-hardcoded-credentials", Reason: "test fixture, not a real key"},
		{Timestamp: now.Add(-2 * hour), Override: true, Frame: "security/no-hardcoded-credentials", Reason: "vendor library has test key"},
		{Timestamp: now.Add(-3 * hour), Override: true, Frame: "security/no-hardcoded-credentials", Reason: "ci fixture vendor"},
		// git-safety/no-force-push-main bypassed once - below threshold.
		{Timestamp: now.Add(-4 * hour), Override: true, Frame: "git-safety/no-force-push-main", Reason: "release"},
	}
	frameByID := map[string]frames.Frontmatter{
		"security/no-hardcoded-credentials": {Category: "security", Name: "no-hardcoded-credentials", Tier: 1},
		"git-safety/no-force-push-main":     {Category: "git-safety", Name: "no-force-push-main", Tier: 1},
	}
	r := Analyze(entries, now, 24*time.Hour, nil, frameByID, config.ProjectConfig{}, DefaultConfig())

	top := r.TopBypassed(2)
	if len(top) != 1 {
		t.Fatalf("got %d frames at >= 2 bypasses; want 1", len(top))
	}
	if top[0].FrameID != "security/no-hardcoded-credentials" {
		t.Errorf("top frame = %q; want security/no-hardcoded-credentials", top[0].FrameID)
	}
	if top[0].BypassCount != 3 {
		t.Errorf("bypass count = %d; want 3", top[0].BypassCount)
	}
	// Hotspots: "vendor" appears in 2 reasons, "fixture" in 2, "test" in 2.
	if len(top[0].ReasonHotspots) == 0 {
		t.Errorf("expected reason hotspots; got none")
	}
	found := map[string]bool{}
	for _, h := range top[0].ReasonHotspots {
		found[h.Token] = true
	}
	if !found["vendor"] {
		t.Errorf("expected 'vendor' in hotspots; got %v", top[0].ReasonHotspots)
	}
}

func TestAnalyze_StaleFrames(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: now.Add(-1 * time.Hour), Frame: "security/active-frame", Result: "PASS"},
	}
	frameByID := map[string]frames.Frontmatter{
		"security/active-frame": {Category: "security", Name: "active-frame", Tier: 2},
		"security/quiet-frame":  {Category: "security", Name: "quiet-frame", Tier: 2},
		"convention/dead":       {Category: "convention", Name: "dead", Tier: 6},
	}
	enabled := []string{"security/active-frame", "security/quiet-frame", "convention/dead", "git-safety/*"}
	r := Analyze(entries, now, 7*24*time.Hour, enabled, frameByID, config.ProjectConfig{}, DefaultConfig())

	if len(r.StaleFrames) != 2 {
		t.Errorf("stale count = %d; want 2 (quiet-frame + dead). got %+v", len(r.StaleFrames), r.StaleFrames)
	}
	// Wildcards are skipped.
	for _, s := range r.StaleFrames {
		if strings.HasSuffix(s.FrameID, "/*") {
			t.Errorf("wildcard %q surfaced as stale", s.FrameID)
		}
	}
	// Sorted by tier ascending - so tier-2 comes before tier-6.
	if r.StaleFrames[0].FrameID != "security/quiet-frame" {
		t.Errorf("first stale = %q; want security/quiet-frame (tier 2 before tier 6)", r.StaleFrames[0].FrameID)
	}
}

func TestAnalyze_HoursPrevented_TierDefault(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		// Tier 1 frame fires 3× → 3 × 4h = 12h
		{Timestamp: now.Add(-1 * time.Hour), Frame: "security/tier-1-frame", Result: "BLOCK"},
		{Timestamp: now.Add(-2 * time.Hour), Frame: "security/tier-1-frame", Result: "BLOCK"},
		{Timestamp: now.Add(-3 * time.Hour), Frame: "security/tier-1-frame", Result: "BLOCK"},
		// Tier 6 fires once → 1 × 0.1h = 0.1h
		{Timestamp: now.Add(-1 * time.Hour), Frame: "convention/tier-6-frame", Result: "WARN"},
		// PASS shouldn't count toward prevented hours.
		{Timestamp: now.Add(-1 * time.Hour), Frame: "security/tier-1-frame", Result: "PASS"},
	}
	frameByID := map[string]frames.Frontmatter{
		"security/tier-1-frame":   {Category: "security", Name: "tier-1-frame", Tier: 1},
		"convention/tier-6-frame": {Category: "convention", Name: "tier-6-frame", Tier: 6},
	}
	r := Analyze(entries, now, 24*time.Hour, nil, frameByID, config.ProjectConfig{}, DefaultConfig())

	if got, want := r.TotalHoursPrevented, 12.1; got != want {
		t.Errorf("total hours = %v; want %v", got, want)
	}
	if got := r.HoursPreventedByTier[1]; got != 12.0 {
		t.Errorf("tier 1 hours = %v; want 12.0", got)
	}
	if got := r.HoursPreventedByTier[6]; got != 0.1 {
		t.Errorf("tier 6 hours = %v; want 0.1", got)
	}
}

func TestAnalyze_HoursPrevented_ProjectOverride(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: now.Add(-1 * time.Hour), Frame: "security/x", Result: "BLOCK"},
		{Timestamp: now.Add(-2 * time.Hour), Frame: "security/x", Result: "BLOCK"},
	}
	frameByID := map[string]frames.Frontmatter{
		"security/x": {Category: "security", Name: "x", Tier: 1},
	}
	v := 8.0
	cfg := config.ProjectConfig{TimeEstimates: config.TimeEstimates{Tier1: &v}}
	r := Analyze(entries, now, 24*time.Hour, nil, frameByID, cfg, DefaultConfig())

	if got := r.TotalHoursPrevented; got != 16.0 {
		t.Errorf("total hours = %v; want 16.0 (2 BLOCKs × 8h project override)", got)
	}
	if r.FrameStats["security/x"].HoursSource != frames.TimeFromConfig {
		t.Errorf("source = %q; want project-tier", r.FrameStats["security/x"].HoursSource)
	}
}

func TestAnalyze_HoursPrevented_FrameOverride(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: now.Add(-1 * time.Hour), Frame: "security/x", Result: "BLOCK"},
	}
	frameByID := map[string]frames.Frontmatter{
		"security/x": {Category: "security", Name: "x", Tier: 1, TimeCostHoursPrevented: 12.5},
	}
	r := Analyze(entries, now, 24*time.Hour, nil, frameByID, config.ProjectConfig{}, DefaultConfig())

	if got := r.TotalHoursPrevented; got != 12.5 {
		t.Errorf("total hours = %v; want 12.5 (1 BLOCK × 12.5h frame override)", got)
	}
	if r.FrameStats["security/x"].HoursSource != frames.TimeFromFrame {
		t.Errorf("source = %q; want frame", r.FrameStats["security/x"].HoursSource)
	}
}

func TestAnalyze_IgnoresOutOfWindowEntries(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	entries := []Entry{
		// In window.
		{Timestamp: now.Add(-1 * time.Hour), Frame: "f", Result: "BLOCK"},
		// Out of window (30d ago, but window is 1d).
		{Timestamp: now.Add(-30 * 24 * time.Hour), Frame: "f", Result: "BLOCK"},
	}
	frameByID := map[string]frames.Frontmatter{"f": {Tier: 1}}
	r := Analyze(entries, now, 24*time.Hour, nil, frameByID, config.ProjectConfig{}, DefaultConfig())
	if r.EntriesAnalyzed != 1 {
		t.Errorf("entries analyzed = %d; want 1", r.EntriesAnalyzed)
	}
}

func TestTokenize(t *testing.T) {
	// "and", "with", "the" are stopwords. "test" is intentionally NOT a
	// stopword - it's a domain term that often clusters across bypasses.
	// "foo" is below the minLen=4 threshold.
	got := tokenize("vendor/foo and ci-fixtures with the test", 4)
	want := map[string]bool{"vendor": true, "ci-fixtures": true, "test": true}
	if len(got) != 3 {
		t.Errorf("got %d tokens; want 3 (vendor, ci-fixtures, test): %v", len(got), got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected token %q", g)
		}
	}
}

func TestTopTokens_OneCountPerReason(t *testing.T) {
	reasons := []string{
		"vendor vendor vendor", // would count as 3 if naive
		"vendor library",
	}
	got := topTokens(reasons, 4, 2, 10)
	if len(got) != 1 {
		t.Fatalf("got %d tokens; want 1", len(got))
	}
	if got[0].Token != "vendor" || got[0].Count != 2 {
		t.Errorf("token = %q count = %d; want vendor 2 (de-duped per reason)", got[0].Token, got[0].Count)
	}
}
