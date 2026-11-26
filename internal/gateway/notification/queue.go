// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// QueueRecord is one pending delivery in pr-comment-queue.jsonl. The record
// outlives any single inline attempt - the durability guarantee in spec §3.4
// is "queue write before any network call." The daemon drains queue records
// older than 30s (avoids racing the pre-receive inline attempt).
type QueueRecord struct {
	ID           string       `json:"id"` // matches Notification.EventID
	QueuedAt     time.Time    `json:"queued_at"`
	UpstreamKind string       `json:"upstream_kind"` // "gitea" | "github" | ... - looked up by registry
	Notification Notification `json:"notification"`
	// WebhookURL + WebhookAuth are read only from records queued before the
	// webhook was resolved from gateway.toml at delivery time; new records
	// leave them empty so no secret is written to the queue or deadletter.
	WebhookURL  string           `json:"webhook_url,omitempty"`
	WebhookAuth WebhookAuth      `json:"webhook_auth,omitempty"`
	State       QueueRecordState `json:"state"`
	// LoopConfig drives the per-PR loop state machine (attempt count + bot
	// rotation) at delivery time, when the PR number is finally known. Set by
	// pre-receive from the repo's [notification.loop]/[mention] config. Zero
	// (MaxAttempts 0) means "no loop tracking" - the delivery still posts the
	// comment, it just doesn't advance attempt counters.
	LoopConfig LoopConfig `json:"loop_config,omitempty"`
	// Cooldown pauses delivery for a PR whose rejects arrive faster than the
	// threshold. Zero means no cooldown.
	Cooldown CooldownConfig `json:"cooldown,omitempty"`
	// OmitPRPeople is [notification.mention] include-pr-assignees = false:
	// the comment tags no assignees or reviewers. Negated so records queued
	// before the field existed keep the default (tag them).
	OmitPRPeople bool `json:"omit_pr_people,omitempty"`
	// Delivery is the repo's [notification.delivery] retry policy. Zero
	// fields fall back to the daemon defaults.
	Delivery         DeliveryPolicy `json:"delivery,omitempty"`
	DeliveryAttempts int            `json:"delivery_attempts"`
	LastError        string         `json:"last_error,omitempty"`
	NextRetryAt      time.Time      `json:"next_retry_at,omitempty"`
}

