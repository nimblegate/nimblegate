// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Daemon polls all repos' queue files and drains pending notification
// deliveries through the Orchestrator. Runs as a goroutine alongside the
// dashboard HTTP server (wired in a subsequent task).
//
// Per spec §3.2: drains queue records older than InlineRaceGap (avoids
// racing pre-receive's inline attempt). Per-record exponential backoff with
// deadletter routing after DeliveryMaxAttempts failed attempts.
type Daemon struct {
	PolicyRoot   string // root of <policy-root>/<repo>/ directories
	Orchestrator *Orchestrator
	Config       DaemonConfig
	Now          func() time.Time // injected for tests; defaults to time.Now().UTC
}

// DaemonConfig is the operator-tunable shape of the drain loop. A zero
// PollInterval triggers the DefaultDaemonConfig substitution so callers can
// pass Daemon{} for defaults.
type DaemonConfig struct {
	PollInterval        time.Duration   // default 5s
	InlineRaceGap       time.Duration   // default 30s - daemon ignores records younger than this
	DeliveryMaxAttempts int             // default 20
	Backoff             []time.Duration // default {1m, 5m, 30m, 2h} - last entry repeats
}

// DefaultDaemonConfig returns the spec-default tuning per §3.2.
func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		PollInterval:        5 * time.Second,
		InlineRaceGap:       30 * time.Second,
		DeliveryMaxAttempts: 20,
		Backoff:             []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour},
	}
}

// PollOnce scans every <policy-root>/<repo>/pr-comment-queue.jsonl and tries
// to drain pending records older than InlineRaceGap whose NextRetryAt has
// passed. Idempotent: running twice produces the same end state.
func (d *Daemon) PollOnce(ctx context.Context) error {
	d.resolveDefaults()
	repos, err := scanRepos(d.PolicyRoot)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		queuePath := filepath.Join(d.PolicyRoot, repo, "pr-comment-queue.jsonl")
		deadletterPath := filepath.Join(d.PolicyRoot, repo, "pr-comment-deadletter.jsonl")
		d.drainQueue(ctx, queuePath, deadletterPath)
	}
	return nil
}

// drainQueue walks one repo's queue and tries each eligible record once. Per
// the spec, one PollOnce sweep is "one attempt per eligible record"; the
// next poll re-evaluates after the backoff has had time to land.
func (d *Daemon) drainQueue(ctx context.Context, queuePath, deadletterPath string) {
	records, err := ReadQueueRecords(queuePath)
	if err != nil {
		log.Printf("notification daemon: read queue %s: %v", queuePath, err)
		return
	}
	for _, rec := range records {
		if d.Now().Sub(rec.QueuedAt) < d.Config.InlineRaceGap {
			continue
		}
		if !rec.NextRetryAt.IsZero() && d.Now().Before(rec.NextRetryAt) {
			continue
		}
		if err := d.Orchestrator.DeliverOne(ctx, rec); err != nil {
			log.Printf("notification daemon: deliver %s (repo %s): %v", rec.ID, rec.Notification.Repo.Name, err)
			rec.DeliveryAttempts++
			rec.LastError = err.Error()
			rec.NextRetryAt = d.Now().Add(computeBackoff(rec.DeliveryAttempts, d.Config.Backoff))
			if rec.DeliveryAttempts >= d.Config.DeliveryMaxAttempts {
				if mErr := MoveToDeadletter(queuePath, deadletterPath, rec.ID); mErr != nil {
					log.Printf("notification daemon: move to deadletter %s: %v", rec.ID, mErr)
				}
				continue
			}
			if uErr := UpdateQueueRecord(queuePath, rec); uErr != nil {
				log.Printf("notification daemon: update queue record %s: %v", rec.ID, uErr)
			}
			continue
		}
		if rErr := RemoveQueueRecord(queuePath, rec.ID); rErr != nil {
			log.Printf("notification daemon: remove delivered record %s: %v", rec.ID, rErr)
		}
	}
}

// resolveDefaults fills in zero-valued config + Now from the spec defaults.
// Idempotent: safe to call from both Run and PollOnce.
func (d *Daemon) resolveDefaults() {
	if d.Config.PollInterval == 0 {
		d.Config = DefaultDaemonConfig()
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
}

// computeBackoff returns the delay for the given attempt number (1-indexed).
// Last schedule entry repeats for any attempt past its length.
func computeBackoff(attempt int, schedule []time.Duration) time.Duration {
	if attempt <= 0 || len(schedule) == 0 {
		return time.Minute
	}
	if attempt > len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempt-1]
}

// scanRepos returns subdirectory names under policyRoot - each is a repo
// the gateway has configured. Skips files that aren't directories. Missing
// root returns an empty slice + no error (daemon survives an early-boot
// race where the dashboard creates the dir after the daemon starts).
func scanRepos(policyRoot string) ([]string, error) {
	entries, err := os.ReadDir(policyRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		// Skip the internal lib root + event/archive dirs.
		if strings.HasPrefix(name, "_") {
			continue
		}
		// Registered repos are activation symlinks (<policy-root>/<name> ->
		// _repos/<name>). os.Stat follows the symlink; e.IsDir() is Lstat-based
		// and returns false for a symlink, which would make the daemon skip
		// every registered repo and never drain its queue.
		fi, statErr := os.Stat(filepath.Join(policyRoot, name))
		if statErr != nil || !fi.IsDir() {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}
