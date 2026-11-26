// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/gateway/notification"
)

func TestCorrelateNotificationStatus(t *testing.T) {
	dir := t.TempDir()
	queued := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

	if err := notification.AppendQueueRecord(filepath.Join(dir, "pr-comment-queue.jsonl"),
		notification.QueueRecord{ID: "evt-queued"}); err != nil {
		t.Fatal(err)
	}
	if err := notification.AppendQueueRecord(filepath.Join(dir, "pr-comment-deadletter.jsonl"),
		notification.QueueRecord{ID: "evt-dead"}); err != nil {
		t.Fatal(err)
	}
	outcomes := filepath.Join(dir, notification.OutcomesFile)
	if err := notification.AppendOutcome(outcomes, "evt-delivered", notification.OutcomeDelivered); err != nil {
		t.Fatal(err)
	}
	if err := notification.AppendOutcome(outcomes, "evt-dropped", notification.OutcomeDropped); err != nil {
		t.Fatal(err)
	}

	recs := []AuditRecord{
		{Time: queued, Notification: &NotificationStatus{EventID: "evt-queued", QueuedAt: queued}},
		{Time: queued, Notification: &NotificationStatus{EventID: "evt-dead", QueuedAt: queued}},
		{Time: queued, Notification: &NotificationStatus{EventID: "evt-delivered", QueuedAt: queued}},
		{Time: queued, Notification: &NotificationStatus{EventID: "evt-dropped", QueuedAt: queued}},
		{Time: queued, Notification: &NotificationStatus{EventID: "evt-gone", QueuedAt: queued}},
		{Time: queued, Notification: nil}, // no notification → untouched
	}
	CorrelateNotificationStatus(dir, recs)

	if recs[0].Notification.Deadlettered || !recs[0].Notification.DeliveredAt.IsZero() || recs[0].Notification.Unknown {
		t.Errorf("evt-queued should remain queued, got %+v", recs[0].Notification)
	}
	if !recs[1].Notification.Deadlettered {
		t.Error("evt-dead should be marked deadlettered")
	}
	if recs[2].Notification.DeliveredAt.IsZero() {
		t.Error("evt-delivered (recorded delivered) should be marked delivered")
	}
	if n := recs[3].Notification; !n.Dropped || !n.DeliveredAt.IsZero() {
		t.Errorf("evt-dropped should be marked dropped, not delivered: %+v", n)
	}
	if n := recs[4].Notification; !n.Unknown || !n.DeliveredAt.IsZero() {
		t.Errorf("evt-gone (no recorded outcome) must be unknown, not delivered: %+v", n)
	}
	if v := notifStatusView(recs[4].Notification); v.Indicator != "unknown" {
		t.Errorf("feed pill for an unrecorded outcome = %q, want unknown", v.Indicator)
	}
	if recs[5].Notification != nil {
		t.Error("a record without a notification must be left untouched")
	}
}
