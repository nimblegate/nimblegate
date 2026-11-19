// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/config"
)

const uiPolicy = `[frames]
enabled = ["security/no-hardcoded-credentials"]

[ui]
applied_kits = ["core"]
seen_frames = ["security/no-hardcoded-credentials"]

[[ui.user_kits]]
name = "mine"
frames = [
    "security/no-hardcoded-credentials",
]
`

func writeUIPolicy(t *testing.T) (root, path string) {
	t.Helper()
	root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(root, "r", "appframes.toml")
	if err := os.WriteFile(path, []byte(uiPolicy), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, path
}

func assertUIKept(t *testing.T, path string) {
	t.Helper()
	b, _ := os.ReadFile(path)
	for _, want := range []string{`applied_kits = ["core"]`, `seen_frames = ["security/no-hardcoded-credentials"]`, "[[ui.user_kits]]", `name = "mine"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("lost %q; file now:\n%s", want, b)
		}
	}
}

func TestWritePolicyTOML_KeepsUISection(t *testing.T) {
	t.Run("severity", func(t *testing.T) {
		root, path := writeUIPolicy(t)
		fp, err := LoadFramePolicy(root, "r")
		if err != nil {
			t.Fatal(err)
		}
		if err := fp.WithSeverity("security/no-hardcoded-credentials", "WARN").Save(root, "r"); err != nil {
			t.Fatal(err)
		}
		assertUIKept(t, path)
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), `severity = "WARN"`) {
			t.Errorf("severity not written:\n%s", b)
		}
		// A second save must not duplicate the kept tables.
		fp, _ = LoadFramePolicy(root, "r")
		if err := fp.Save(root, "r"); err != nil {
			t.Fatal(err)
		}
		b, _ = os.ReadFile(path)
		if n := strings.Count(string(b), "[ui]"); n != 1 {
			t.Errorf("[ui] appears %d times after two saves:\n%s", n, b)
		}
	})
	t.Run("time estimates", func(t *testing.T) {
		root, path := writeUIPolicy(t)
		h := 2.0
		if err := SaveTimeEstimates(root, "r", config.TimeEstimates{Tier1: &h}); err != nil {
			t.Fatal(err)
		}
		assertUIKept(t, path)
	})
	t.Run("linters", func(t *testing.T) {
		root, path := writeUIPolicy(t)
		lp, err := LoadLinterPolicy(root, "r")
		if err != nil {
			t.Fatal(err)
		}
		if err := lp.Save(root, "r"); err != nil {
			t.Fatal(err)
		}
		assertUIKept(t, path)
	})
}

func TestForeignTOMLBlocks(t *testing.T) {
	doc := "[frames]\nenabled = []\n\n[frames.\"a/b\"]\nseverity = \"WARN\"\n\n[ui]\napplied_kits = []\n\n[[ui.user_kits]]\nname = \"x\"\n"
	got := ForeignTOMLBlocks(doc, func(n string) bool { return n == "frames" || strings.HasPrefix(n, "frames.") })
	if strings.Contains(got, "frames") || !strings.Contains(got, "[ui]") || !strings.Contains(got, "[[ui.user_kits]]") {
		t.Errorf("ForeignTOMLBlocks = %q", got)
	}
}
