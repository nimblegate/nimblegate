// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// RelayStatus is the persisted outcome of the most recent backstop reconcile
// attempt for one repo. It lets the dashboard and doctor surface a relay that
// silently stopped delivering (the gate accepted pushes the upstream never
// received) without any network call.
type RelayStatus struct {
	LastAttempt time.Time
	LastSuccess time.Time
	OK          bool
	Error       string // already redacted
	DriftedRefs int    // refs reconciled (re-pushed) on the last attempt; >0 means it was behind
}

// relayStatusPath is the per-repo status record path.
func relayStatusPath(policyRoot, repo string) string {
	return filepath.Join(policyRoot, repo, "relay-status.json")
}

// ReadRelayStatus reads the persisted relay status for repo. The bool is false
// when no record exists yet (the backstop has not run for this repo).
func ReadRelayStatus(policyRoot, repo string) (RelayStatus, bool) {
	b, err := os.ReadFile(relayStatusPath(policyRoot, repo))
	if err != nil {
		return RelayStatus{}, false
	}
	var s RelayStatus
	if err := json.Unmarshal(b, &s); err != nil {
		return RelayStatus{}, false
	}
	return s, true
}

// WriteRelayStatus atomically writes the relay status for repo (temp file +
// rename). Mode 0640 because Error can carry a redacted upstream error string,
// so world read stays off - but the writer is the relay user and the readers
// (dashboard, doctor) are not, so owner-only made every status unreadable to
// the pages that exist to show it.
func WriteRelayStatus(policyRoot, repo string, s RelayStatus) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	path := relayStatusPath(policyRoot, repo)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o640); err != nil { // WriteFile's mode is umask-masked; this is not
		return err
	}
	return os.Rename(tmp, path)
}
