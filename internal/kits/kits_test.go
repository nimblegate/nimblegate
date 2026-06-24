// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package kits

import (
	"testing"
)

func TestLoadStdlib_ReturnsAllBuiltinKits(t *testing.T) {
	ks, err := LoadStdlib()
	if err != nil {
		t.Fatalf("LoadStdlib() error: %v", err)
	}
	want := []string{"core", "web-app", "cf-pages-project", "cf-workers-project", "security-strict", "encoding-strict"}
	for _, name := range want {
		k, ok := ks.Get(name)
		if !ok {
			t.Errorf("kit %q missing from stdlib", name)
			continue
		}
		if k.Display == "" {
			t.Errorf("kit %q has empty Display name", name)
		}
		if len(k.Frames) == 0 {
			t.Errorf("kit %q has empty Frames list", name)
		}
	}
}

func TestLoadStdlib_CoreHas15Frames(t *testing.T) {
	ks, _ := LoadStdlib()
	core, _ := ks.Get("core")
	if len(core.Frames) != 15 {
		t.Errorf("core kit: got %d frames, want 15", len(core.Frames))
	}
}

func TestLoadStdlib_KitNamesAreUnique(t *testing.T) {
	ks, _ := LoadStdlib()
	seen := map[string]bool{}
	for _, k := range ks.All() {
		if seen[k.Name] {
			t.Errorf("duplicate kit name: %s", k.Name)
		}
		seen[k.Name] = true
	}
}
