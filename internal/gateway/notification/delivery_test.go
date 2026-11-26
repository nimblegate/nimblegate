// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nimblegate/internal/gateway/upstream"
)

// faultyUpstream wraps the stub and injects errors into chosen calls.
type faultyUpstream struct {
	*upstream.Stub
	peopleErr error
	scanErr   error
}

func (f *faultyUpstream) ReadPRPeople(ctx context.Context, pr *upstream.PullRequest) (upstream.PRPeople, error) {
	if f.peopleErr != nil {
		return upstream.PRPeople{}, f.peopleErr
	}
	return f.Stub.ReadPRPeople(ctx, pr)
}

func (f *faultyUpstream) ScanForMarker(ctx context.Context, pr *upstream.PullRequest, marker string) (*upstream.Comment, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	return f.Stub.ScanForMarker(ctx, pr, marker)
}

func faultyOrchestrator(t *testing.T, f *faultyUpstream) *Orchestrator {
	t.Helper()
	reg := upstream.NewRegistry()
	reg.Register("faulty", f)
	reg.RegisterHost("upstream.test", "faulty")
	_, o := buildOrchestrator(upstream.NewStub(), "unused.test", t.TempDir())
	o.Upstreams = reg
	return o
}

func countingServer(t *testing.T, status int, n *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(n, 1)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Concurrent appends (the pre-receive hook) racing rewrites (the daemon) must
// not lose records: each rewrite reads, filters and renames under the lock.
func TestQueue_ConcurrentAppendsSurviveRewrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pr-comment-queue.jsonl")
	if err := AppendQueueRecord(path, QueueRecord{ID: "seed"}); err != nil {
		t.Fatal(err)
	}
	const n = 200
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = AppendQueueRecord(path, QueueRecord{ID: fmt.Sprintf("a%d", i)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			_ = UpdateQueueRecord(path, QueueRecord{ID: "seed", DeliveryAttempts: i})
		}
	}()
	wg.Wait()
	recs, err := ReadQueueRecords(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != n+1 {
		t.Fatalf("lost updates: want %d records, got %d", n+1, len(recs))
	}
}

// A failing PR-comment side must not swallow the webhook.
func TestDeliverOne_WebhookFiresWhenCommentFails(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("repo", "refs/heads/main", &upstream.PullRequest{Number: 7})
	o := faultyOrchestrator(t, &faultyUpstream{Stub: stub, peopleErr: fmt.Errorf("%w: HTTP 403", upstream.ErrPermanent)})
	var hits int32
	rec := baseRec("evt_403", "repo", "https://upstream.test/repo", "refs/heads/main")
	rec.WebhookURL = countingServer(t, 200, &hits).URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}

	err := o.DeliverOne(context.Background(), rec)
	if !errors.Is(err, upstream.ErrPermanent) {
		t.Fatalf("comment error must be returned, got %v", err)
	}
	if hits != 1 {
		t.Fatalf("webhook must fire despite the comment failure, got %d POSTs", hits)
	}
}

// A failed sticky lookup is transient, not "no sticky": creating a comment
// then would leave a duplicate on the PR.
func TestDeliverOne_StickyLookupErrorDoesNotCreate(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("repo", "refs/heads/main", &upstream.PullRequest{Number: 7})
	o := faultyOrchestrator(t, &faultyUpstream{Stub: stub, scanErr: fmt.Errorf("%w: HTTP 500", upstream.ErrTransient)})
	rec := baseRec("evt_scan", "repo", "https://upstream.test/repo", "refs/heads/main")

	err := o.DeliverOne(context.Background(), rec)
	if !errors.Is(err, upstream.ErrTransient) {
		t.Fatalf("want transient error, got %v", err)
	}
	if got := stub.Comments(7); len(got) != 0 {
		t.Fatalf("no comment may be created after a failed lookup, got %d", len(got))
	}
}

// include-pr-assignees = false: nobody is auto-tagged.
func TestDeliverOne_OmitPRPeople(t *testing.T) {
	stub := upstream.NewStub()
	stub.AddPR("repo", "refs/heads/main", &upstream.PullRequest{Number: 7})
	stub.SetPeople(7, upstream.PRPeople{Assignees: []string{"alice"}, Reviewers: []string{"bob"}})
	var got Notification
	_, o := buildOrchestrator(stub, "upstream.test", t.TempDir())
	o.Render = func(n Notification) string { got = n; return "BODY" }
	rec := baseRec("evt_omit", "repo", "https://upstream.test/repo", "refs/heads/main")
	rec.OmitPRPeople = true

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if got.Mention == nil || len(got.Mention.AutoTaggedHumans) != 0 {
		t.Fatalf("no humans may be tagged, got %+v", got.Mention)
	}
}

