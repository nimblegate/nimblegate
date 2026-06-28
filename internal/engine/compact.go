// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultCompactQuiescence is the minimum time a part file's mtime
// must be in the past before compaction considers it safe to consume.
// The window protects against compacting a file that's still being
// written to (or about to be - a slow-running agent that pauses
// between writes).
const DefaultCompactQuiescence = 5 * time.Minute

// CompactionResult summarizes one CompactAudit run.
type CompactionResult struct {
	PartsConsidered int
	PartsConsumed   int
	BytesAppended   int64
	Skipped         []string // why each non-consumed part was skipped
}

// CompactAudit walks the project's `audit.parts/` directory, appends
// the contents of every quiescent part file into the consolidated
// `audit.log`, and removes the consumed parts.
//
// A part is "quiescent" when its mtime is older than `quiescence`. The
// window protects against an active writer racing with the compactor:
// only files that haven't been written to for the window are eligible.
// A typical writer's part file becomes eligible shortly after the
// process exits.
//
// Concurrency: a sentinel lock file in the parts directory prevents two
// compactors from running simultaneously. The lock is held via O_CREATE|
// O_EXCL - if another process holds it, this call returns (0 consumed,
// nil) immediately. This means concurrent compaction calls are safe but
// not coordinated; the second call simply skips.
//
// audit.log rotation: when appending parts would push audit.log past
// the rotation threshold (env APPFRAMES_AUDIT_MAX_BYTES), the consolidated
// log is rotated first via the same .N suffix scheme used elsewhere.
//
// Returns a CompactionResult summarizing what happened. Non-fatal
// per-part errors (e.g. one part file can't be read) are collected in
// Skipped; the function continues to the next part. The error return
// is reserved for catastrophic failures (can't open audit.log, can't
// acquire/release the sentinel lock).
func CompactAudit(projectRoot string, quiescence time.Duration) (CompactionResult, error) {
	if quiescence <= 0 {
		quiescence = DefaultCompactQuiescence
	}
	res := CompactionResult{}

	auditLogPath := filepath.Join(projectRoot, ".appframes", "audit.log")
	partsDir := filepath.Join(filepath.Dir(auditLogPath), AuditPartsDirName)

	// No parts dir = nothing to do.
	if _, err := os.Stat(partsDir); err != nil {
		return res, nil
	}

	// Sentinel-file lock prevents concurrent compactors.
	lockPath := filepath.Join(partsDir, ".compact.lock")
	lockFile, err := acquireCompactLock(lockPath)
	if err != nil {
		// Another compactor is running, or we can't create the lock.
		// Either way, return cleanly - compaction is opportunistic.
		return res, nil
	}
	defer releaseCompactLock(lockFile, lockPath)

	parts, err := eligibleParts(partsDir, quiescence)
	if err != nil {
		return res, fmt.Errorf("compact: list parts: %w", err)
	}
	res.PartsConsidered = len(parts)
	if len(parts) == 0 {
		return res, nil
	}

	// Open audit.log for append; create if missing.
	if err := os.MkdirAll(filepath.Dir(auditLogPath), 0o755); err != nil {
		return res, fmt.Errorf("compact: mkdir: %w", err)
	}
	maxBytes := envOrDefaultInt64("APPFRAMES_AUDIT_MAX_BYTES", DefaultAuditMaxBytes)
	maxFiles := clampToInt(envOrDefaultInt64("APPFRAMES_AUDIT_MAX_FILES", DefaultAuditMaxFiles))

	for _, part := range parts {
		data, err := os.ReadFile(part)
		if err != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: %v", part, err))
			continue
		}
		if len(data) == 0 {
			// Empty part - safe to remove.
			_ = os.Remove(part)
			res.PartsConsumed++
			continue
		}

		// Rotate audit.log first if appending would exceed the threshold.
		if maxBytes > 0 {
			info, err := os.Stat(auditLogPath)
			curSize := int64(0)
			if err == nil {
				curSize = info.Size()
			}
			if curSize+int64(len(data)) > maxBytes {
				if rerr := rotateConsolidated(auditLogPath, maxFiles); rerr != nil {
					return res, fmt.Errorf("compact: rotate: %w", rerr)
				}
			}
		}

		f, err := os.OpenFile(auditLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return res, fmt.Errorf("compact: open audit.log: %w", err)
		}
		n, err := f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return res, fmt.Errorf("compact: write audit.log: %w", err)
		}
		if closeErr != nil {
			return res, fmt.Errorf("compact: close audit.log: %w", closeErr)
		}
		res.BytesAppended += int64(n)

		if rmErr := os.Remove(part); rmErr != nil {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: appended but cannot remove (%v)", part, rmErr))
			continue
		}
		res.PartsConsumed++
	}

	return res, nil
}

// eligibleParts returns part file paths under partsDir whose mtime is
// older than `quiescence`. Sorted chronologically by filename (which
// embeds start-time in nanoseconds - see partFilename).
//
// Skips the sentinel lock file and any non-audit.* entries.
func eligibleParts(partsDir string, quiescence time.Duration) ([]string, error) {
	entries, err := os.ReadDir(partsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	cutoff := time.Now().Add(-quiescence)
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "audit.") {
			continue
		}
		full := filepath.Join(partsDir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		out = append(out, full)
	}
	sort.Strings(out)
	return out, nil
}

// rotateConsolidated is CompactAudit's view of rotation: rename
// audit.log → audit.log.1, shift older siblings, drop the oldest past
// maxFiles. Caller already verified the rotation is needed.
func rotateConsolidated(auditLogPath string, maxFiles int) error {
	for i := maxFiles; i >= 1; i-- {
		oldName := fmt.Sprintf("%s.%d", auditLogPath, i)
		if _, err := os.Stat(oldName); err != nil {
			continue
		}
		if i == maxFiles {
			_ = os.Remove(oldName)
			continue
		}
		newName := fmt.Sprintf("%s.%d", auditLogPath, i+1)
		if err := os.Rename(oldName, newName); err != nil {
			return fmt.Errorf("rename %s → %s: %w", oldName, newName, err)
		}
	}
	if _, err := os.Stat(auditLogPath); err == nil {
		if err := os.Rename(auditLogPath, auditLogPath+".1"); err != nil {
			return fmt.Errorf("rename audit.log → .1: %w", err)
		}
	}
	return nil
}

// acquireCompactLock creates the sentinel file with O_EXCL. Returns the
// open file on success; returns an error if the lock is held.
func acquireCompactLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
}

// releaseCompactLock closes and removes the sentinel.
func releaseCompactLock(f *os.File, path string) {
	if f != nil {
		_ = f.Close()
	}
	_ = os.Remove(path)
}

// Discard helps tests verify CompactAudit's behavior without polluting
// stderr. Currently unused - kept for future debug output gating.
var _ = io.Discard
