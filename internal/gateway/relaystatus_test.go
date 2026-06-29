// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRelayStatus_writeReadRoundTrip(t *testing.T) {
	policyRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(policyRoot, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	want := RelayStatus{LastAttempt: now, LastSuccess: now, OK: true, DriftedRefs: 2}
	if err := WriteRelayStatus(policyRoot, "demo", want); err != nil {
		t.Fatalf("WriteRelayStatus: %v", err)
	}
	got, ok := ReadRelayStatus(policyRoot, "demo")
	if !ok {
		t.Fatal("ReadRelayStatus: want ok=true after write")
	}
	if !got.LastAttempt.Equal(want.LastAttempt) || !got.LastSuccess.Equal(want.LastSuccess) ||
		got.OK != want.OK || got.DriftedRefs != want.DriftedRefs {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, want)
	}
}

func TestRelayStatus_absentReturnsFalse(t *testing.T) {
	policyRoot := t.TempDir()
	if _, ok := ReadRelayStatus(policyRoot, "missing"); ok {
		t.Fatal("absent record should return ok=false")
	}
}
