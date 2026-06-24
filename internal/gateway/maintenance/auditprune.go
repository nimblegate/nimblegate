// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuditPruneResult is one repo's outcome from the audit retention task.
type AuditPruneResult struct {
	Repo            string
	Scanned         int
	KeptAccept      int
	KeptReject      int
	KeptUnparseable int
	PrunedAccept    int
	PrunedReject    int
	Err             error
	Took            time.Time
}

// runAuditPrune walks <policyRoot>/<repo>/audit.log and rewrites each to drop
// records by a DECISION-AWARE rule:
//
//   - accept records (accept==true && observed==false) older than acceptRetention -> pruned
//   - reject/observed records -> kept, unless rejectRetention > 0 and older than it
//   - unparseable lines -> ALWAYS kept (an unreadable audit line is evidence)
//
// Atomic rewrite via temp file + rename, mirroring pruneOneDeadletter. Missing
// files are no-ops. acceptRetention must be > 0; rejectRetention == 0 means
// keep rejects forever.
func runAuditPrune(now func() time.Time, policyRoot string, acceptRetention, rejectRetention time.Duration) []AuditPruneResult {
	entries, err := os.ReadDir(policyRoot)
	if err != nil {
		return nil
	}
	acceptCutoff := now().Add(-acceptRetention)
	var rejectCutoff time.Time
	if rejectRetention > 0 {
		rejectCutoff = now().Add(-rejectRetention)
	}
	var out []AuditPruneResult
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if len(name) > 0 && name[0] == '_' {
			continue
		}
		path := filepath.Join(policyRoot, name, "audit.log")
		st, err := os.Stat(path)
		if err != nil || st.Size() == 0 {
			continue
		}
		out = append(out, pruneOneAudit(now, name, path, acceptCutoff, rejectCutoff))
	}
	return out
}

func pruneOneAudit(now func() time.Time, repo, path string, acceptCutoff, rejectCutoff time.Time) AuditPruneResult {
	res := AuditPruneResult{Repo: repo, Took: now()}
	in, err := os.Open(path)
	if err != nil {
		res.Err = fmt.Errorf("open %s: %w", path, err)
		return res
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".audit-rewrite-*")
	if err != nil {
		res.Err = fmt.Errorf("create tmp: %w", err)
		return res
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		res.Err = fmt.Errorf("chmod tmp: %w", err)
		return res
	}

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	w := bufio.NewWriter(tmp)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		res.Scanned++
		var head struct {
			Time     time.Time `json:"time"`
			Accept   bool      `json:"accept"`
			Observed bool      `json:"observed"`
		}
		keep := true
		if err := json.Unmarshal(line, &head); err != nil {
			res.KeptUnparseable++ // never drop an unreadable line
		} else if head.Accept && !head.Observed {
			if head.Time.Before(acceptCutoff) {
				keep = false
				res.PrunedAccept++
			} else {
				res.KeptAccept++
			}
		} else {
			if !rejectCutoff.IsZero() && head.Time.Before(rejectCutoff) {
				keep = false
				res.PrunedReject++
			} else {
				res.KeptReject++
			}
		}
		if !keep {
			continue
		}
		if _, err := w.Write(line); err != nil {
			res.Err, _ = err, tmp.Close()
			return res
		}
		if err := w.WriteByte('\n'); err != nil {
			res.Err, _ = err, tmp.Close()
			return res
		}
	}
	if err := sc.Err(); err != nil {
		res.Err, _ = fmt.Errorf("scan: %w", err), tmp.Close()
		return res
	}
	if err := w.Flush(); err != nil {
		res.Err, _ = err, tmp.Close()
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
