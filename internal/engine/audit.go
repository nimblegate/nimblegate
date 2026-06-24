// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Default rotation policy for the audit log. Both knobs are tunable via
// environment variables (APPFRAMES_AUDIT_MAX_BYTES, APPFRAMES_AUDIT_MAX_FILES)
// so an integration test can shrink them without code changes.
const (
	DefaultAuditMaxBytes = 10 * 1024 * 1024 // 10 MB
	DefaultAuditMaxFiles = 5                // audit.log + audit.log.1..audit.log.5
)

// Audit is the JSONL audit log writer. Safe for concurrent calls.
//
// As of the multi-agent concurrency work, each process writes to its OWN
// part file under `<projectDir>/audit.parts/audit.<pid>.<starttime>.log`.
// The consolidated `audit.log` is the *read* target, populated by
// CompactAudit. This design eliminates write contention between
// concurrent nimblegate invocations: every process has a private file
// it appends to with no inter-process locking.
type Audit struct {
	mu       sync.Mutex
	f        *os.File
	path     string // logical path (the project's consolidated audit.log)
	partPath string // actual file this writer appends to (per-process)
	maxBytes int64  // 0 means use default; <0 means rotation disabled
	maxFiles int    // number of rotated siblings to retain (audit.log.1 .. audit.log.N)
	curBytes int64  // accurate size of the part file
}

type auditEntry struct {
	Timestamp string `json:"ts"`
	Trigger   string `json:"trigger"`
	Frame     string `json:"frame"`
	Result    string `json:"result"`
	Target    string `json:"target,omitempty"`
	Override  bool   `json:"override"`
	Reason    string `json:"reason,omitempty"`
	Fix       string `json:"fix,omitempty"`
}

// auditSuppression is the JSONL shape for a single hit suppressed by a
// whitelist entry. Recorded separately from the raw CheckResult entry so
// the audit trail captures both: "frame X fired on file Y" AND "hit at
// file Y was then suppressed by the whitelist". A silent bypass is
// impossible.
type auditSuppression struct {
	Timestamp string `json:"ts"`
	Kind      string `json:"kind"` // always "whitelist-suppression"
	Trigger   string `json:"trigger"`
	Frame     string `json:"frame"`
	File      string `json:"file"`
	Label     string `json:"label"`
}

// AuditPartsDirName is the subdirectory under .appframes/ where each
// nimblegate invocation's private audit-log file lives until compaction
// consolidates it into the main audit.log.
const AuditPartsDirName = "audit.parts"

// OpenAudit opens the writer for this nimblegate invocation's private
// audit-log part file under `<dir(path)>/audit.parts/`. The `path`
// argument is treated as the LOGICAL audit log path (the consolidated
// file readers see); the actual writer file is a per-process part.
//
// The parts directory is created if missing. Each part filename
// embeds the PID and the start-time in nanoseconds so collisions
// between simultaneous invocations are impossible.
func OpenAudit(path string) (*Audit, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit: mkdir parent: %w", err)
	}
	partsDir := filepath.Join(filepath.Dir(path), AuditPartsDirName)
	if err := os.MkdirAll(partsDir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: mkdir parts: %w", err)
	}
	partPath := filepath.Join(partsDir, partFilename(os.Getpid(), time.Now().UTC()))
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: open %s: %w", partPath, err)
	}
	a := &Audit{
		f:        f,
		path:     path,
		partPath: partPath,
		maxBytes: envOrDefaultInt64("APPFRAMES_AUDIT_MAX_BYTES", DefaultAuditMaxBytes),
		maxFiles: int(envOrDefaultInt64("APPFRAMES_AUDIT_MAX_FILES", DefaultAuditMaxFiles)),
	}
	if info, err := f.Stat(); err == nil {
		a.curBytes = info.Size()
	}
	return a, nil
}

// partFilename returns the canonical name for a part file. The name
// embeds PID and start-time so it sorts chronologically AND uniquely
// identifies the originating process.
func partFilename(pid int, startTime time.Time) string {
	// Use nanoseconds so two processes that start in the same second
	// get distinct names. The format also sorts lexicographically by
	// time, which CompactAudit relies on for chronological merging.
	return fmt.Sprintf("audit.%d.%d.log", startTime.UnixNano(), pid)
}