// DeliveryPolicy is the per-repo retry cap and backoff schedule.
type DeliveryPolicy struct {
	MaxAttempts int             `json:"max_attempts,omitempty"`
	Backoff     []time.Duration `json:"backoff,omitempty"`
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
// absent, mode 0600), under the path's lock so it cannot land between a
// rewriter's read and rename.
func AppendQueueRecord(path string, rec QueueRecord) error {
	unlock, err := LockFile(path)
	if err != nil {
		return err
	}
	defer unlock()
	return appendQueueRecord(path, rec)
}

func appendQueueRecord(path string, rec QueueRecord) error {
	if rec.QueuedAt.IsZero() {
		rec.QueuedAt = time.Now().UTC()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return appendLine(path, b)
}

func appendLine(path string, b []byte) (err error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if _, err = f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
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
	unlock, err := LockFile(path)
	if err != nil {
		return err
	}
	defer unlock()
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

// MarkDelivered removes a delivered record from the queue and records the
// delivery in the repo's outcomes file.
func MarkDelivered(queuePath, id string) error {
	if err := RemoveQueueRecord(queuePath, id); err != nil {
		return err
	}
	return AppendOutcome(filepath.Join(filepath.Dir(queuePath), OutcomesFile), id, OutcomeDelivered)
}

// MoveToDeadletter removes the record with rec.ID from queuePath and appends
// rec - the caller's copy, carrying the final attempt count and last error -
// to deadletterPath. Append to the deadletter first so the record is not lost
// if the queue rewrite fails. Missing ID = no-op.
func MoveToDeadletter(queuePath, deadletterPath string, rec QueueRecord) error {
	unlockQ, err := LockFile(queuePath)
	if err != nil {
		return err
	}
	defer unlockQ()
	records, err := ReadQueueRecords(queuePath)
	if err != nil {
		return err
	}
	found := false
	filtered := make([]QueueRecord, 0, len(records))
	for _, r := range records {
		if r.ID == rec.ID {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !found {
		return nil
	}
	unlockD, err := LockFile(deadletterPath)
	if err != nil {
		return err
	}
	err = appendQueueRecord(deadletterPath, rec)
	unlockD()
	if err != nil {
		return err
	}
	return writeQueueAtomic(queuePath, filtered)
}

// UpdateQueueRecord rewrites the record with matching rec.ID in-place via the
// atomic temp+rename rewrite. Used by the daemon to bump DeliveryAttempts +
// NextRetryAt + LastError after a failed delivery (the record stays in the
// queue for the next backoff window). Missing ID = no-op.
func UpdateQueueRecord(path string, rec QueueRecord) error {
	unlock, err := LockFile(path)
	if err != nil {
		return err
	}
	defer unlock()
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
// resolutions or overlap reports (Event != "push.resolved" / "push.overlap"),
// atomically. Called when a clean push
// resolves a ref's loop: any reject records still pending for that ref are now
// moot, and delivering one AFTER the resolution would flip the ✅ comment back to
// ⛔ - the resolution clears the PR state, so a late reject starts a fresh
// attempt 1 and re-opens the loop. The just-appended resolution record is kept.
// Each dropped record is noted in the outcomes file. Returns the number removed.
func RemovePendingRejectsForRef(path, ref string) (int, error) {
	unlock, err := LockFile(path)
	if err != nil {
		return 0, err
	}
	defer unlock()
	records, err := ReadQueueRecords(path)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	kept := make([]QueueRecord, 0, len(records))
	var removed []string
	for _, r := range records {
		if r.Notification.Event != "push.resolved" && r.Notification.Event != "push.overlap" && recordTargetsRef(r, ref) {
			removed = append(removed, r.ID)
			continue
		}
		kept = append(kept, r)
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := writeQueueAtomic(path, kept); err != nil {
		return 0, err
	}
	outcomes := filepath.Join(filepath.Dir(path), OutcomesFile)
	for _, id := range removed {
		_ = AppendOutcome(outcomes, id, OutcomeDropped)
	}
	return len(removed), nil
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
	unlock, err := LockFile(path)
	if err != nil {
		return 0, err
	}
	defer unlock()
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
	unlockQ, err := LockFile(queuePath)
	if err != nil {
		return 0, err
	}
	defer unlockQ()
	unlockD, err := LockFile(deadletterPath)
	if err != nil {
		return 0, err
	}
	defer unlockD()
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
		if err := appendQueueRecord(queuePath, dead[i]); err != nil {
			return 0, err
		}
	}
	// All records moved back to the queue - empty the deadletter file.
	if err := writeQueueAtomic(deadletterPath, nil); err != nil {
		return 0, err
	}
	return len(dead), nil
}

// writeQueueAtomic rewrites path with records via a temp file in the same
// directory + rename. If the rewrite is interrupted, the original file is
// unchanged. Callers hold the path's lock.
func writeQueueAtomic(path string, records []QueueRecord) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	abort := func(origErr error) error {
		err := errors.Join(origErr, f.Close())
		os.Remove(tmp)
		return err
	}
	for _, r := range records {
		b, merr := json.Marshal(r)
		if merr != nil {
			return abort(merr)
		}
		if _, werr := f.Write(append(b, '\n')); werr != nil {
			return abort(werr)
		}
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// OutcomesFile records how each record left the queue, so the feed can say
// "delivered" from evidence instead of inferring it from absence.
const OutcomesFile = "pr-comment-outcomes.jsonl"

const (
	OutcomeDelivered = "delivered"
	OutcomeDropped   = "dropped" // superseded by a resolution before delivery
)

// Outcome is one line of OutcomesFile. The timestamp is tagged "queued_at" so
// the deadletter retention prune handles this file unchanged.
type Outcome struct {
	ID      string    `json:"id"`
	Outcome string    `json:"outcome"`
	At      time.Time `json:"queued_at"`
}

// AppendOutcome appends one outcome line to path.
func AppendOutcome(path, id, outcome string) error {
	b, err := json.Marshal(Outcome{ID: id, Outcome: outcome, At: time.Now().UTC()})
	if err != nil {
		return err
	}
	unlock, err := LockFile(path)
	if err != nil {
		return err
	}
	defer unlock()
	return appendLine(path, b)
}

// ReadOutcomes returns the outcomes in path keyed by record ID. Missing file
// = nil.
func ReadOutcomes(path string) map[string]Outcome {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	out := map[string]Outcome{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var o Outcome
		if json.Unmarshal(sc.Bytes(), &o) == nil && o.ID != "" {
			out[o.ID] = o
		}
	}
	return out
}
