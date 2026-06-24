// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"os"
	"time"
)

// AuditRecord is one gate decision, appended to the gateway's decision log.
type AuditRecord struct {
	Time         time.Time           `json:"time"`
	Repo         string              `json:"repo"`
	Refs         []string            `json:"refs"`
	RefUpdates   []RefUpdate         `json:"ref_updates,omitempty"` // full ref tuples (name + old/new SHAs); added 2026-06-04. Older log lines have nil here; readers fall back to Refs (names only).
	Accept       bool                `json:"accept"`
	Observed     bool                `json:"observed,omitempty"` // true → would have rejected, but observe mode relayed it
	Messages     []string            `json:"messages,omitempty"`
	Findings     []Finding           `json:"findings,omitempty"`
	Suppressed   []Suppression       `json:"suppressed,omitempty"`
	Notification *NotificationStatus `json:"notification,omitempty"`
}

// NotificationStatus records the notification-rail lifecycle per AuditRecord.
// Nil = notification rail disabled or not yet evaluated; non-nil = rail engaged.
//
// The lifecycle states travel through three updates:
//  1. pre-receive sets EventID + QueuedAt + InlineAttempted ± InlineSucceeded
//  2. daemon's successful drain sets DeliveredAt + DeliveryAttempts
//  3. daemon's max-attempts-exceeded sets Deadlettered = true
type NotificationStatus struct {
	EventID          string    `json:"event_id"`
	QueuedAt         time.Time `json:"queued_at"`
	InlineAttempted  bool      `json:"inline_attempted"`
	InlineSucceeded  bool      `json:"inline_succeeded"`
	InlineError      string    `json:"inline_error,omitempty"`
	DeliveredAt      time.Time `json:"delivered_at,omitempty"`
	DeliveryAttempts int       `json:"delivery_attempts,omitempty"`
	Deadlettered     bool      `json:"deadlettered,omitempty"`
}

// AppendAudit appends one record as a JSON line to path (created if absent).
func AppendAudit(path string, rec AuditRecord) error {
	if rec.Time.IsZero() {
		rec.Time = time.Now().UTC()
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