// Write appends one JSON line for the given context + result. If the line
// would push the file past maxBytes, the log is rotated first.
func (a *Audit) Write(ctx CheckContext, r CheckResult) error {
	ts := r.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	entry := auditEntry{
		Timestamp: ts.Format(time.RFC3339Nano),
		Trigger:   string(ctx.Trigger),
		Frame:     r.FrameID,
		Result:    string(r.Outcome),
		Target:    ctx.Command,
		Override:  r.Override,
		Reason:    r.Reason,
		Fix:       r.Fix,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	line := append(data, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	// Rotate before writing if this line would push us past the limit.
	// maxBytes <= 0 disables rotation (used by tests that want raw behavior).
	if a.maxBytes > 0 && a.curBytes+int64(len(line)) > a.maxBytes {
		if err := a.rotateLocked(); err != nil {
			return fmt.Errorf("audit: rotate: %w", err)
		}
	}

	if _, err := a.f.Write(line); err != nil {
		return fmt.Errorf("audit: write %s: %w", a.partPath, err)
	}
	a.curBytes += int64(len(line))
	return nil
}

// WriteSuppression records one whitelist-suppression event. Called by
// the trigger layer after ApplyWhitelist returns; one entry per
// suppressed Hit. The audit log thus carries the full chain: raw frame
// result first, then the suppression note that explains why a downstream
// reader doesn't see it in the rendered output.
func (a *Audit) WriteSuppression(ctx CheckContext, s SuppressionLog) error {
	ts := s.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	entry := auditSuppression{
		Timestamp: ts.Format(time.RFC3339Nano),
		Kind:      "whitelist-suppression",
		Trigger:   string(ctx.Trigger),
		Frame:     s.FrameID,
		File:      s.File,
		Label:     s.Label,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("audit: marshal suppression: %w", err)
	}
	line := append(data, '\n')

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.maxBytes > 0 && a.curBytes+int64(len(line)) > a.maxBytes {
		if err := a.rotateLocked(); err != nil {
			return fmt.Errorf("audit: rotate: %w", err)
		}
	}
	if _, err := a.f.Write(line); err != nil {
		return fmt.Errorf("audit: write suppression %s: %w", a.partPath, err)
	}
	a.curBytes += int64(len(line))
	return nil
}

// Close closes the log file.
func (a *Audit) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}

// rotateLocked rotates the writer's part file when it grows past
// maxBytes: partFile → partFile.1, .1 → .2, etc., drops files past
// maxFiles, and reopens a fresh part file. Caller must hold a.mu.
//
// Typical processes never trigger this (part files stay tiny). It's
// here for the rare long-lived agent that writes a lot of audit
// entries. CompactAudit picks up rotated siblings (audit.<pid>.<t>.log.N)
// the same way it picks up the primary part file.
func (a *Audit) rotateLocked() error {
	if err := a.f.Close(); err != nil {
		return fmt.Errorf("close current: %w", err)
	}
	a.f = nil

	for i := a.maxFiles; i >= 1; i-- {
		oldName := a.partPath + "." + strconv.Itoa(i)
		if _, err := os.Stat(oldName); err != nil {
			continue
		}
		if i == a.maxFiles {
			_ = os.Remove(oldName)
			continue
		}
		newName := a.partPath + "." + strconv.Itoa(i+1)
		if err := os.Rename(oldName, newName); err != nil {
			return fmt.Errorf("rename %s → %s: %w", oldName, newName, err)
		}
	}
	if err := os.Rename(a.partPath, a.partPath+".1"); err != nil {
		return fmt.Errorf("rename current → .1: %w", err)
	}

	f, err := os.OpenFile(a.partPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("reopen: %w", err)
	}
	a.f = f
	a.curBytes = 0
	return nil
}

// RotatedFiles returns the absolute paths of every audit-log file the project
// has, oldest-first so callers can iterate chronologically. Includes:
//
//   - Rotated siblings of the consolidated log (audit.log.1, audit.log.2, ...)
//   - The consolidated audit.log itself
//   - Every per-process part file under audit.parts/ (audit.<t>.<pid>.log
//     and any rotated siblings .log.N)
//
// Parts are sorted by filename, which (by the partFilename format) sorts
// chronologically by start-time. This lets readers like `nimblegate status`
// and `nimblegate audit analyze` see the full picture without needing
// compaction to have run.
func RotatedFiles(currentPath string) []string {
	dir := filepath.Dir(currentPath)
	base := filepath.Base(currentPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{currentPath}
	}
	prefix := base + "."
	var rotated []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		// Only accept .<number> suffixes - not .gz or other variants.
		if _, err := strconv.Atoi(strings.TrimPrefix(name, prefix)); err != nil {
			continue
		}
		rotated = append(rotated, filepath.Join(dir, name))
	}
	// Sort by trailing number descending (so .5 comes before .1), so when
	// we append the current at the end, the result is oldest → newest.
	sort.Slice(rotated, func(i, j int) bool {
		ni, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(rotated[i]), prefix))
		nj, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(rotated[j]), prefix))
		return ni > nj
	})
	if _, err := os.Stat(currentPath); err == nil {
		rotated = append(rotated, currentPath)
	}

	// Append per-process part files under audit.parts/. Sorted by
	// filename, which by partFilename() format is chronological.
	partsDir := filepath.Join(dir, AuditPartsDirName)
	partEntries, err := os.ReadDir(partsDir)
	if err == nil {
		var parts []string
		for _, e := range partEntries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "audit.") {
				continue
			}
			parts = append(parts, filepath.Join(partsDir, name))
		}
		sort.Strings(parts)
		rotated = append(rotated, parts...)
	}

	if len(rotated) == 0 {
		return []string{currentPath}
	}
	return rotated
}

// envOrDefaultInt64 reads an int64 from env (or returns dflt if unset / invalid).
func envOrDefaultInt64(key string, dflt int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return dflt
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return dflt
	}
	return n
}