// The webhook comes from the current repo config, not the URL frozen on the
// record at push time.
func TestDeliverOne_WebhookResolvedAtDeliveryTime(t *testing.T) {
	var oldHits, newHits int32
	_, o := buildOrchestrator(upstream.NewStub(), "upstream.test", t.TempDir())
	o.Webhooks = map[string]WebhookTarget{"repo": {URL: countingServer(t, 200, &newHits).URL, Auth: WebhookAuth{Mode: "none"}}}
	rec := baseRec("evt_hook", "repo", "https://upstream.test/repo", "refs/heads/main")
	rec.WebhookURL = countingServer(t, 200, &oldHits).URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}

	if err := o.DeliverOne(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if newHits != 1 || oldHits != 0 {
		t.Fatalf("want delivery to the configured URL only: new=%d old=%d", newHits, oldHits)
	}
}

// A permanent error deadletters at once, and the deadletter holds the record
// as last attempted (attempt count + error), not the stale queue copy.
func TestDaemon_PermanentErrorDeadlettersImmediately(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	dlPath := filepath.Join(policyRoot, "demo", "pr-comment-deadletter.jsonl")
	var hits int32
	rec := makeQueueRec("evt_404", now.Add(-time.Minute))
	rec.WebhookURL = countingServer(t, http.StatusNotFound, &hits).URL
	rec.WebhookAuth = WebhookAuth{Mode: "none"}
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatal(err)
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q, _ := ReadQueueRecords(queuePath); len(q) != 0 {
		t.Fatalf("permanent failure must leave the queue, %d left", len(q))
	}
	dl, _ := ReadQueueRecords(dlPath)
	if len(dl) != 1 || dl[0].DeliveryAttempts != 1 || dl[0].LastError == "" {
		t.Fatalf("deadletter must hold the updated record, got %+v", dl)
	}
}

// Per-repo [notification.delivery] overrides the daemon defaults.
func TestDaemon_PerRecordDeliveryPolicy(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	dlPath := filepath.Join(policyRoot, "demo", "pr-comment-deadletter.jsonl")
	var hits int32
	url := countingServer(t, http.StatusServiceUnavailable, &hits).URL
	first := makeQueueRec("evt_first", now.Add(-time.Minute))
	first.WebhookURL, first.WebhookAuth = url, WebhookAuth{Mode: "none"}
	first.Delivery = DeliveryPolicy{MaxAttempts: 2, Backoff: []time.Duration{10 * time.Second}}
	last := first
	last.ID, last.Notification.EventID = "evt_last", "evt_last"
	last.DeliveryAttempts = 1
	for _, r := range []QueueRecord{first, last} {
		if err := AppendQueueRecord(queuePath, r); err != nil {
			t.Fatal(err)
		}
	}

	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	q, _ := ReadQueueRecords(queuePath)
	if len(q) != 1 || q[0].ID != "evt_first" || !q[0].NextRetryAt.Equal(now.Add(10*time.Second)) {
		t.Fatalf("first failure should back off by the repo schedule (10s), got %+v", q)
	}
	if dl, _ := ReadQueueRecords(dlPath); len(dl) != 1 || dl[0].ID != "evt_last" {
		t.Fatalf("second failure should hit the repo cap of 2, deadletter = %+v", dl)
	}
}

// A successful drain records the delivery for the feed.
func TestDaemon_DeliveredRecordsOutcome(t *testing.T) {
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	d, _, policyRoot := buildDaemonHarness(t, "demo", now)
	queuePath := filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl")
	if err := AppendQueueRecord(queuePath, makeQueueRec("evt_ok", now.Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	if err := d.PollOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	o := ReadOutcomes(filepath.Join(policyRoot, "demo", OutcomesFile))
	if o["evt_ok"].Outcome != OutcomeDelivered {
		t.Fatalf("want evt_ok recorded delivered, got %+v", o)
	}
}
