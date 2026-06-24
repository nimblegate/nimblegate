// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package tasks

import (
	"path/filepath"
	"testing"
	"time"

	"nimblegate/internal/engine"
)

func key(frame, file, label string) FindingKey {
	return FindingKey{FrameID: frame, File: file, Label: label}
}

func TestFindingKey_IDStableAndDistinct(t *testing.T) {
	a := key("security/x", "a.js", "innerHTML").ID()
	b := key("security/x", "a.js", "innerHTML").ID()
	c := key("security/x", "b.js", "innerHTML").ID()
	if a != b {
		t.Errorf("same key produced different IDs: %s vs %s", a, b)
	}
	if a == c {
		t.Errorf("different keys produced same ID: %s", a)
	}
	if len(a) == 0 {
		t.Error("ID is empty")
	}
}

func TestReconcile_newFindingOpens(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	next, resolved := Reconcile(NewLedger(), []Finding{{Key: key("f/a", "x.js", "L"), Severity: "WARN"}}, now)
	if len(resolved) != 0 {
		t.Fatalf("no prior tasks → nothing resolved, got %d", len(resolved))
	}
	tk := next.Tasks[key("f/a", "x.js", "L").ID()]
	if tk == nil || tk.Status != StatusOpen || tk.RunsSeen != 1 || !tk.FirstSeen.Equal(now) {
		t.Fatalf("new finding not opened correctly: %+v", tk)
	}
}

func TestReconcile_persistingFindingBumps(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	f := []Finding{{Key: key("f/a", "x.js", "L"), Severity: "WARN"}}
	l1, _ := Reconcile(NewLedger(), f, t0)
	l2, _ := Reconcile(l1, f, t1)
	tk := l2.Tasks[key("f/a", "x.js", "L").ID()]
	if tk.RunsSeen != 2 || !tk.FirstSeen.Equal(t0) || !tk.LastSeen.Equal(t1) || tk.Status != StatusOpen {
		t.Fatalf("persisting finding not bumped: %+v", tk)
	}
}

func TestReconcile_absentFindingResolves(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	l1, _ := Reconcile(NewLedger(), []Finding{{Key: key("f/a", "x.js", "L"), Severity: "WARN"}}, t0)
	l2, resolved := Reconcile(l1, nil, t1) // finding gone this run
	tk := l2.Tasks[key("f/a", "x.js", "L").ID()]
	if tk.Status != StatusResolved || tk.ResolvedAt == nil {
		t.Fatalf("absent finding not resolved: %+v", tk)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved list = %d, want 1", len(resolved))
	}
}

func TestReconcile_regressionReopens(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	f := []Finding{{Key: key("f/a", "x.js", "L"), Severity: "WARN"}}
	l1, _ := Reconcile(NewLedger(), f, t0)
	l2, _ := Reconcile(l1, nil, t0.Add(time.Hour)) // resolved
	l3, _ := Reconcile(l2, f, t0.Add(2*time.Hour)) // reappears
	tk := l3.Tasks[key("f/a", "x.js", "L").ID()]
	if tk.Status != StatusOpen || tk.ResolvedAt != nil {
		t.Fatalf("regression not reopened: %+v", tk)
	}
}

func TestReconcile_tracksLastSeenLineWithoutAffectingIdentity(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	k := key("f/a", "x.js", "L")
	l1, _ := Reconcile(NewLedger(), []Finding{{Key: k, Severity: "WARN", Line: 10}}, t0)
	if l1.Tasks[k.ID()].Line != 10 {
		t.Fatalf("Line = %d, want 10 (carried from finding)", l1.Tasks[k.ID()].Line)
	}
	// Same finding, moved to a new line (edit above it): still the SAME task,
	// line updated to last-seen, NOT resolved/reopened.
	l2, resolved := Reconcile(l1, []Finding{{Key: k, Severity: "WARN", Line: 42}}, t1)
	tk := l2.Tasks[k.ID()]
	if tk.Line != 42 {
		t.Errorf("Line = %d, want 42 (last-seen)", tk.Line)
	}
	if tk.RunsSeen != 2 || tk.Status != StatusOpen {
		t.Errorf("line change should bump the same task, got runs=%d status=%s", tk.RunsSeen, tk.Status)
	}
	if len(resolved) != 0 {
		t.Errorf("a line change must not resolve the task, got %d resolved", len(resolved))
	}
}

func TestKeysFromResults(t *testing.T) {
	root := "/proj"
	results := []engine.CheckResult{
		{
			FrameID: "security/no-innerHTML-user-input",
			Outcome: engine.OutcomeWarn,
			Hits: []engine.Hit{
				{File: filepath.Join(root, "a.js"), Line: 10, Label: "innerHTML"},
				{File: filepath.Join(root, "b.js"), Line: 2, Label: "innerHTML"},
			},
		},
		{FrameID: "convention/clean", Outcome: engine.OutcomePass}, // passing → no task
	}
	got := KeysFromResults(results, root)
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2: %+v", len(got), got)
	}
	if got[0].Key.File != "a.js" || got[0].Severity != "WARN" || got[0].Line != 10 {
		t.Errorf("finding 0 = %+v, want relative path + WARN + line 10", got[0])
	}
}

func TestLoadSave_roundtrip(t *testing.T) {
	root := t.TempDir()
	l, _ := Reconcile(NewLedger(), []Finding{{Key: key("f/a", "x.js", "L"), Severity: "BLOCK"}}, time.Now().UTC())
	if err := l.Save(root); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.OpenTasks()) != 1 {
		t.Fatalf("roundtrip lost tasks: %+v", got)
	}
}

func TestLoad_missingFileIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("missing ledger should not error: %v", err)
	}
	if len(got.OpenTasks()) != 0 {
		t.Errorf("empty ledger expected, got %d", len(got.OpenTasks()))
	}
}
