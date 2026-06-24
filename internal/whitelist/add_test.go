// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package whitelist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAddEntryCreatesAppendsDedups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wl", "_canonical", "whitelist.toml")

	added, err := AddEntry(path, Entry{Frame: "security/no-private-keys-in-repo", Path: "a_test.go", Reason: "fixture"})
	if err != nil || !added {
		t.Fatalf("first add: added=%v err=%v", added, err)
	}
	// Appending a different entry keeps the first.
	if _, err := AddEntry(path, Entry{Frame: "command-safety/curl-pipe-shell", Path: "install.sh", Reason: "bootstrap"}); err != nil {
		t.Fatal(err)
	}
	// Duplicate (same frame+path) is a no-op.
	added, err = AddEntry(path, Entry{Frame: "security/no-private-keys-in-repo", Path: "a_test.go", Reason: "dup"})
	if err != nil || added {
		t.Errorf("dup add: added=%v err=%v, want added=false", added, err)
	}

	// File round-trips through Load (both entries present).
	known := map[string]bool{"security/no-private-keys-in-repo": true, "command-safety/curl-pipe-shell": true}
	wl, err := Load(path, known, time.Now().UTC())
	if err != nil {
		t.Fatalf("Load after add: %v", err)
	}
	if !wl.Match("security/no-private-keys-in-repo", "a_test.go", "") {
		t.Error("first entry should match after add+reload")
	}

	// Validation: empty reason / frame → error, no write.
	if _, err := AddEntry(filepath.Join(dir, "x.toml"), Entry{Frame: "x/y", Path: "z"}); err == nil {
		t.Error("empty reason must error")
	}
}

func TestAddEntryPreservesComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "whitelist.toml")
	if err := os.WriteFile(path, []byte("# header comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddEntry(path, Entry{Frame: "f/g", Path: "p", Reason: "r"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "# header comment") {
		t.Error("existing comment must be preserved")
	}
}
