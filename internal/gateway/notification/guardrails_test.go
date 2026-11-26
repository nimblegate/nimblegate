// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/upstream"
)

func guardrailRec(id string) QueueRecord {
	rec := baseRec(id, "gw-test", "https://upstream.test/gw-test", "refs/heads/fix-demo")
	rec.LoopConfig = LoopConfig{MaxAttempts: 5, DefaultMention: "@nimblegate-bot"}
	rec.Cooldown = CooldownConfig{ThresholdCount: 3, ThresholdWindow: 5 * time.Minute, Duration: 10 * time.Minute}
	return rec
}

func TestDeliverOne_ExhaustedWithoutFallback_MarksPayload(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("gw-test", "refs/heads/fix-demo", &upstream.PullRequest{Number: 7})
	var got captured
	srv := newWebhookServer(t, http.StatusOK, &got)
	defer srv.Close()

	_, o := buildOrchestrator(stub, "upstream.test", t.TempDir())
	var last Notification
	o.Render = func(n Notification) string { last = n; return "BODY" }
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	o.Now = func() time.Time { return now }

	for i := 1; i <= 6; i++ {
		now = now.Add(time.Hour) // spaced out: never trips the cooldown
		rec := guardrailRec("evt" + string(rune('0'+i)))
		rec.WebhookURL = srv.URL
		if err := o.DeliverOne(context.Background(), rec); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		wantExhausted := i >= 5
		if last.LoopState == nil || last.LoopState.Exhausted != wantExhausted {
			t.Fatalf("attempt %d: LoopState = %+v, want exhausted=%v", i, last.LoopState, wantExhausted)
		}
		var payload Notification
		if err := json.Unmarshal(got.body, &payload); err != nil {
			t.Fatalf("attempt %d: webhook payload: %v", i, err)
		}
		if payload.LoopState == nil || payload.LoopState.Exhausted != wantExhausted {
			t.Errorf("attempt %d: webhook loop_state = %+v, want exhausted=%v", i, payload.LoopState, wantExhausted)
		}
	}
}

func TestDeliverOne_CooldownHoldsDelivery(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("gw-test", "refs/heads/fix-demo", &upstream.PullRequest{Number: 7})
	var got captured
	srv := newWebhookServer(t, http.StatusOK, &got)
	defer srv.Close()

	root := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", root)
	rendered := 0
	o.Render = func(n Notification) string { rendered++; return "BODY" }
	start := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	now := start
	o.Now = func() time.Time { return now }

	deliver := func(id string) error {
		rec := guardrailRec(id)
		rec.WebhookURL = srv.URL
		return o.DeliverOne(context.Background(), rec)
	}
	for i, id := range []string{"a", "b", "c"} {
		now = start.Add(time.Duration(i) * time.Minute)
		if err := deliver(id); err != nil {
			t.Fatalf("reject %s: %v", id, err)
		}
	}
	s, _ := ReadPRState(root, "gw-test", 7)
	wantUntil := start.Add(2*time.Minute + 10*time.Minute)
	if s == nil || !s.Loop.CooldownUntil.Equal(wantUntil) {
		t.Fatalf("CooldownUntil = %+v, want %v", s, wantUntil)
	}

	got.body = nil
	now = start.Add(3 * time.Minute)
	err := deliver("d")
	var cd *CooldownError
	if !errors.As(err, &cd) || !cd.Until.Equal(wantUntil) {
		t.Fatalf("4th reject during cooldown: err = %v, want CooldownError until %v", err, wantUntil)
	}
	if rendered != 3 || got.body != nil {
		t.Errorf("held record was delivered: rendered=%d webhook=%q", rendered, got.body)
	}
	if s2, _ := ReadPRState(root, "gw-test", 7); s2.Loop.AttemptCount != 3 {
		t.Errorf("held record advanced the loop: AttemptCount = %d, want 3", s2.Loop.AttemptCount)
	}

	now = wantUntil.Add(time.Second)
	if err := deliver("d"); err != nil {
		t.Fatalf("after cooldown: %v", err)
	}
	if rendered != 4 || got.body == nil {
		t.Errorf("record not delivered after cooldown: rendered=%d", rendered)
	}
	if s3, _ := ReadPRState(root, "gw-test", 7); !s3.Loop.CooldownUntil.IsZero() || s3.Loop.AttemptCount != 4 {
		t.Errorf("after cooldown: state = %+v, want attempt 4 and no cooldown", s3.Loop)
	}
}

