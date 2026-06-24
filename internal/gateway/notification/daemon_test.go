// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// buildDaemonHarness wires a Daemon pointed at a fresh policy root containing
// one repo dir, an orchestrator backed by a stub upstream, and a "now" hook
// the tests advance manually. The webhook URL is configurable per test: empty
// = no webhook (deliveries succeed); failing-server URL = deliveries fail.
func buildDaemonHarness(t *testing.T, repo string, fixedNow time.Time) (*Daemon, *upstream.Stub, string) {
	t.Helper()
	policyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(policyRoot, repo), 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}

	stub := upstream.NewStub()
	reg := upstream.NewRegistry()
	reg.Register("stub", stub)
	reg.RegisterHost("upstream.test", "stub")
	orch := &Orchestrator{
		Upstreams:  reg,
		Webhook:    webhook.NewClient(),
		Render:     func(n Notification) string { return "BODY: " + n.EventID },
		PolicyRoot: policyRoot,
	}
	d := &Daemon{
		PolicyRoot:   policyRoot,
		Orchestrator: orch,
		Config:       DefaultDaemonConfig(),
		Now:          func() time.Time { return fixedNow },
	}
	return d, stub, policyRoot
}

func makeQueueRec(id string, queuedAt time.Time) QueueRecord {
	return QueueRecord{
		ID:           id,
		QueuedAt:     queuedAt,
		UpstreamKind: "stub",
		Notification: Notification{
			SchemaVersion: SchemaVersion,
			EventID:       id,
			Event:         "push.rejected",
			Repo:          RepoInfo{Name: "demo", UpstreamURL: "https://upstream.test/demo"},
			Push:          PushInfo{Refs: []RefInfo{{Name: "refs/heads/main"}}},
		},
	}
}

func TestDaemon_PollOnce_DrainsOlderRecord(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")

	// queuedAt = 31s ago (older than 30s race gap → eligible).
	rec := makeQueueRec("evt_old", now.Add(-31*time.Second))
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 0 {
		t.Errorf("expected queue drained (orchestrator no-PR + no-webhook returns nil), got %d records: %+v", len(got), got)
	}
}

func TestDaemon_PollOnce_SkipsYoungerRecord(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")

	// queuedAt = 5s ago (younger than 30s → daemon must skip).
	rec := makeQueueRec("evt_young", now.Add(-5*time.Second))
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 1 {
		t.Errorf("expected record retained (younger than race gap), got %d records", len(got))
	}
}

func TestDaemon_PollOnce_RespectsNextRetryAt(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")

	rec := makeQueueRec("evt_retry", now.Add(-time.Hour)) // old enough
	rec.NextRetryAt = now.Add(time.Minute)                // but not yet retry-eligible
	rec.DeliveryAttempts = 1
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 1 {
		t.Fatalf("expected record retained, got %d records", len(got))
	}
	if got[0].DeliveryAttempts != 1 {
		t.Errorf("DeliveryAttempts should be unchanged at 1, got %d", got[0].DeliveryAttempts)
	}
}

func TestDaemon_PollOnce_DeliveryError_IncrementsAndBackoff(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")

	// Webhook server that always returns 503 → upstream.ErrTransient.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rec := makeQueueRec("evt_fail", now.Add(-time.Minute))
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 1 {
		t.Fatalf("expected record retained on failure, got %d records", len(got))
	}
	if got[0].DeliveryAttempts != 1 {
		t.Errorf("DeliveryAttempts = %d, want 1", got[0].DeliveryAttempts)
	}
	wantRetry := now.Add(time.Minute) // first-attempt backoff = 1m
	if !got[0].NextRetryAt.Equal(wantRetry) {
		t.Errorf("NextRetryAt = %v, want %v", got[0].NextRetryAt, wantRetry)
	}
	if got[0].LastError == "" {
		t.Errorf("LastError should be populated after a failed delivery")
	}
}

func TestDaemon_PollOnce_MovesToDeadletterAfterMaxAttempts(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	deadletterPath := filepath.Join(policyRoot, "demo", "pr-comment-deadletter.jsonl")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rec := makeQueueRec("evt_doomed", now.Add(-time.Hour))
	rec.WebhookURL = srv.URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}
	rec.DeliveryAttempts = 19 // one more failure → moves to deadletter
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	queueRecs, _ := ReadQueueRecords(queuePath)
	if len(queueRecs) != 0 {
		t.Errorf("queue should be empty after deadletter move, got %d records", len(queueRecs))
	}
	dlRecs, _ := ReadQueueRecords(deadletterPath)
	if len(dlRecs) != 1 || dlRecs[0].ID != "evt_doomed" {
		t.Errorf("deadletter should hold evt_doomed, got %+v", dlRecs)
	}
}

func TestDaemon_PollOnce_Idempotent(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")

	rec := makeQueueRec("evt_idem", now.Add(-time.Minute))
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce 1: %v", err)
	}
	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce 2: %v", err)
	}

	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 0 {
		t.Errorf("after two polls queue should still be empty, got %d records", len(got))
	}
}

func TestDaemon_PollOnce_MissingPolicyRoot_NoError(t *testing.T) {
	d := &Daemon{
		PolicyRoot:   filepath.Join(t.TempDir(), "absent"),
		Orchestrator: &Orchestrator{},
		Config:       DefaultDaemonConfig(),
		Now:          func() time.Time { return time.Now().UTC() },
	}
	if err := d.PollOnce(context.Background()); err != nil {
		t.Errorf("missing policy root should be a no-op, got %v", err)
	}
}

func TestComputeBackoff_Schedule(t *testing.T) {
	sched := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour}
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Minute},
		{2, 5 * time.Minute},
		{3, 30 * time.Minute},
		{4, 2 * time.Hour},
		{5, 2 * time.Hour},  // past end → repeats last
		{99, 2 * time.Hour}, // far past → still repeats last
	}
	for _, c := range cases {
		got := computeBackoff(c.attempt, sched)
		if got != c.want {
			t.Errorf("computeBackoff(%d) = %v, want %v", c.attempt, got, c.want)
		}
	}
}

func TestComputeBackoff_ZeroAndEmpty(t *testing.T) {
	if got := computeBackoff(0, []time.Duration{time.Minute}); got != time.Minute {
		t.Errorf("attempt 0 should default to 1m, got %v", got)
	}
	if got := computeBackoff(1, nil); got != time.Minute {
		t.Errorf("empty schedule should default to 1m, got %v", got)
	}
}
