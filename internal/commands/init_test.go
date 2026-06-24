// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"strings"
	"testing"
)

func TestInitDefaultsToCoreKit(t *testing.T) {
	tmp := t.TempDir()
	rc := initAt(tmp, []string{})
	if rc != 0 {
		t.Fatalf("init returned %d, want 0", rc)
	}
	body, err := os.ReadFile(tmp + "/appframes.toml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		`"git/folder-branch-lock"`,
		`"security/no-hardcoded-credentials"`,
		`"database/schema-vs-code-drift"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config missing %s", want)
		}
	}
	if strings.Contains(s, "*") || strings.Contains(s, "@") {
		t.Errorf("config contains old-syntax markers: %s", s)
	}
	if !strings.Contains(s, `applied_kits = ["core"]`) {
		t.Errorf("config missing applied_kits = [\"core\"]; got: %s", s)
	}
}

func TestInitKitFlag(t *testing.T) {
	tmp := t.TempDir()
	rc := initAt(tmp, []string{"--kit", "web-app"})
	if rc != 0 {
		t.Fatalf("init returned %d, want 0", rc)
	}
	body, _ := os.ReadFile(tmp + "/appframes.toml")
	s := string(body)
	if !strings.Contains(s, `"web/html-img-alt"`) {
		t.Errorf("web-app kit did not include html-img-alt; got: %s", s)
	}
	if !strings.Contains(s, `applied_kits = ["web-app"]`) {
		t.Errorf("missing applied_kits = [\"web-app\"]")
	}
}

func TestInitKitNone(t *testing.T) {
	tmp := t.TempDir()
	rc := initAt(tmp, []string{"--kit", "none"})
	if rc != 0 {
		t.Fatal(rc)
	}
	body, _ := os.ReadFile(tmp + "/appframes.toml")
	s := string(body)
	if !strings.Contains(s, `enabled = []`) {
		t.Errorf("--kit none should write empty enabled; got: %s", s)
	}
}

func TestInitKitUnknown(t *testing.T) {
	tmp := t.TempDir()
	rc := initAt(tmp, []string{"--kit", "nonsense"})
	if rc == 0 {
		t.Error("unknown kit should return non-zero")
	}
}
