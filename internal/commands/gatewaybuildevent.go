// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"nimblegate/internal/gateway"
)

// lastBuildFile is the on-disk marker the dashboard uses to detect "did the
// binary change since last start?" - sits next to _events.jsonl in policy root
// so a backup of one captures the other.
const lastBuildFile = "_last_build"

// currentBuildID returns the running binary's identifier in the form
// "<sha>" or "<sha>-dirty". Returns "" if the binary has no VCS info baked in
// (e.g. built outside a git repo) - callers must skip the change-detection
// path in that case since they can't distinguish "fresh build" from "no info."
func currentBuildID() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var sha string
	var dirty bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) >= 7 {
				sha = s.Value[:7]
			} else {
				sha = s.Value
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if sha == "" {
		return ""
	}
	if dirty {
		return sha + "-dirty"
	}
	return sha
}

// splitBuildID returns (sha, dirty) from "<sha>" or "<sha>-dirty".
func splitBuildID(id string) (string, bool) {
	if strings.HasSuffix(id, "-dirty") {
		return strings.TrimSuffix(id, "-dirty"), true
	}
	return id, false
}

// emitBuildUpdateEventIfChanged compares the running binary's build ID against
// the marker at <policyRoot>/_last_build. First-run (file missing) writes the
// marker silently - a stranger's first start of the dashboard shouldn't
// manufacture a "build update" event for a transition that didn't happen. A
// mismatch emits one build-update event with payload {from, to, dirty} then
// writes the new marker. Same-ID start is a no-op. Errors are swallowed
// (telemetry is best-effort; dashboard startup must not fail because the
// marker file got corrupted).
//
// Returns true when an event was emitted - exposed for tests.
func emitBuildUpdateEventIfChanged(policyRoot string) bool {
	if policyRoot == "" {
		return false
	}
	current := currentBuildID()
	if current == "" {
		return false
	}
	path := filepath.Join(policyRoot, lastBuildFile)
	prior, err := os.ReadFile(path)
	if err != nil {
		// First run (or unreadable). Write the marker silently and return -
		// we have nothing to report a transition FROM.
		_ = writeLastBuild(path, current)
		return false
	}
	priorID := strings.TrimSpace(string(prior))
	if priorID == current {
		return false
	}
	from, _ := splitBuildID(priorID)
	to, dirty := splitBuildID(current)
	_ = gateway.AppendEvent(policyRoot, gateway.Event{
		Event:    "build-update",
		Operator: "system",
		Payload: map[string]any{
			"from":  from,
			"to":    to,
			"dirty": dirty,
		},
		OK: true,
	})
	_ = writeLastBuild(path, current)
	return true
}

// writeLastBuild persists the marker atomically (temp + rename) so a crash
// mid-write can't leave a half-written ID that looks like a different SHA.
func writeLastBuild(path, id string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(id+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
