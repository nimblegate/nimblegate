// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"path/filepath"
	"testing"
	"time"
)

func TestQueue_AppendThenRead(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "pr-comment-queue.jsonl")

	rec := QueueRecord{
		ID:               "evt_a1",
		QueuedAt:         time.Date(2026, 6, 4, 18, 23, 45, 0, time.UTC),
		UpstreamKind:     "github",
		Notification:     Notification{SchemaVersion: "1.0", EventID: "evt_a1", Event: "push.rejected"},
		DeliveryAttempts: 0,
	}
	if err := AppendQueueRecord(queuePath, rec); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := ReadQueueRecords(queuePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].ID != "evt_a1" {
		t.Errorf("expected 1 record with id evt_a1, got %d records: %+v", len(got), got)
	}
}

func TestQueue_AppendMultipleReadsAll(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.jsonl")
	for i, id := range []string{"a", "b", "c"} {
		if err := AppendQueueRecord(queuePath, QueueRecord{ID: id, QueuedAt: time.Unix(int64(i), 0)}); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}
	got, err := ReadQueueRecords(queuePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "b" || got[2].ID != "c" {
		t.Errorf("order/contents wrong: %+v", got)
	}
}

func TestQueue_ReadEmptyMissingFile(t *testing.T) {
	got, err := ReadQueueRecords(filepath.Join(t.TempDir(), "does-not-exist.jsonl"))
	if err != nil {
		t.Errorf("missing file should return nil, nil - got err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("missing file should return empty, got %+v", got)
	}
}

func TestQueue_RemoveOneLeavesOthers(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.jsonl")
	for _, id := range []string{"a", "b", "c"} {
		_ = AppendQueueRecord(queuePath, QueueRecord{ID: id})
	}
	if err := RemoveQueueRecord(queuePath, "b"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("expected [a, c], got %+v", got)
	}
}

func TestQueue_RemoveMissingIsNoop(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.jsonl")
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "a"})
	if err := RemoveQueueRecord(queuePath, "missing"); err != nil {
		t.Errorf("remove missing should be no-op, got err: %v", err)
	}
	got, _ := ReadQueueRecords(queuePath)
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("expected [a], got %+v", got)
	}
}

func TestQueue_MoveToDeadletter(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.jsonl")
	dlPath := filepath.Join(dir, "deadletter.jsonl")
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "a"})
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "b"})

	if err := MoveToDeadletter(queuePath, dlPath, "a"); err != nil {
		t.Fatalf("move: %v", err)
	}
	queue, _ := ReadQueueRecords(queuePath)
	dl, _ := ReadQueueRecords(dlPath)
	if len(queue) != 1 || queue[0].ID != "b" {
		t.Errorf("queue should have only b, got %+v", queue)
	}
	if len(dl) != 1 || dl[0].ID != "a" {
		t.Errorf("deadletter should have a, got %+v", dl)
	}
}

func TestResetQueueBackoff(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.jsonl")
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "a", DeliveryAttempts: 7, LastError: "HTTP 403", NextRetryAt: time.Unix(9999999999, 0)})
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "b", DeliveryAttempts: 3, LastError: "HTTP 403", NextRetryAt: time.Unix(9999999999, 0)})
	n, err := ResetQueueBackoff(queuePath)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("reset count: got %d want 2", n)
	}
	recs, _ := ReadQueueRecords(queuePath)
	if len(recs) != 2 {
		t.Fatalf("records: got %d want 2", len(recs))
	}
	for _, r := range recs {
		if r.DeliveryAttempts != 0 || r.LastError != "" || !r.NextRetryAt.IsZero() {
			t.Errorf("record %s not reset: %+v", r.ID, r)
		}
	}
}

func TestResetQueueBackoff_emptyIsNoop(t *testing.T) {
	n, err := ResetQueueBackoff(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || n != 0 {
		t.Fatalf("empty: got n=%d err=%v", n, err)
	}
}

func TestRequeueDeadletter(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "q.jsonl")
	dlPath := filepath.Join(dir, "dl.jsonl")
	_ = AppendQueueRecord(queuePath, QueueRecord{ID: "live"})
	_ = AppendQueueRecord(dlPath, QueueRecord{ID: "dead1", DeliveryAttempts: 20, LastError: "HTTP 403"})
	_ = AppendQueueRecord(dlPath, QueueRecord{ID: "dead2", DeliveryAttempts: 20, LastError: "HTTP 403"})
	n, err := RequeueDeadletter(queuePath, dlPath)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("requeued: got %d want 2", n)
	}
	// Deadletter now empty.
	dl, _ := ReadQueueRecords(dlPath)
	if len(dl) != 0 {
		t.Fatalf("deadletter should be empty, got %+v", dl)
	}
	// Queue now has the original + 2 requeued, with cleared backoff.
	q, _ := ReadQueueRecords(queuePath)
	if len(q) != 3 {
		t.Fatalf("queue: got %d want 3", len(q))
	}
	for _, r := range q {
		if r.ID == "dead1" || r.ID == "dead2" {
			if r.DeliveryAttempts != 0 || r.LastError != "" {
				t.Errorf("requeued %s should have cleared backoff: %+v", r.ID, r)
			}
		}
	}
}

// TestRemovePendingRejectsForRef is the #3 stale-reject fix: when a clean push
// resolves a ref, its pending reject records are purged so they can't deliver
// after the resolution and flip the ✅ comment back to ⛔. The resolution record
// and records for other refs survive.
func TestRemovePendingRejectsForRef(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "q.jsonl")
	mk := func(id, event, ref string) QueueRecord {
		return QueueRecord{ID: id, Notification: Notification{Event: event, Push: PushInfo{Refs: []RefInfo{{Name: ref}}}}}
	}
	_ = AppendQueueRecord(q, mk("rej-x", "push.rejected", "refs/heads/x"))
	_ = AppendQueueRecord(q, mk("res-x", "push.resolved", "refs/heads/x"))
	_ = AppendQueueRecord(q, mk("rej-y", "push.rejected", "refs/heads/y"))

	n, err := RemovePendingRejectsForRef(q, "refs/heads/x")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removed: got %d want 1 (only the stale reject on ref x)", n)
	}
	ids := map[string]bool{}
	got, _ := ReadQueueRecords(q)
	for _, r := range got {
		ids[r.ID] = true
	}
	if ids["rej-x"] {
		t.Error("stale reject for the resolved ref x must be removed")
	}
	if !ids["res-x"] {
		t.Error("the resolution record for ref x must be kept")
	}
	if !ids["rej-y"] {
		t.Error("a reject for a different ref (y) must be kept")
	}
}

func TestRemovePendingRejectsForRef_noneIsNoop(t *testing.T) {
	dir := t.TempDir()
	q := filepath.Join(dir, "q.jsonl")
	_ = AppendQueueRecord(q, QueueRecord{ID: "res", Notification: Notification{Event: "push.resolved", Push: PushInfo{Refs: []RefInfo{{Name: "refs/heads/x"}}}}})
	n, err := RemovePendingRejectsForRef(q, "refs/heads/x")
	if err != nil || n != 0 {
		t.Fatalf("no rejects → noop: got n=%d err=%v", n, err)
	}
	if got, _ := ReadQueueRecords(q); len(got) != 1 {
		t.Fatalf("resolution must survive, got %d records", len(got))
	}
}
