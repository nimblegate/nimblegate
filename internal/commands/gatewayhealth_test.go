// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
)

// seedHealthFixture builds two repos under policyRoot:
//   - repoA: registered, two queue records, one deadletter record, one
//     delivered audit notification (within last 24h)
//   - repoB: registered, no queue, no deadletter, no notifications
//
// Returns the time the delivered notification was stamped so the test can
// assert relative-time rendering.
func seedHealthFixture(t *testing.T, policyRoot string) time.Time {
	t.Helper()

	mustMkdir := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	mustWrite := func(p, body string) {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	// repoA registered (gateway.toml present), with seeded queue/deadletter/audit.
	repoA := filepath.Join(policyRoot, "repoA")
	mustMkdir(repoA)
	mustWrite(filepath.Join(repoA, "gateway.toml"), `upstream-url = "https://example.test/repoA.git"`+"\n")

	if err := notification.AppendQueueRecord(filepath.Join(repoA, "pr-comment-queue.jsonl"),
		notification.QueueRecord{ID: "q1", QueuedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append queue rec 1: %v", err)
	}
	if err := notification.AppendQueueRecord(filepath.Join(repoA, "pr-comment-queue.jsonl"),
		notification.QueueRecord{ID: "q2", QueuedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append queue rec 2: %v", err)
	}
	if err := notification.AppendQueueRecord(filepath.Join(repoA, "pr-comment-deadletter.jsonl"),
		notification.QueueRecord{ID: "dl1", QueuedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("append dl rec: %v", err)
	}

	deliveredAt := time.Now().UTC().Add(-15 * time.Minute)
	if err := gateway.AppendAudit(filepath.Join(repoA, "audit.log"), gateway.AuditRecord{
		Time:   deliveredAt,
		Repo:   "repoA",
		Refs:   []string{"refs/heads/main"},
		Accept: false,
		Notification: &gateway.NotificationStatus{
			EventID:          "evt-1",
			QueuedAt:         deliveredAt,
			InlineAttempted:  true,
			InlineSucceeded:  true,
			DeliveredAt:      deliveredAt,
			DeliveryAttempts: 1,
		},
	}); err != nil {
		t.Fatalf("append audit: %v", err)
	}

	// repoB registered but empty - no queue, no audit.
	repoB := filepath.Join(policyRoot, "repoB")
	mustMkdir(repoB)
	mustWrite(filepath.Join(repoB, "gateway.toml"), `upstream-url = "https://example.test/repoB.git"`+"\n")

	return deliveredAt
}

func TestCollectHealth_perRepoCounters(t *testing.T) {
	root := t.TempDir()
	seedHealthFixture(t, root)
	d := collectHealth(root, "", time.Now().Add(-5*time.Minute), time.Now())

	if len(d.Repos) != 2 {
		t.Fatalf("want 2 repos, got %d (%+v)", len(d.Repos), d.Repos)
	}
	// Sorted alphabetically by listGatewayRepos contract → repoA first.
	if d.Repos[0].Name != "repoA" || d.Repos[1].Name != "repoB" {
		t.Errorf("unexpected repo order: %+v", d.Repos)
	}
	if d.Repos[0].QueueDepth != 2 {
		t.Errorf("repoA queue depth = %d, want 2", d.Repos[0].QueueDepth)
	}
	if d.Repos[0].DeadletterCount != 1 {
		t.Errorf("repoA deadletter = %d, want 1", d.Repos[0].DeadletterCount)
	}
	if d.Repos[1].QueueDepth != 0 || d.Repos[1].DeadletterCount != 0 {
		t.Errorf("repoB should be empty, got queue=%d dl=%d", d.Repos[1].QueueDepth, d.Repos[1].DeadletterCount)
	}
	if d.Repos[0].LastDrainAgo == "-" {
		t.Errorf("repoA should have a non-empty last drain (audit delivered): got %q", d.Repos[0].LastDrainAgo)
	}
	if d.Repos[1].LastDrainAgo != "-" {
		t.Errorf("repoB should have last-drain=-, got %q", d.Repos[1].LastDrainAgo)
	}
}

func TestCollectHealth_successRates24h(t *testing.T) {
	root := t.TempDir()
	seedHealthFixture(t, root)
	d := collectHealth(root, "", time.Now().Add(-time.Minute), time.Now())

	if !d.HasActivity {
		t.Fatalf("expected activity flag (1 notification attempted), got false")
	}
	// One attempted notification, one succeeded → 100% on both sides.
	if d.WebhookSuccess != 100 {
		t.Errorf("webhook success = %d, want 100", d.WebhookSuccess)
	}
	if d.CommentSuccess != 100 {
		t.Errorf("comment success = %d, want 100", d.CommentSuccess)
	}
}

func TestCollectHealth_emptyPolicyRoot(t *testing.T) {
	root := t.TempDir() // no gateway.toml entries
	d := collectHealth(root, "", time.Now().Add(-1*time.Hour), time.Now())
	if len(d.Repos) != 0 {
		t.Errorf("empty root should yield no repos, got %d", len(d.Repos))
	}
	if d.HasActivity {
		t.Errorf("empty root should have no activity flag")
	}
	if d.LastPollAgo != "no successful drain yet" {
		t.Errorf("LastPollAgo = %q, want the no-drain sentinel", d.LastPollAgo)
	}
	// PID + Uptime should still be populated - these come from the running
	// process regardless of policy-root content.
	if d.PID <= 0 {
		t.Errorf("PID should be the process PID, got %d", d.PID)
	}
	if d.Uptime == "" {
		t.Errorf("Uptime should render even on empty root, got empty")
	}
}

func TestHealthHandler_rendersExpectedSurfaces(t *testing.T) {
	root := t.TempDir()
	seedHealthFixture(t, root)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	healthHandler(root, "", "").ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		// Per-repo line surfaces - these are the operator's primary signal.
		"Repo: repoA", "Queue: 2", "Deadletter: 1",
		"Repo: repoB", "Queue: 0",
		// The Investigate button only appears when deadletter > 0.
		`hx-post="/health/investigate?repo=repoA"`,
		// Daemon + disk status lines.
		"Dashboard service", "PID ", "Daemon loop", "Disk free",
		// 24h delivery success surfaces.
		"Webhook delivery success", "PR comment success",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("health page missing %q\n%s", want, body)
		}
	}
}

// A failing persisted relay status renders a visible failing line on /health.
func TestHealth_failingRelayRenders(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repoR")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "gateway.toml"),
		[]byte(`upstream-url = "https://example.test/repoR.git"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gateway.WriteRelayStatus(root, "repoR", gateway.RelayStatus{OK: false, Error: "relay to upstream failed"}); err != nil {
		t.Fatal(err)
	}

	d := collectHealth(root, "", time.Now().Add(-time.Minute), time.Now())
	var buf bytes.Buffer
	if err := renderHealth(&buf, d); err != nil {
		t.Fatalf("renderHealth: %v", err)
	}
	if body := buf.String(); !strings.Contains(body, "relay failing: relay to upstream failed") {
		t.Errorf("health page missing failing-relay line\n%s", body)
	}
}

func TestHealthHandler_wrongPath404s(t *testing.T) {
	req := httptest.NewRequest("GET", "/health/nope", nil)
	rec := httptest.NewRecorder()
	healthHandler(t.TempDir(), "", "").ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Errorf("wrong path should 404, got %d", rec.Code)
	}
}

// TestCollectHealth_skeletonSummaryWhenReposRootSet confirms the skeleton
// aggregator runs when reposRoot is provided and the totals reflect a
// mixed clean/issue fleet. Empty-reposRoot suppression is covered by
// every other test in this file (they all pass "" and never see the line).
func TestCollectHealth_skeletonSummary(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// Two registered repos: one fully wired (SSH upstream), one with the
	// seeded appframes.toml removed (degraded issue).
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "clean", UpstreamURL: "git@gitea.internal:you/clean.git",
		Enabled: true, PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "broken", UpstreamURL: "git@gitea.internal:you/broken.git",
		Enabled: true, PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(policyRoot, "broken", "appframes.toml")); err != nil {
		t.Fatal(err)
	}

	d := collectHealth(policyRoot, reposRoot, time.Now().Add(-time.Minute), time.Now())

	if !d.SkeletonChecked {
		t.Fatalf("SkeletonChecked should be true when reposRoot is set")
	}
	if d.SkeletonReposTotal != 2 {
		t.Errorf("SkeletonReposTotal = %d, want 2", d.SkeletonReposTotal)
	}
	if d.SkeletonReposIssues != 1 {
		t.Errorf("SkeletonReposIssues = %d, want 1", d.SkeletonReposIssues)
	}
	if d.SkeletonIssuesTotal != 1 {
		t.Errorf("SkeletonIssuesTotal = %d, want 1", d.SkeletonIssuesTotal)
	}
	if d.SkeletonBlocking != 0 {
		t.Errorf("SkeletonBlocking = %d, want 0 (the issue is degraded, not blocking)", d.SkeletonBlocking)
	}
}

func TestCollectHealth_skeletonSummarySuppressedWithoutReposRoot(t *testing.T) {
	root := t.TempDir()
	d := collectHealth(root, "", time.Now().Add(-time.Minute), time.Now())
	if d.SkeletonChecked {
		t.Errorf("empty reposRoot → SkeletonChecked should be false; got true")
	}
}

func TestFormatUptime_buckets(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{2*time.Hour + 30*time.Minute, "2h 30m"},
		{3*24*time.Hour + 5*time.Hour, "3d 5h"},
	}
	for _, c := range cases {
		if got := formatUptime(c.in); got != c.want {
			t.Errorf("formatUptime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatBytes_buckets(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{512, "512 B"},
		{1500, "2 KB"},
		{2_000_000, "2 MB"},
		{3_500_000_000, "3.5 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
