// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendAudit(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	rec := AuditRecord{Repo: "demo", Accept: false, Refs: []string{"refs/heads/main"}, Messages: []string{"BLOCK security/x"}}
	if err := AppendAudit(p, rec); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	if err := AppendAudit(p, AuditRecord{Repo: "demo", Accept: true}); err != nil {
		t.Fatalf("AppendAudit 2: %v", err)
	}
	b, _ := os.ReadFile(p)
	lines := strings.Count(strings.TrimSpace(string(b)), "\n") + 1
	if lines != 2 {
		t.Errorf("want 2 jsonl lines, got %d:\n%s", lines, b)
	}
	if !strings.Contains(string(b), "security/x") {
		t.Error("audit should record the decision messages")
	}
}

func TestAppendAudit_findingsRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "audit.log")
	rec := AuditRecord{
		Repo:   "demo",
		Accept: true,
		Refs:   []string{"refs/heads/main"},
		Findings: []Finding{
			{ID: "app-correctness/no-owner-todos", Severity: "WARN", Message: "TODO without owner"},
			{ID: "security/x", Severity: "BLOCK", Message: "boom"},
		},
	}
	if err := AppendAudit(p, rec); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	b, _ := os.ReadFile(p)
	var got AuditRecord
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("want 2 findings round-tripped, got %d: %+v", len(got.Findings), got.Findings)
	}
	if got.Findings[0].ID != "app-correctness/no-owner-todos" || got.Findings[0].Severity != "WARN" || got.Findings[0].Message != "TODO without owner" {
		t.Errorf("finding[0] lost data: %+v", got.Findings[0])
	}
	if got.Findings[1].Severity != "BLOCK" {
		t.Errorf("finding[1] severity = %q, want BLOCK", got.Findings[1].Severity)
	}
}

// TestAuditRecord_NotificationRoundTrip exercises the new Notification field:
// an AuditRecord with NotificationStatus set must JSON round-trip without
// losing any lifecycle data.
func TestAuditRecord_NotificationRoundTrip(t *testing.T) {
	queuedAt := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	deliveredAt := time.Date(2026, 6, 4, 12, 0, 5, 0, time.UTC)
	rec := AuditRecord{
		Repo:   "demo",
		Accept: false,
		Refs:   []string{"refs/heads/main"},
		Notification: &NotificationStatus{
			EventID:          "evt_abc",
			QueuedAt:         queuedAt,
			InlineAttempted:  true,
			InlineSucceeded:  true,
			DeliveredAt:      deliveredAt,
			DeliveryAttempts: 1,
		},
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got AuditRecord
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Notification == nil {
		t.Fatalf("Notification lost on round-trip; raw=%s", b)
	}
	if got.Notification.EventID != "evt_abc" {
		t.Errorf("EventID = %q, want evt_abc", got.Notification.EventID)
	}
	if !got.Notification.QueuedAt.Equal(queuedAt) {
		t.Errorf("QueuedAt = %v, want %v", got.Notification.QueuedAt, queuedAt)
	}
	if !got.Notification.InlineAttempted || !got.Notification.InlineSucceeded {
		t.Errorf("inline flags lost: attempted=%v succeeded=%v", got.Notification.InlineAttempted, got.Notification.InlineSucceeded)
	}
	if !got.Notification.DeliveredAt.Equal(deliveredAt) {
		t.Errorf("DeliveredAt = %v, want %v", got.Notification.DeliveredAt, deliveredAt)
	}
	if got.Notification.DeliveryAttempts != 1 {
		t.Errorf("DeliveryAttempts = %d, want 1", got.Notification.DeliveryAttempts)
	}
	if got.Notification.Deadlettered {
		t.Errorf("Deadlettered = true, want false (not set)")
	}
}

// TestAuditRecord_OldJSONLineBackwardsCompat is the load-bearing
// backwards-compat invariant: OLD audit log lines (written before the
// notification field existed) must unmarshal cleanly with Notification == nil.
// If this test fails, every existing audit log on every nimblegate instance
// would silently lose readability - we'd be breaking the persistence contract.
func TestAuditRecord_OldJSONLineBackwardsCompat(t *testing.T) {
	// A representative pre-Notification audit line: no "notification" key at all.
	oldLine := `{"time":"2026-06-01T10:00:00Z","repo":"demo","refs":["refs/heads/main"],"accept":false,"messages":["BLOCK security/x"]}`
	var got AuditRecord
	if err := json.Unmarshal([]byte(oldLine), &got); err != nil {
		t.Fatalf("old line failed to unmarshal: %v", err)
	}
	if got.Notification != nil {
		t.Errorf("Notification should be nil for old lines, got %+v", got.Notification)
	}
	if got.Repo != "demo" || got.Accept {
		t.Errorf("other fields corrupted: %+v", got)
	}
	if len(got.Messages) != 1 || got.Messages[0] != "BLOCK security/x" {
		t.Errorf("messages lost: %+v", got.Messages)
	}
}
