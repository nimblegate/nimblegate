// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package tasks

import (
	"testing"
	"time"
)

func openLedgerWith(k FindingKey, now time.Time) *Ledger {
	l, _ := Reconcile(NewLedger(), []Finding{{Key: k, Severity: "WARN"}}, now)
	return l
}

func TestReconcile_deferredStaysDeferredWhileFiring(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	if _, err := l.Defer(k.ID(), "next phase", nil, t0); err != nil {
		t.Fatal(err)
	}
	// finding still fires next run → stays deferred (does not nag the open list)
	l2, resolved := Reconcile(l, []Finding{{Key: k, Severity: "WARN"}}, t0.Add(time.Hour))
	tk := l2.Tasks[k.ID()]
	if tk.Status != StatusDeferred {
		t.Errorf("status = %s, want deferred", tk.Status)
	}
	if len(l2.OpenTasks()) != 0 {
		t.Errorf("deferred task must not appear in OpenTasks")
	}
	if len(l2.DeferredTasks()) != 1 {
		t.Errorf("deferred task should appear in DeferredTasks")
	}
	if len(resolved) != 0 {
		t.Errorf("still-firing deferred task must not resolve")
	}
}

func TestReconcile_deferredExpiresAndResurfaces(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	until := t0.Add(24 * time.Hour)
	if _, err := l.Defer(k.ID(), "later", &until, t0); err != nil {
		t.Fatal(err)
	}
	// past the until date + still firing → resurfaces to open
	l2, _ := Reconcile(l, []Finding{{Key: k, Severity: "WARN"}}, until.Add(time.Hour))
	tk := l2.Tasks[k.ID()]
	if tk.Status != StatusOpen {
		t.Errorf("expired defer should resurface to open, got %s", tk.Status)
	}
	if tk.DeferUntil != nil {
		t.Errorf("resurfaced task should clear DeferUntil")
	}
}

func TestReconcile_deferredResolvesWhenFixed(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	_, _ = l.Defer(k.ID(), "later", nil, t0)
	// finding gone (fixed even though deferred) → resolved
	l2, resolved := Reconcile(l, nil, t0.Add(time.Hour))
	if l2.Tasks[k.ID()].Status != StatusResolved {
		t.Errorf("deferred-then-fixed should resolve, got %s", l2.Tasks[k.ID()].Status)
	}
	if len(resolved) != 1 {
		t.Errorf("resolved list = %d, want 1", len(resolved))
	}
}

func TestLedger_LinkPersistsThroughResolve(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	if _, err := l.Link(k.ID(), "#42"); err != nil {
		t.Fatal(err)
	}
	if l.Tasks[k.ID()].PRRef != "#42" {
		t.Fatalf("link did not set PRRef")
	}
	// resolve (fixed) - PRRef must survive so "fixed in #42" stays visible
	l2, _ := Reconcile(l, nil, t0.Add(time.Hour))
	if l2.Tasks[k.ID()].PRRef != "#42" {
		t.Errorf("PRRef lost on resolve: %+v", l2.Tasks[k.ID()])
	}
}

func TestLedger_ResolveIDByPrefixAndAmbiguity(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	full := k.ID()
	// unambiguous prefix resolves
	if _, err := l.Link(full[:6], "#1"); err != nil {
		t.Errorf("prefix link should resolve: %v", err)
	}
	// unknown prefix errors
	if _, err := l.Link("zzzzzz", "#1"); err == nil {
		t.Errorf("unknown prefix should error")
	}
}

func TestLedger_DeferRejectsResolved(t *testing.T) {
	t0 := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	k := key("f/a", "x.js", "L")
	l := openLedgerWith(k, t0)
	l2, _ := Reconcile(l, nil, t0.Add(time.Hour)) // now resolved
	if _, err := l2.Defer(k.ID(), "x", nil, t0); err == nil {
		t.Errorf("deferring a resolved task should error")
	}
}
