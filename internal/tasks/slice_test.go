// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package tasks

import (
	"testing"
	"time"
)

func TestSliceState_RoundtripAndClear(t *testing.T) {
	root := t.TempDir()
	if st, _ := LoadSlice(root); st.Active() {
		t.Fatal("no slice should be active initially")
	}
	now := time.Date(2026, 5, 25, 9, 0, 0, 0, time.UTC)
	if err := (&SliceState{Name: "payments", StartedAt: now}).SaveSlice(root); err != nil {
		t.Fatal(err)
	}
	st, err := LoadSlice(root)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Active() || st.Name != "payments" || !st.StartedAt.Equal(now) {
		t.Fatalf("roundtrip failed: %+v", st)
	}
	if err := ClearSlice(root); err != nil {
		t.Fatal(err)
	}
	if st, _ := LoadSlice(root); st.Active() {
		t.Error("slice should be inactive after Clear")
	}
}

func TestLedger_OpenSince(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 8, 0, 0, 0, time.UTC)
	sliceStart := t0.Add(time.Hour)
	after := sliceStart.Add(time.Hour)

	l := NewLedger()
	// one finding from before the slice, one introduced during it
	l, _ = Reconcile(l, []Finding{{Key: key("f/old", "old.js", "L"), Severity: "WARN"}}, t0)
	l, _ = Reconcile(l, []Finding{
		{Key: key("f/old", "old.js", "L"), Severity: "WARN"},  // still firing
		{Key: key("f/new", "new.js", "L"), Severity: "BLOCK"}, // introduced during the slice
	}, after)

	since := l.OpenSince(sliceStart)
	if len(since) != 1 {
		t.Fatalf("OpenSince = %d, want 1 (only the finding first-seen during the slice): %+v", len(since), since)
	}
	if since[0].FrameID != "f/new" {
		t.Errorf("wrong task: %+v", since[0])
	}
}
