// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const unboundFrameMsg = "project frames can only override stdlib frame metadata/severity; documentation/unbound has no check"

// frameProject writes a project with the given [frames] enabled list and an
// optional project frame at .appframes/documentation/<name>.md.
func frameProject(t *testing.T, enabled []string, frameName, triggers string) string {
	t.Helper()
	dir := t.TempDir()
	quoted := make([]string, len(enabled))
	for i, e := range enabled {
		quoted[i] = `"` + e + `"`
	}
	cfg := "[project]\nname = \"test\"\n\n[frames]\nenabled = [" + strings.Join(quoted, ", ") + "]\n"
	if err := os.WriteFile(filepath.Join(dir, "appframes.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if frameName != "" {
		fd := filepath.Join(dir, ".appframes", "documentation")
		if err := os.MkdirAll(fd, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "---\nname: " + frameName + "\ncategory: documentation\nsubcategory: notes\nseverity: BLOCK\ntriggers: " + triggers + "\n---\nmarkdown only\n"
		if err := os.WriteFile(filepath.Join(fd, frameName+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runIn(t *testing.T, bin, dir string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

func TestFrameConfigCLI(t *testing.T) {
	bin := buildBinary(t)

	t.Run("lint names markdown-only project frame", func(t *testing.T) {
		dir := frameProject(t, []string{"documentation/unbound"}, "unbound", "[cli]")
		_, out := runIn(t, bin, dir, "lint")
		if !strings.Contains(out, unboundFrameMsg) {
			t.Errorf("lint output missing %q:\n%s", unboundFrameMsg, out)
		}
	})

	t.Run("check warns once on markdown-only project frame", func(t *testing.T) {
		dir := frameProject(t, []string{"documentation/unbound", "git/folder-branch-lock"}, "unbound", "[cli]")
		code, out := runIn(t, bin, dir, "check")
		if code != 0 {
			t.Errorf("check exit = %d, want 0 (WARN, not ERROR):\n%s", code, out)
		}
		if strings.Count(out, unboundFrameMsg) != 1 {
			t.Errorf("want the message exactly once:\n%s", out)
		}
		if strings.Contains(out, "frame error") {
			t.Errorf("unbound project frame should not report a frame error:\n%s", out)
		}
	})

	t.Run("lint rejects unknown trigger", func(t *testing.T) {
		dir := frameProject(t, []string{"git/folder-branch-lock"}, "badtrig", "[cli, commit]")
		code, out := runIn(t, bin, dir, "lint")
		if code != 1 || !strings.Contains(out, `unknown trigger "commit"`) {
			t.Errorf("lint exit = %d, want 1 with unknown trigger message:\n%s", code, out)
		}
	})

	t.Run("enable accepts project frame id", func(t *testing.T) {
		dir := frameProject(t, []string{"git/folder-branch-lock"}, "unbound", "[cli]")
		code, out := runIn(t, bin, dir, "enable", "documentation/unbound")
		if code != 0 {
			t.Fatalf("enable exit = %d:\n%s", code, out)
		}
		cfg, _ := os.ReadFile(filepath.Join(dir, "appframes.toml"))
		if !strings.Contains(string(cfg), `"documentation/unbound"`) {
			t.Errorf("config missing enabled project frame:\n%s", cfg)
		}
	})

	t.Run("enable refuses wildcards and groups", func(t *testing.T) {
		dir := frameProject(t, []string{"git/folder-branch-lock"}, "", "")
		for _, target := range []string{"security/*", "*", "@cloudflare"} {
			code, out := runIn(t, bin, dir, "enable", target)
			if code != 2 || !strings.Contains(out, "uses removed syntax") || !strings.Contains(out, "nimblegate kits apply") {
				t.Errorf("enable %s: exit = %d, want 2 with removed-syntax guidance:\n%s", target, code, out)
			}
		}
		cfg, _ := os.ReadFile(filepath.Join(dir, "appframes.toml"))
		if strings.Contains(string(cfg), "*") || strings.Contains(string(cfg), "@") {
			t.Errorf("refused target written to config:\n%s", cfg)
		}
	})

	t.Run("disable still removes a legacy wildcard", func(t *testing.T) {
		dir := frameProject(t, []string{"git/*", "security/no-hardcoded-credentials"}, "", "")
		code, out := runIn(t, bin, dir, "disable", "git/*")
		if code != 0 {
			t.Fatalf("disable exit = %d:\n%s", code, out)
		}
		cfg, _ := os.ReadFile(filepath.Join(dir, "appframes.toml"))
		if strings.Contains(string(cfg), "git/*") {
			t.Errorf("wildcard not removed:\n%s", cfg)
		}
	})

	t.Run("check expands wildcard with deprecation line", func(t *testing.T) {
		dir := frameProject(t, []string{"git/*"}, "", "")
		_, out := runIn(t, bin, dir, "check")
		if strings.Count(out, "Deprecated: [frames] enabled uses wildcard entries (git/*)") != 1 {
			t.Errorf("want one deprecation line:\n%s", out)
		}
		if strings.Contains(out, "no checks ran") {
			t.Errorf("wildcard should still expand:\n%s", out)
		}
	})

	t.Run("help does not list list --group", func(t *testing.T) {
		_, out := runIn(t, bin, t.TempDir(), "help")
		if strings.Contains(out, "--group") {
			t.Errorf("help still mentions --group:\n%s", out)
		}
	})
}

func TestKitsCreate_validatesFrameIDs(t *testing.T) {
	dir := frameProject(t, nil, "unbound", "[cli]")
	t.Chdir(dir)
	if rc := Kits([]string{"create", "--frames", "git/folder-branch-lock,no/such-frame", "mine"}); rc != 2 {
		t.Errorf("unknown id: rc = %d, want 2", rc)
	}
	cfg, _ := os.ReadFile("appframes.toml")
	if strings.Contains(string(cfg), "mine") {
		t.Errorf("kit written despite unknown id:\n%s", cfg)
	}
	if rc := Kits([]string{"create", "--frames", "git/folder-branch-lock, documentation/unbound", "mine"}); rc != 0 {
		t.Errorf("stdlib + project ids: rc = %d, want 0", rc)
	}
}
