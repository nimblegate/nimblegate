// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// writeLocalConfig writes a flat appframes.toml directly into dir (not into a
// per-repo subdir). Used by CLI-level kit tests where the Kits command reads
// from CWD/appframes.toml rather than the per-repo gateway path.
func writeLocalConfig(t *testing.T, dir string, enabled, applied []string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("[frames]\nenabled = [\n")
	for _, id := range enabled {
		fmt.Fprintf(&b, "    %q,\n", id)
	}
	b.WriteString("]\n\n[ui]\napplied_kits = [")
	for i, name := range applied {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", name)
	}
	b.WriteString("]\n")
	if err := os.WriteFile(dir+"/appframes.toml", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readLocalEnabled reads the enabled list from dir/appframes.toml (flat path).
func readLocalEnabled(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(dir + "/appframes.toml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Frames struct {
			Enabled []string `toml:"enabled"`
		} `toml:"frames"`
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Frames.Enabled
}

func TestKits_ListSucceeds(t *testing.T) {
	tmp := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	if rc := Kits([]string{"list"}); rc != 0 {
		t.Errorf("kits list returned %d, want 0", rc)
	}
}

func TestKits_ApplyClearRoundtrip(t *testing.T) {
	tmp := t.TempDir()
	wd, _ := os.Getwd()
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	writeLocalConfig(t, ".", nil, nil)

	if rc := Kits([]string{"apply", "core"}); rc != 0 {
		t.Fatalf("apply core: rc=%d", rc)
	}
	enabled := readLocalEnabled(t, ".")
	if len(enabled) != 15 {
		t.Errorf("after apply: %d frames, want 15", len(enabled))
	}
	if rc := Kits([]string{"clear", "core"}); rc != 0 {
		t.Fatalf("clear core: rc=%d", rc)
	}
	enabled = readLocalEnabled(t, ".")
	if len(enabled) != 0 {
		t.Errorf("after clear: %d frames, want 0", len(enabled))
	}
}

func TestKits_UnknownSub(t *testing.T) {
	if rc := Kits([]string{"nope"}); rc == 0 {
		t.Error("unknown subcommand should return non-zero")
	}
}

func TestKits_ApplyUnknownKit(t *testing.T) {
	tmp := t.TempDir()
	wd, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(wd)
	writeLocalConfig(t, ".", nil, nil)

	if rc := Kits([]string{"apply", "nonsense"}); rc == 0 {
		t.Error("unknown kit should return non-zero")
	}
}
