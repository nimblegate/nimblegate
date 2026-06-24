// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAppendEvent_writesAndRoundTrips(t *testing.T) {
	root := t.TempDir()
	want := Event{
		Timestamp: time.Date(2026, 5, 30, 14, 35, 2, 0, time.UTC),
		Event:     "archive",
		Repo:      "myapp",
		Operator:  "local",
		Payload:   map[string]any{"k": "v"},
		OK:        true,
	}
	if err := AppendEvent(root, want); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	got, err := ReadEvents(root, func(Event) bool { return true })
	if err != nil || len(got) != 1 {
		t.Fatalf("ReadEvents: got=%v err=%v", got, err)
	}
	if got[0].Event != want.Event || got[0].Repo != want.Repo || got[0].Operator != want.Operator || !got[0].OK {
		t.Fatalf("event mismatch: got=%+v want=%+v", got[0], want)
	}
}

func TestAppendEvent_defaultsTimestampAndOperator(t *testing.T) {
	root := t.TempDir()
	before := time.Now().UTC()
	if err := AppendEvent(root, Event{Event: "add", Repo: "x", OK: true}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadEvents(root, func(Event) bool { return true })
	if got[0].Operator != "local" {
		t.Fatalf("operator default: got %q want local", got[0].Operator)
	}
	if got[0].Timestamp.Before(before) || got[0].Timestamp.After(time.Now().UTC().Add(time.Second)) {
		t.Fatalf("timestamp default: got %v", got[0].Timestamp)
	}
	if got[0].Payload == nil {
		t.Fatalf("payload default: nil")
	}
}

func TestAppendEvent_appendsNotOverwrites(t *testing.T) {
	root := t.TempDir()
	_ = AppendEvent(root, Event{Event: "add", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "archive", Repo: "a", OK: true})
	got, _ := ReadEvents(root, func(Event) bool { return true })
	if len(got) != 2 || got[0].Event != "add" || got[1].Event != "archive" {
		t.Fatalf("events: %+v", got)
	}
}

func TestReadEvents_missingFileReturnsNil(t *testing.T) {
	root := t.TempDir()
	got, err := ReadEvents(root, func(Event) bool { return true })
	if err != nil || got != nil {
		t.Fatalf("missing file: got=%v err=%v", got, err)
	}
}

func TestReadEvents_filterPredicate(t *testing.T) {
	root := t.TempDir()
	_ = AppendEvent(root, Event{Event: "add", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "archive", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "restore", Repo: "a", OK: true})
	got, _ := ReadEvents(root, func(e Event) bool {
		return e.Event == "archive" || e.Event == "restore"
	})
	if len(got) != 2 {
		t.Fatalf("filter: want 2 got %d", len(got))
	}
}

func TestAppendEvent_fileIsJSONL(t *testing.T) {
	root := t.TempDir()
	_ = AppendEvent(root, Event{Event: "add", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "archive", Repo: "a", OK: true})
	b, err := os.ReadFile(filepath.Join(root, EventsFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines got %d: %q", len(lines), b)
	}
}
