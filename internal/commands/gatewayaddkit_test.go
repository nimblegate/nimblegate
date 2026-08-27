package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/kits"
)

// applyStarterKit was reachable only from the dashboard's add handler, so the
// CLI could not narrow a repo to a kit at all. Note what a kit does: an empty
// [frames] enabled runs every stdlib frame (engine.go isFrameEnabled), so
// writing a kit's IDs into the list REDUCES coverage to that kit. This test
// pins the write itself, not a claim about which set is safer.
func TestApplyStarterKitCoreEnablesFrames(t *testing.T) {
	root := t.TempDir()
	if err := applyStarterKit(root, "demo", "core", false); err != nil {
		t.Fatalf("applyStarterKit: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, "demo", "appframes.toml"))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	ks, err := kits.LoadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	core, ok := ks.Get("core")
	if !ok {
		t.Fatal(`stdlib has no "core" kit`)
	}
	for _, id := range core.Frames {
		if !strings.Contains(string(b), id) {
			t.Errorf("core frame %q not enabled in the written config", id)
		}
	}
}

// The --kit flag validates against this list before AddRepo runs, so an
// unknown name fails loudly instead of silently registering an ungated repo.
func TestKitNamesCoversStdlib(t *testing.T) {
	ks, err := kits.LoadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	names := kitNames(ks)
	if len(names) != len(ks.All()) {
		t.Fatalf("kitNames returned %d names, stdlib has %d kits", len(names), len(ks.All()))
	}
	var hasCore bool
	for _, n := range names {
		if n == "core" {
			hasCore = true
		}
	}
	if !hasCore {
		t.Errorf(`kitNames omits "core", the --kit default: %v`, names)
	}
}
