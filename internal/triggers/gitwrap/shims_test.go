// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitwrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderShim_GitVerbCases(t *testing.T) {
	got := renderShim(shimSpec{
		Name:  "git",
		Route: "git",
		Verbs: []string{"push", "reset", "branch"},
	})
	for _, want := range []string{
		"#!/bin/sh",
		`exec nimblegate git "$@"`, // routes destructive verbs to nimblegate git
		"push|reset|branch",
		`exec git "$@"`, // fast path for non-destructive
		"SHIM_DIR=",
		"grep -vxF",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("git shim missing %q\nGot:\n%s", want, got)
		}
	}
	// Sanity: must NOT have the double-git bug (`nimblegate git git ...`).
	if strings.Contains(got, "nimblegate git git") {
		t.Errorf("git shim has doubled subcommand name; got:\n%s", got)
	}
}

func TestRenderShim_RmRecursiveOnly(t *testing.T) {
	got := renderShim(shimSpec{
		Name:        "rm",
		Route:       "cmd rm",
		RecursiveRm: true,
	})
	for _, want := range []string{
		"#!/bin/sh",
		`exec nimblegate cmd rm "$@"`,
		`-r|-R|--recursive`,
		`-rf`, // combined short-flag forms must be present
		`-Rf`,
		`exec rm "$@"`, // pass-through when no recursive flag
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rm shim missing %q\nGot:\n%s", want, got)
		}
	}
	// Sanity: should NOT contain verb-based case (rm doesn't use verbs).
	if strings.Contains(got, `case "$1"`) {
		t.Errorf("rm shim wrongly contains verb case statement; got:\n%s", got)
	}
}

func TestRenderShim_AptVerbCases(t *testing.T) {
	got := renderShim(shimSpec{
		Name:  "apt",
		Route: "cmd apt",
		Verbs: []string{"purge", "remove", "autoremove"},
	})
	for _, want := range []string{
		`exec nimblegate cmd apt "$@"`,
		"purge|remove|autoremove",
		`exec apt "$@"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("apt shim missing %q", want)
		}
	}
}

func TestRenderShim_AllStripPathFirst(t *testing.T) {
	// EVERY shim must strip its own dir from PATH before exec'ing
	// anything - otherwise destructive shim would recurse into itself.
	for _, spec := range shimSpecs {
		got := renderShim(spec)
		if !strings.Contains(got, "SHIM_DIR=") {
			t.Errorf("shim %q missing SHIM_DIR= header (path-strip logic)", spec.Name)
		}
		if !strings.Contains(got, `export PATH`) {
			t.Errorf("shim %q missing 'export PATH' after stripping shim dir", spec.Name)
		}
	}
}

func TestInstallShims_CreatesExecutableFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := InstallShims()
	if err != nil {
		t.Fatalf("InstallShims: %v", err)
	}
	expected := filepath.Join(tmp, ShimsDirName)
	if dir != expected {
		t.Errorf("dir = %q; want %q", dir, expected)
	}
	for _, name := range ShimNames() {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("shim %s not created: %v", name, err)
			continue
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("shim %s not executable; mode = %v", name, info.Mode())
		}
		data, _ := os.ReadFile(path)
		if !strings.HasPrefix(string(data), "#!/bin/sh\n") {
			t.Errorf("shim %s missing #!/bin/sh; first bytes: %q", name, string(data[:32]))
		}
	}
}

func TestInstallShims_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if _, err := InstallShims(); err != nil {
		t.Fatal(err)
	}
	// Re-run; should succeed (overwrite existing files).
	if _, err := InstallShims(); err != nil {
		t.Errorf("second InstallShims: %v", err)
	}
}

func TestUninstallShims_RemovesAllShims(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := InstallShims()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range ShimNames() {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("setup: shim %s missing", name)
		}
	}

	if _, err := UninstallShims(); err != nil {
		t.Fatalf("UninstallShims: %v", err)
	}
	for _, name := range ShimNames() {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("shim %s should have been removed", name)
		}
	}
}

func TestUninstallShims_NoShimsDirIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// No InstallShims call - the shims dir doesn't exist.
	if _, err := UninstallShims(); err != nil {
		t.Errorf("UninstallShims should be a clean no-op when dir is missing; got %v", err)
	}
}

func TestUninstallShims_PreservesUnknownFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	dir, err := InstallShims()
	if err != nil {
		t.Fatal(err)
	}
	// Drop a user-added file alongside our shims.
	userFile := filepath.Join(dir, "user-custom-tool")
	if err := os.WriteFile(userFile, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := UninstallShims(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userFile); err != nil {
		t.Errorf("user file %s should still exist after uninstall: %v", userFile, err)
	}
}

func TestRenderShimForName(t *testing.T) {
	if got := RenderShimForName("git"); !strings.Contains(got, `exec nimblegate git "$@"`) {
		t.Errorf("RenderShimForName(\"git\") missing routing line; got:\n%s", got)
	}
	if got := RenderShimForName("does-not-exist"); got != "" {
		t.Errorf("RenderShimForName(unknown) should return empty; got %q", got)
	}
}

func TestShimNames_MatchesSpecs(t *testing.T) {
	names := ShimNames()
	if len(names) != len(shimSpecs) {
		t.Errorf("ShimNames len = %d; want %d", len(names), len(shimSpecs))
	}
	want := map[string]bool{"git": true, "apt": true, "apt-get": true, "rm": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected shim name %q", n)
		}
	}
}