func TestDaemon_CooldownReschedulesWithoutCountingAttempt(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	d, stub, root := buildDaemonHarness(t, "demo", now)
	stub.AddPR("demo", "refs/heads/main", &upstream.PullRequest{Number: 3})
	until := now.Add(7 * time.Minute)
	if err := WritePRState(root, "demo", 3, PRState{
		SchemaVersion: "1.0",
		PRNumber:      3,
		Loop:          LoopCounters{AttemptCount: 3, MaxAttempts: 5, CooldownUntil: until},
	}); err != nil {
		t.Fatal(err)
	}
	rec := makeQueueRec("held", now.Add(-time.Hour))
	rec.LoopConfig = LoopConfig{MaxAttempts: 5, DefaultMention: "@nimblegate-bot"}
	queue := filepath.Join(root, "demo", "pr-comment-queue.jsonl")
	if err := AppendQueueRecord(queue, rec); err != nil {
		t.Fatal(err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	recs, _ := ReadQueueRecords(queue)
	if len(recs) != 1 {
		t.Fatalf("queue = %d records, want the held one kept", len(recs))
	}
	if recs[0].DeliveryAttempts != 0 || !recs[0].NextRetryAt.Equal(until) || recs[0].LastError != "" {
		t.Errorf("held record = attempts %d, next %v, err %q; want 0, %v, none", recs[0].DeliveryAttempts, recs[0].NextRetryAt, recs[0].LastError, until)
	}
	if _, err := os.Stat(filepath.Join(root, "demo", "pr-comment-deadletter.jsonl")); err == nil {
		t.Error("held record reached the deadletter")
	}
}

func TestDeliverOne_WebhookRetryDoesNotAdvanceLoop(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("gw-test", "refs/heads/fix-demo", &upstream.PullRequest{Number: 7})
	status := http.StatusInternalServerError
	var got captured
	srv := newWebhookServer(t, 0, &got)
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
	defer srv.Close()

	root := t.TempDir()
	_, o := buildOrchestrator(stub, "upstream.test", root)
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	o.Now = func() time.Time { return now }

	deliver := func(id string) error {
		rec := guardrailRec(id)
		rec.WebhookURL = srv.URL
		return o.DeliverOne(context.Background(), rec)
	}
	// Three rejects in two minutes trip the cooldown on the third.
	for i, id := range []string{"a", "b"} {
		now = now.Add(time.Duration(i) * time.Minute)
		status = http.StatusOK
		if err := deliver(id); err != nil {
			t.Fatal(err)
		}
	}
	status = http.StatusInternalServerError
	now = now.Add(time.Minute)
	for i := 0; i < 4; i++ {
		var cd *CooldownError
		if err := deliver("c"); err == nil || errors.As(err, &cd) {
			t.Fatalf("retry %d of c: err = %v, want webhook failure (not held by its own cooldown)", i, err)
		}
	}
	s, _ := ReadPRState(root, "gw-test", 7)
	if s.Loop.AttemptCount != 3 || len(s.AttemptHistory) != 3 {
		t.Fatalf("webhook retries advanced the loop: attempts=%d history=%d, want 3/3", s.Loop.AttemptCount, len(s.AttemptHistory))
	}
	status = http.StatusOK
	if err := deliver("c"); err != nil {
		t.Fatalf("final retry of c: %v", err)
	}
	if s, _ := ReadPRState(root, "gw-test", 7); s.Loop.AttemptCount != 3 {
		t.Errorf("successful retry advanced the loop: attempts=%d, want 3", s.Loop.AttemptCount)
	}
}
