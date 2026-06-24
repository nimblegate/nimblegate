// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package whitelist

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStats_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)

	// Empty load - file doesn't exist yet.
	sf, err := LoadStats(tmp)
	if err != nil {
		t.Fatalf("initial LoadStats: %v", err)
	}
	if len(sf.Entries) != 0 {
		t.Errorf("expected 0 entries on fresh load, got %d", len(sf.Entries))
	}

	// Hand-build a fake Whitelist with one matched entry.
	w := &Whitelist{
		entries: []*resolved{
			{
				raw:     Entry{Frame: "git-safety/no-force-push-main", Path: "**", Reason: "test"},
				matched: 3,
			},
		},
	}

	sf.Merge(w, now)
	if len(sf.Entries) != 1 {
		t.Fatalf("expected 1 entry after merge, got %d", len(sf.Entries))
	}
	if sf.Entries[0].MatchedTotal != 3 {
		t.Errorf("MatchedTotal: got %d, want 3", sf.Entries[0].MatchedTotal)
	}
	if !sf.Entries[0].FirstMatched.Equal(now) {
		t.Errorf("FirstMatched: got %v, want %v", sf.Entries[0].FirstMatched, now)
	}

	// Save + reload.
	if err := sf.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	sf2, err := LoadStats(tmp)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(sf2.Entries) != 1 || sf2.Entries[0].MatchedTotal != 3 {
		t.Errorf("after reload: got %+v", sf2.Entries)
	}
}

func TestStats_MergeAccumulates(t *testing.T) {
	tmp := t.TempDir()
	now1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	now2 := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)

	sf, _ := LoadStats(tmp)
	w := &Whitelist{
		entries: []*resolved{
			{
				raw:     Entry{Frame: "convention/html-placeholder-content", Path: "AGENTS_LEARNING.md", Reason: "doc"},
				matched: 2,
			},
		},
	}
	sf.Merge(w, now1)

	// Reset per-run counter; merge again with a later timestamp.
	w.entries[0].matched = 5
	sf.Merge(w, now2)

	if len(sf.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(sf.Entries))
	}
	got := sf.Entries[0]
	if got.MatchedTotal != 7 {
		t.Errorf("MatchedTotal: got %d, want 7 (2+5)", got.MatchedTotal)
	}
	if !got.FirstMatched.Equal(now1) {
		t.Errorf("FirstMatched should NOT update on subsequent merges, got %v", got.FirstMatched)
	}
	if !got.LastMatched.Equal(now2) {
		t.Errorf("LastMatched: got %v, want %v", got.LastMatched, now2)
	}
}

func TestStats_UnmatchedEntriesPreserved(t *testing.T) {
	tmp := t.TempDir()
	now1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	now2 := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)

	sf, _ := LoadStats(tmp)
	wOld := &Whitelist{
		entries: []*resolved{
			{raw: Entry{Frame: "old-frame", Path: "a.md", Reason: "x"}, matched: 4},
		},
	}
	sf.Merge(wOld, now1)
	_ = sf.Save(tmp)

	// New run: different whitelist content, doesn't match the old entry.
	sf2, _ := LoadStats(tmp)
	wNew := &Whitelist{
		entries: []*resolved{
			{raw: Entry{Frame: "new-frame", Path: "b.md", Reason: "y"}, matched: 1},
		},
	}
	sf2.Merge(wNew, now2)

	if len(sf2.Entries) != 2 {
		t.Fatalf("expected 2 entries (old preserved + new), got %d", len(sf2.Entries))
	}
}

func TestEntryStats_IsStale(t *testing.T) {
	now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		lastMatch time.Time
		staleDays int
		want      bool
	}{
		{"never matched", time.Time{}, 90, false},
		{"matched today", now, 90, false},
		{"matched 30 days ago", now.AddDate(0, 0, -30), 90, false},
		{"matched 91 days ago", now.AddDate(0, 0, -91), 90, true},
		{"custom threshold 7d", now.AddDate(0, 0, -10), 7, true},
		{"zero staleDays falls back to default", now.AddDate(0, 0, -100), 0, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			es := EntryStats{LastMatched: c.lastMatch}
			if got := es.IsStale(now, c.staleDays); got != c.want {
				t.Errorf("IsStale: got %v, want %v", got, c.want)
			}
		})
	}
}

func TestSave_AtomicViaRename(t *testing.T) {
	tmp := t.TempDir()
	sf := &StatsFile{
		Version: statsVersion,
		Entries: []EntryStats{{Frame: "x", Path: "y", MatchedTotal: 1}},
	}
	if err := sf.Save(tmp); err != nil {
		t.Fatalf("Save: %v", err)
	}
	final := filepath.Join(tmp, ".appframes", "_canonical", statsFilename)
	if _, err := os.Stat(final); err != nil {
		t.Errorf("final file missing: %v", err)
	}
	// .tmp must NOT remain after successful save.
	if _, err := os.Stat(final + ".tmp"); err == nil {
		t.Error(".tmp leftover after Save")
	}
}
