// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"time"
)

// QueueRecord is one pending delivery in pr-comment-queue.jsonl. The record
// outlives any single inline attempt - the durability guarantee in spec §3.4
// is "queue write before any network call." The daemon drains queue records
// older than 30s (avoids racing the pre-receive inline attempt).
type QueueRecord struct {
	ID           string           `json:"id"` // matches Notification.EventID
	QueuedAt     time.Time        `json:"queued_at"`
	UpstreamKind string           `json:"upstream_kind"` // "gitea" | "github" | ... - looked up by registry
	Notification Notification     `json:"notification"`
	WebhookURL   string           `json:"webhook_url,omitempty"`
	WebhookAuth  WebhookAuth      `json:"webhook_auth,omitempty"`
	State        QueueRecordState `json:"state"`
	// LoopConfig drives the per-PR loop state machine (attempt count + bot
	// rotation) at delivery time, when the PR number is finally known. Set by
	// pre-receive from the repo's [notification.loop]/[mention] config. Zero
	// (MaxAttempts 0) means "no loop tracking" - the delivery still posts the
	// comment, it just doesn't advance attempt counters.
	LoopConfig       LoopConfig `json:"loop_config,omitempty"`
	DeliveryAttempts int        `json:"delivery_attempts"`
	LastError        string     `json:"last_error,omitempty"`
	NextRetryAt      time.Time  `json:"next_retry_at,omitempty"`
}

// QueueRecordState carries the per-PR state references the orchestrator needs
// to upsert the sticky comment. Filled by pre-receive at queue-write time.
type QueueRecordState struct {
	PRNumber        int    `json:"pr_number,omitempty"`
	StickyCommentID string `json:"sticky_comment_id,omitempty"`
}

// WebhookAuth carries the verification mode + secret (or token) for webhook
// delivery. Mode "none" sends no auth; "hmac" signs the payload; "bearer"
// sends the secret as Authorization: Bearer <secret>.
type WebhookAuth struct {
	Mode       string `json:"mode"`                  // "hmac" | "bearer" | "none"
	Secret     string `json:"secret,omitempty"`      // signing key or token
	HeaderName string `json:"header_name,omitempty"` // optional override
}

// AppendQueueRecord writes one record as a JSON line to path (created if
// absent, mode 0600). Atomic per-line: a partial write doesn't corrupt
// previously appended records because each line ends with '\n' and parsers
// skip malformed lines (see ReadQueueRecords).
func AppendQueueRecord(path string, rec QueueRecord) error {
	if rec.QueuedAt.IsZero() {
		rec.QueuedAt = time.Now().UTC()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// ReadQueueRecords returns every parseable record from path. Missing file =
// empty + no error (gateway not yet active for this repo). Malformed lines
// are skipped silently (partial-write recovery) - the daemon trusts what it
// can read and drops what it can't.
func ReadQueueRecords(path string) ([]QueueRecord, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []QueueRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec QueueRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue // partial-write tolerance
		}
		out = append(out, rec)
	}
	return out, sc.Err()
}

