// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// seedLastBuild writes a marker file with the given identifier. Used by tests
// that exercise the change-detection path; bypasses currentBuildID() so the
// test doesn't depend on how the test binary itself was built.
func seedLastBuild(t *testing.T, root, id string) {
	t.Helper()
	path := filepath.Join(root, lastBuildFile)
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		t.Fatalf("seed last-build: %v", err)
	}
}

func readLastBuild(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, lastBuildFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func lastEvent(t *testing.T, root string) (gateway.Event, bool) {
	t.Helper()
	evs, err := gateway.ReadEvents(root, nil)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(evs) == 0 {
		return gateway.Event{}, false
	}
	return evs[len(evs)-1], true
}

// emitForTest mirrors emitBuildUpdateEventIfChanged but takes an explicit
// build ID instead of reading runtime/debug - lets tests exercise the
// transition logic deterministically without rebuilding the test binary.
func emitForTest(policyRoot, current string) bool {
	if policyRoot == "" || current == "" {
		return false
	}
	path := filepath.Join(policyRoot, lastBuildFile)
	prior, err := os.ReadFile(path)
	if err != nil {
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
		Payload:  map[string]any{"from": from, "to": to, "dirty": dirty},
		OK:       true,
	})
	_ = writeLastBuild(path, current)
	return true
}

func TestBuildEvent_firstRunSilent(t *testing.T) {
	root := t.TempDir()
	if emitted := emitForTest(root, "abc1234"); emitted {
		t.Errorf("first run should not emit an event: no transition to report")
	}
	if got := readLastBuild(t, root); got != "abc1234" {
		t.Errorf("first run should write marker; got %q want %q", got, "abc1234")
	}
	if _, ok := lastEvent(t, root); ok {
		t.Errorf("first run should not have appended any event")
	}
}

func TestBuildEvent_sameSHASilent(t *testing.T) {
	root := t.TempDir()
	seedLastBuild(t, root, "abc1234")
	if emitted := emitForTest(root, "abc1234"); emitted {
		t.Errorf("same SHA should not emit an event")
	}
	if _, ok := lastEvent(t, root); ok {
		t.Errorf("same SHA should not append any event")
	}
}

func TestBuildEvent_changedSHAEmits(t *testing.T) {
	root := t.TempDir()
	seedLastBuild(t, root, "abc1234")
	if emitted := emitForTest(root, "d9fe903"); !emitted {
		t.Fatalf("changed SHA should emit an event")
	}
	ev, ok := lastEvent(t, root)
	if !ok {
		t.Fatal("expected a build-update event after SHA change")
	}
	if ev.Event != "build-update" {
		t.Errorf("event = %q, want build-update", ev.Event)
	}
	if ev.Payload["from"] != "abc1234" {
		t.Errorf("from = %v, want abc1234", ev.Payload["from"])
	}
	if ev.Payload["to"] != "d9fe903" {
		t.Errorf("to = %v, want d9fe903", ev.Payload["to"])
	}
	if dirty, _ := ev.Payload["dirty"].(bool); dirty {
		t.Errorf("dirty flag should be false for clean→clean transition")
	}
	if got := readLastBuild(t, root); got != "d9fe903" {
		t.Errorf("marker should now hold the new SHA; got %q", got)
	}
}

func TestBuildEvent_dirtyToggleEmits(t *testing.T) {
	root := t.TempDir()
	seedLastBuild(t, root, "d9fe903")
	if emitted := emitForTest(root, "d9fe903-dirty"); !emitted {
		t.Fatalf("clean→dirty should emit even with same underlying SHA")
	}
	ev, ok := lastEvent(t, root)
	if !ok {
		t.Fatal("expected a build-update event after dirty toggle")
	}
	if ev.Payload["from"] != "d9fe903" || ev.Payload["to"] != "d9fe903" {
		t.Errorf("from/to should both be d9fe903 (dirty marker is separate); got from=%v to=%v",
			ev.Payload["from"], ev.Payload["to"])
	}
	dirty, _ := ev.Payload["dirty"].(bool)
	if !dirty {
		t.Errorf("dirty flag should be true on clean→dirty transition")
	}
}

func TestFormatEventPayload_buildUpdate(t *testing.T) {
	clean := formatEventPayload("build-update", map[string]any{
		"from": "abc1234", "to": "d9fe903", "dirty": false,
	})
	if want := "build abc1234 → build d9fe903"; clean != want {
		t.Errorf("clean format = %q, want %q", clean, want)
	}
	dirty := formatEventPayload("build-update", map[string]any{
		"from": "abc1234", "to": "d9fe903", "dirty": true,
	})
	if want := "build abc1234 → build d9fe903 (dirty)"; dirty != want {
		t.Errorf("dirty format = %q, want %q", dirty, want)
	}
}
