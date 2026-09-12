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

// PushRelay is a repo's most recent live relay outcome, as post-receive
// recorded it.
type PushRelay struct {
	OK        bool
	At        time.Time
	HeadsOnly bool // every ref was a branch update the backstop would re-send
}

// LastPushRelays returns each repo's most recent live relay outcome. Events
// are chronological, so the last one per repo wins; an unreadable events file
// yields an empty map, the same as no pushes.
func LastPushRelays(policyRoot string) map[string]PushRelay {
	m := map[string]PushRelay{}
	evs, err := ReadEvents(policyRoot, func(e Event) bool {
		return e.Event == "relay-ok" || e.Event == "relay-failed"
	})
	if err != nil {
		return m
	}
	for _, e := range evs {
		heads, _ := e.Payload["heads_only"].(bool)
		m[e.Repo] = PushRelay{OK: e.Event == "relay-ok", At: e.Timestamp, HeadsOnly: heads}
	}
	return m
}

// Recovered reports whether a failed push has since reached the upstream
// another way. The backstop compares every branch head against the upstream
// and re-sends any that differ, so a clean pass after the failure means the
// upstream now holds what a branch-only push carried. A failure recorded
// before pushes carried that flag stays failed until the next push.
func (p PushRelay) Recovered(rs RelayStatus, haveStatus bool) bool {
	return !p.OK && p.HeadsOnly && haveStatus && rs.OK && rs.LastSuccess.After(p.At)
}