// RemoveQueueRecord removes the record with matching ID by rewriting the file
// atomically (temp + rename). Missing ID = no-op. This is the success path
// from a delivery attempt: drop the record from the queue after the upstream
// API accepted it.
func RemoveQueueRecord(path, id string) error {
	records, err := ReadQueueRecords(path)
	if err != nil {
		return err
	}
	filtered := make([]QueueRecord, 0, len(records))
	for _, r := range records {
		if r.ID != id {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) == len(records) {
		return nil // no-op: id not present
	}
	return writeQueueAtomic(path, filtered)
}

// MoveToDeadletter removes the record from queuePath and appends it to
// deadletterPath. Used when DeliveryAttempts exceeds the configured cap.
// Two-step: append to deadletter first (ensure record is not lost if the
// queue rewrite fails), then remove from queue.
func MoveToDeadletter(queuePath, deadletterPath, id string) error {
	records, err := ReadQueueRecords(queuePath)
	if err != nil {
		return err
	}
	var found *QueueRecord
	filtered := make([]QueueRecord, 0, len(records))
	for i, r := range records {
		if r.ID == id {
			found = &records[i]
			continue
		}
		filtered = append(filtered, r)
	}
	if found == nil {
		return nil
	}
	if err := AppendQueueRecord(deadletterPath, *found); err != nil {
		return err
	}
	return writeQueueAtomic(queuePath, filtered)
}

// UpdateQueueRecord rewrites the record with matching rec.ID in-place via the
// atomic temp+rename rewrite. Used by the daemon to bump DeliveryAttempts +
// NextRetryAt + LastError after a failed delivery (the record stays in the
// queue for the next backoff window). Missing ID = no-op.
func UpdateQueueRecord(path string, rec QueueRecord) error {
	records, err := ReadQueueRecords(path)
	if err != nil {
		return err
	}
	found := false
	for i, r := range records {
		if r.ID == rec.ID {
			records[i] = rec
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	return writeQueueAtomic(path, records)
}

// RemovePendingRejectsForRef drops queued records targeting `ref` that are NOT
// resolutions (Event != "push.resolved"), atomically. Called when a clean push
// resolves a ref's loop: any reject records still pending for that ref are now
// moot, and delivering one AFTER the resolution would flip the ✅ comment back to
// ⛔ - the resolution clears the PR state, so a late reject starts a fresh
// attempt 1 and re-opens the loop. The just-appended resolution record is kept.
// Returns the number removed.
func RemovePendingRejectsForRef(path, ref string) (int, error) {
	records, err := ReadQueueRecords(path)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	kept := make([]QueueRecord, 0, len(records))
	removed := 0
	for _, r := range records {
		if r.Notification.Event != "push.resolved" && recordTargetsRef(r, ref) {
			removed++
			continue
		}
		kept = append(kept, r)
	}
	if removed == 0 {
		return 0, nil
	}
	if err := writeQueueAtomic(path, kept); err != nil {
		return 0, err
	}
	return removed, nil
}

// recordTargetsRef reports whether the record's notification is for `ref`.
func recordTargetsRef(r QueueRecord, ref string) bool {
	for _, rf := range r.Notification.Push.Refs {
		if rf.Name == ref {
			return true
		}
	}
	return false
}

// ResetQueueBackoff clears the retry backoff on every queued record - zeroing
// NextRetryAt, DeliveryAttempts, and LastError - so the daemon's next poll
// attempts delivery immediately. This is the operator's "Retry now" after fixing
// a bad upstream token: without it, records that failed for an hour sit on a
// multi-hour backoff before the fix takes effect. Returns the number reset.
func ResetQueueBackoff(path string) (int, error) {
	records, err := ReadQueueRecords(path)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	for i := range records {
		records[i].NextRetryAt = time.Time{}
		records[i].DeliveryAttempts = 0
		records[i].LastError = ""
	}
	if err := writeQueueAtomic(path, records); err != nil {
		return 0, err
	}
	return len(records), nil
}

// RequeueDeadletter moves every deadlettered record back into the queue with a
// cleared backoff, then empties the deadletter file. Pairs with ResetQueueBackoff
// for "Retry now": records that already exhausted max-attempts during a bad-token
// window can be retried once the token is fixed - without server access. Returns
// the number requeued.
func RequeueDeadletter(queuePath, deadletterPath string) (int, error) {
	dead, err := ReadQueueRecords(deadletterPath)
	if err != nil {
		return 0, err
	}
	if len(dead) == 0 {
		return 0, nil
	}
	for i := range dead {
		dead[i].NextRetryAt = time.Time{}
		dead[i].DeliveryAttempts = 0
		dead[i].LastError = ""
		if err := AppendQueueRecord(queuePath, dead[i]); err != nil {
			return 0, err
		}
	}
	// All records moved back to the queue - empty the deadletter file.
	if err := writeQueueAtomic(deadletterPath, nil); err != nil {
		return 0, err
	}
	return len(dead), nil
}

// writeQueueAtomic rewrites path with records via temp + rename. If the
// rewrite is interrupted, the original file is unchanged.
func writeQueueAtomic(path string, records []QueueRecord) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	for _, r := range records {
		b, err := json.Marshal(r)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
