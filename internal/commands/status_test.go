// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/incident"
)

const fixtureAuditLog = `{"ts":"2026-05-14T10:00:00Z","trigger":"cli","frame":"git-safety/folder-branch-lock","result":"PASS","override":false}
{"ts":"2026-05-14T10:01:00Z","trigger":"cli","frame":"git-safety/folder-branch-lock","result":"BLOCK","override":false,"reason":"mismatch"}
{"ts":"2026-05-14T10:02:00Z","trigger":"cli","frame":"security/no-innerHTML-user-input","result":"PASS","override":false}
{"ts":"2026-05-14T10:03:00Z","trigger":"git-wrap","frame":"command-safety/apt-purge-preview","result":"BLOCK","override":true,"reason":"--force-yes: cleanup"}
`

func writeAuditLog(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".appframes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "audit.log")
	if err := os.WriteFile(logPath, []byte(fixtureAuditLog), 0o644); err != nil {
		t.Fatal(err)
	}
	return logPath
}

func TestSummarizeAuditLog_CountsByFrameAndOutcome(t *testing.T) {
	logPath := writeAuditLog(t)
	var buf bytes.Buffer
	rows, err := summarizeAuditLog(logPath, &buf, statusFilter{})
	if err != nil {
		t.Fatalf("summarizeAuditLog: %v", err)
	}
	out := buf.String()

	if rows != 4 {
		t.Errorf("rows = %d, want 4", rows)
	}
	if !strings.Contains(out, "git-safety/folder-branch-lock") {
		t.Errorf("missing frame: %s", out)
	}
	if !strings.Contains(out, "OVERRIDES") {
		t.Errorf("missing OVERRIDES section: %s", out)
	}
}

