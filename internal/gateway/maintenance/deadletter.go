// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nimblegate/internal/gateway/notification"
)

// defaultDeadletterRetention is the operator-tunable knob's default. 30 days
// is long enough that an oncall who notices "webhook receiver was down last
// week" can still pull the affected payloads, short enough that a year of
// busy traffic doesn't pile up megabytes of stale failures.
const defaultDeadletterRetention = 30 * 24 * time.Hour

// DeadletterResult is per-repo result for /health.
type DeadletterResult struct {
	Repo    string
	Scanned int // total records read
	Kept    int // records younger than retention
	Pruned  int // records older than retention (rewritten out)
	Err     error
	Took    time.Time // when this sweep ran
}

// runDeadletterPrune walks the policy root for <repo>/pr-comment-deadletter.jsonl
// files and rewrites each to drop records whose queued_at is older than
// retention. Atomic rewrite via temp file + rename. Missing files are no-ops
// (a healthy repo with no failed deliveries has no file).
func runDeadletterPrune(now func() time.Time, policyRoot string, retention time.Duration) []DeadletterResult {
	entries, err := os.ReadDir(policyRoot)
	if err != nil {
		return nil
	}
	cutoff := now().Add(-retention)
	var out []DeadletterResult
	for _, e := range entries {
		if !entryIsDir(policyRoot, e) {
			continue
		}
		name := e.Name()
		if len(name) > 0 && name[0] == '_' {
			continue
		}
		// Delivery outcomes share the deadletter's retention; they are bookkeeping
		// for the feed, not reported on /health.
		if p := filepath.Join(policyRoot, name, notification.OutcomesFile); fileNonEmpty(p) {
			_ = pruneLocked(now, name, p, cutoff)
		}
		path := filepath.Join(policyRoot, name, "pr-comment-deadletter.jsonl")
		if !fileNonEmpty(path) {
			continue // no deadletter file for this repo - healthy
		}
		out = append(out, pruneLocked(now, name, path, cutoff))
	}
	return out
}

func fileNonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// pruneLocked holds the file's queue lock so a record the daemon appends
// mid-rewrite is not lost.
func pruneLocked(now func() time.Time, repo, path string, cutoff time.Time) DeadletterResult {
	unlock, err := notification.LockFile(path)
	if err != nil {
		return DeadletterResult{Repo: repo, Took: now(), Err: fmt.Errorf("lock %s: %w", path, err)}
	}
	defer unlock()
	return pruneOneDeadletter(now, repo, path, cutoff)
}

// entryIsDir follows symlinks: registered repos are activation symlinks
// (<policy-root>/<name> -> _repos/<name>), for which DirEntry.IsDir is false.
func entryIsDir(parent string, e os.DirEntry) bool {
	fi, err := os.Stat(filepath.Join(parent, e.Name()))
	return err == nil && fi.IsDir()
}

// pruneOneDeadletter rewrites one deadletter file in place, keeping only
// records with queued_at >= cutoff. Format: each line is JSON with at least
// a "queued_at" field (RFC3339). Unparseable lines are dropped silently
// (consistent with the existing queue parser's "skip malformed" behavior).
func pruneOneDeadletter(now func() time.Time, repo, path string, cutoff time.Time) DeadletterResult {
	res := DeadletterResult{Repo: repo, Took: now()}
	in, err := os.Open(path)
	if err != nil {
		res.Err = fmt.Errorf("open %s: %w", path, err)
		return res
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".deadletter-rewrite-*")
	if err != nil {
		res.Err = fmt.Errorf("create tmp: %w", err)
		return res
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath) // no-op if rename succeeded
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		res.Err = fmt.Errorf("chmod tmp: %w", err)
		return res
	}

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	w := bufio.NewWriter(tmp)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		res.Scanned++
		var head struct {
			QueuedAt time.Time `json:"queued_at"`
		}
		if err := json.Unmarshal(line, &head); err != nil {
			// Drop unparseable line (matches existing queue parser policy).
			res.Pruned++
			continue
		}
		if head.QueuedAt.Before(cutoff) {
			res.Pruned++
			continue
		}
		if _, err := w.Write(line); err != nil {
			res.Err = fmt.Errorf("write: %w", err)
			_ = tmp.Close()
			return res
		}
		if err := w.WriteByte('\n'); err != nil {
			res.Err = err
			_ = tmp.Close()
			return res
		}
		res.Kept++
	}
	if err := scanner.Err(); err != nil {
		res.Err = fmt.Errorf("scan: %w", err)
		_ = tmp.Close()
		return res
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		res.Err = err
		return res
	}
	if err := tmp.Close(); err != nil {
		res.Err = err
		return res
	}
	if err := os.Rename(tmpPath, path); err != nil {
		res.Err = fmt.Errorf("rename: %w", err)
		return res
	}
	return res
}
