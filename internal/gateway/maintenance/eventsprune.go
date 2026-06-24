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

// eventsFileName is the gateway-wide structured event log (gateway.EventsFile).
// Hardcoded here to avoid importing the gateway package (cycle: gateway has no
// dep on maintenance today, but commands wires both - keep maintenance leaf).
const eventsFileName = "_events.jsonl"

// EventsPruneResult is the outcome of the events retention task.
type EventsPruneResult struct {
	Scanned         int
	Kept            int
	Pruned          int
	KeptUnparseable int
	Err             error
	Took            time.Time
}

// runEventsPrune rewrites <policyRoot>/_events.jsonl, dropping lines whose ts is
// older than retention. Unparseable lines are KEPT. Atomic temp+rename. Missing
// or empty file is a clean no-op. retention must be > 0.
func runEventsPrune(now func() time.Time, policyRoot string, retention time.Duration) EventsPruneResult {
	res := EventsPruneResult{Took: now()}
	path := filepath.Join(policyRoot, eventsFileName)
	st, err := os.Stat(path)
	if err != nil || st.Size() == 0 {
		return res
	}
	cutoff := now().Add(-retention)
	in, err := os.Open(path)
	if err != nil {
		res.Err = fmt.Errorf("open %s: %w", path, err)
		return res
	}
	defer in.Close()

	tmp, err := os.CreateTemp(policyRoot, ".events-rewrite-*")
	if err != nil {
		res.Err = fmt.Errorf("create tmp: %w", err)
		return res
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o644); err != nil {
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
			TS time.Time `json:"ts"`
		}
		keep := true
		if err := json.Unmarshal(line, &head); err != nil {
			res.KeptUnparseable++
		} else if head.TS.Before(cutoff) {
			keep = false
			res.Pruned++
		} else {
			res.Kept++
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