func TestSummarizeAuditLog_FilterByTrigger(t *testing.T) {
	logPath := writeAuditLog(t)
	var buf bytes.Buffer
	rows, err := summarizeAuditLog(logPath, &buf, statusFilter{trigger: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	// 3 of 4 fixture entries have trigger=cli.
	if rows != 3 {
		t.Errorf("rows after trigger=cli filter = %d, want 3", rows)
	}
	if strings.Contains(buf.String(), "command-safety/apt-purge-preview") {
		t.Errorf("git-wrap entry leaked through trigger=cli filter")
	}
}

func TestSummarizeAuditLog_FilterBySince(t *testing.T) {
	logPath := writeAuditLog(t)
	// since cutoff between row 2 (10:01) and row 3 (10:02)
	cutoff, _ := time.Parse(time.RFC3339, "2026-05-14T10:02:00Z")
	var buf bytes.Buffer
	rows, err := summarizeAuditLog(logPath, &buf, statusFilter{since: cutoff})
	if err != nil {
		t.Fatal(err)
	}
	// Rows 3 and 4 should survive.
	if rows != 2 {
		t.Errorf("rows after since filter = %d, want 2", rows)
	}
}

func TestSummarizeAuditLogs_MultiFileAggregation(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".appframes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate one rotation: two-files-worth of audit data.
	current := filepath.Join(dir, "audit.log")
	rotated := filepath.Join(dir, "audit.log.1")
	if err := os.WriteFile(rotated, []byte(`{"ts":"2026-05-10T10:00:00Z","trigger":"cli","frame":"git-safety/folder-branch-lock","result":"PASS","override":false}
{"ts":"2026-05-10T10:01:00Z","trigger":"cli","frame":"git-safety/folder-branch-lock","result":"PASS","override":false}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(current, []byte(`{"ts":"2026-05-14T10:02:00Z","trigger":"cli","frame":"git-safety/folder-branch-lock","result":"BLOCK","override":false}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	rows, err := summarizeAuditLogs([]string{rotated, current}, &buf, statusFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if rows != 3 {
		t.Errorf("rows = %d, want 3 (2 from rotated + 1 from current)", rows)
	}
	out := buf.String()
	if !strings.Contains(out, "git-safety/folder-branch-lock") {
		t.Errorf("missing frame across files; got:\n%s", out)
	}
	// 2 PASS + 1 BLOCK should all attribute to one frame row.
	if !strings.Contains(out, "    2 ") { // PASS column
		t.Errorf("PASS count not aggregated across files; got:\n%s", out)
	}
}

func TestParseSinceDuration(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"24h", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
	}
	for _, tc := range cases {
		got, err := parseSinceDuration(tc.in)
		if err != nil {
			t.Errorf("parseSinceDuration(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseSinceDuration(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if _, err := parseSinceDuration("garbage"); err == nil {
		t.Error("expected error for garbage input")
	}
}

// nudgeTestSetup builds a temp project with an audit log containing N bypass
// entries timestamped at `auditTime` and M bypass-source incidents dated on
// `incidentDate`. Returns the project root.
func nudgeTestSetup(t *testing.T, bypasses int, incidentDates []time.Time, auditTime time.Time) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".appframes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "appframes.toml"), []byte("[frames]\nenabled = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, ".appframes", "audit.log")
	var lines []string
	for i := 0; i < bypasses; i++ {
		ts := auditTime.Add(time.Duration(i) * time.Minute).Format(time.RFC3339Nano)
		lines = append(lines, fmt.Sprintf(`{"ts":%q,"trigger":"git-wrap","frame":"git-wrap/override","result":"INFO","override":true,"reason":"--force-yes: test"}`, ts))
	}
	_ = os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644)

	incDir := filepath.Join(root, ".appframes", "_incidents")
	_ = os.MkdirAll(incDir, 0o755)
	for i, d := range incidentDates {
		inc := incident.NewDraft(incident.NewDraftOptions{
			Title:  fmt.Sprintf("bypass capture %d", i),
			Date:   d,
			Source: incident.SourceBypass,
		})
		inc.SourcePath = filepath.Join(incDir, incident.Filename(d, fmt.Sprintf("bypass-capture-%d", i)))
		data, _ := inc.Marshal()
		_ = os.WriteFile(inc.SourcePath, data, 0o644)
	}
	return root
}

func TestEmitIncidentNudge_FiresWhenUncaptured(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	root := nudgeTestSetup(t, 3, nil, now.Add(-24*time.Hour))
	var buf bytes.Buffer
	emitIncidentNudge(&buf, root, []string{filepath.Join(root, ".appframes", "audit.log")}, 7*24*time.Hour, now)
	out := buf.String()
	if !strings.Contains(out, "3 bypass") {
		t.Errorf("nudge missing bypass count: %s", out)
	}
	if !strings.Contains(out, "not yet captured") {
		t.Errorf("nudge missing 'not yet captured': %s", out)
	}
}

func TestEmitIncidentNudge_SilentWhenAllCaptured(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	d := now.Add(-24 * time.Hour)
	root := nudgeTestSetup(t, 2, []time.Time{d, d}, now.Add(-12*time.Hour))
	var buf bytes.Buffer
	emitIncidentNudge(&buf, root, []string{filepath.Join(root, ".appframes", "audit.log")}, 7*24*time.Hour, now)
	if buf.Len() != 0 {
		t.Errorf("expected silent nudge when bypasses == captured; got: %s", buf.String())
	}
}

func TestEmitIncidentNudge_IgnoresOldBypasses(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	// Bypasses 30 days ago - outside the 7d window.
	root := nudgeTestSetup(t, 5, nil, now.Add(-30*24*time.Hour))
	var buf bytes.Buffer
	emitIncidentNudge(&buf, root, []string{filepath.Join(root, ".appframes", "audit.log")}, 7*24*time.Hour, now)
	if buf.Len() != 0 {
		t.Errorf("expected silent nudge for bypasses outside window; got: %s", buf.String())
	}
}

func TestEmitIncidentNudge_PartialCapture(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	d := now.Add(-24 * time.Hour)
	// 4 bypasses, 1 captured incident - should nudge for the remaining 3.
	root := nudgeTestSetup(t, 4, []time.Time{d}, now.Add(-24*time.Hour))
	var buf bytes.Buffer
	emitIncidentNudge(&buf, root, []string{filepath.Join(root, ".appframes", "audit.log")}, 7*24*time.Hour, now)
	out := buf.String()
	if !strings.Contains(out, "3 bypass") {
		t.Errorf("expected '3 bypass' (4 bypass - 1 captured); got: %s", out)
	}
}
