// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nimblegate/internal/gateway/notification"
)

// ReadDecisions discovers <policyRoot>/*/audit.log, tails the last tailPerRepo
// lines of each, parses the JSONL into AuditRecords, skips malformed lines.
// Fail-soft: unreadable/absent files yield nothing, never an error.
func ReadDecisions(policyRoot string, tailPerRepo int) []AuditRecord {
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "audit.log"))
	var out []AuditRecord
	for _, p := range matches {
		recs := tailParse(p, tailPerRepo)
		CorrelateNotificationStatus(filepath.Dir(p), recs)
		out = append(out, recs...)
	}
	return out
}

// CorrelateNotificationStatus recovers the live delivery state of each record's
// notification by cross-referencing its EventID against the repo's queue +
// deadletter files. The audit log is append-only and the daemon delivers
// asynchronously, so a record only stores "a notification fired (EventID)" at
// push time - the actual outcome is computed here at read time:
//
//	in deadletter file → deadlettered
//	in queue file      → still queued (left as-is)
//	in neither         → delivered (removed from the queue on success)
func CorrelateNotificationStatus(repoDir string, recs []AuditRecord) {
	queued := notifIDSet(filepath.Join(repoDir, "pr-comment-queue.jsonl"))
	dead := notifIDSet(filepath.Join(repoDir, "pr-comment-deadletter.jsonl"))
	for i := range recs {
		n := recs[i].Notification
		if n == nil || n.EventID == "" {
			continue
		}
		// Respect any terminal state already written into the record.
		if n.Deadlettered || !n.DeliveredAt.IsZero() || n.InlineSucceeded {
			continue
		}
		switch {
		case dead[n.EventID]:
			n.Deadlettered = true
		case queued[n.EventID]:
			// still pending - leave as queued
		default:
			n.DeliveredAt = n.QueuedAt
			if n.DeliveredAt.IsZero() {
				n.DeliveredAt = recs[i].Time
			}
		}
	}
}

// notifIDSet reads a queue/deadletter .jsonl and returns the set of record IDs.
func notifIDSet(path string) map[string]bool {
	recs, _ := notification.ReadQueueRecords(path)
	if len(recs) == 0 {
		return nil
	}
	m := make(map[string]bool, len(recs))
	for _, r := range recs {
		m[r.ID] = true
	}
	return m
}

// tailParse keeps the last n lines (memory-bounded ring buffer) and parses them.
func tailParse(path string, n int) []AuditRecord {
	if n <= 0 {
		n = 500
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if n > 0 && len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, line)
	}

	recs := make([]AuditRecord, 0, len(ring))
	for _, line := range ring {
		var r AuditRecord
		if json.Unmarshal([]byte(line), &r) == nil {
			recs = append(recs, r)
		}
	}
	return recs
}

// ReadDecisionsBefore is ReadDecisions with a time cursor for backward paging.
// before.IsZero() → newest tailPerRepo per repo (identical to ReadDecisions).
// Otherwise → the newest tailPerRepo records per repo whose Time is strictly
// before `before`. Fail-soft like ReadDecisions.
func ReadDecisionsBefore(policyRoot string, before time.Time, tailPerRepo int) []AuditRecord {
	if before.IsZero() {
		return ReadDecisions(policyRoot, tailPerRepo)
	}
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "audit.log"))
	var out []AuditRecord
	for _, p := range matches {
		recs := parseFilteredBefore(p, before, tailPerRepo)
		CorrelateNotificationStatus(filepath.Dir(p), recs)
		out = append(out, recs...)
	}
	return out
}

// parseFilteredBefore scans the whole file (records older-than-before are not
// at the tail), parses each line, and keeps the newest n records with
// Time < before via a ring buffer. Malformed lines are skipped (read path -
// unlike the retention path, a reader simply can't render an unparseable line).
// Memory is bounded by n; time is O(file), acceptable because retention bounds
// file size. A tail-from-end optimization is a later concern (spec: correctness first).
func parseFilteredBefore(path string, before time.Time, n int) []AuditRecord {
	if n <= 0 {
		n = 500
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	ring := make([]AuditRecord, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var r AuditRecord
		if json.Unmarshal(line, &r) != nil {
			continue
		}
		if !r.Time.Before(before) {
			continue
		}
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, r)
	}
	return ring
}
