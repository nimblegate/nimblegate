// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/commands"
)

// makeProjectAt creates a temporary directory containing .appframes/ (so
// paths.FindProjectRoot succeeds) plus an appframes.toml with the given
// content. Returns the project root.
func makeProjectAt(t *testing.T, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".appframes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "appframes.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// withCwd switches working directory for the test, restoring on cleanup.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// captureStdout runs fn while replacing os.Stdout with a pipe, returns
// what fn wrote.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	w.Close()
	<-done
	os.Stdout = orig
	return buf.String()
}

func TestMigrateConfig_dryRun_myappShape(t *testing.T) {
	v1Content := `
[frames]
enabled = []

[ui]
applied_kits = ["cf-pages-project", "security-strict"]
`
	root := makeProjectAt(t, v1Content)
	withCwd(t, root)

	out := captureStdout(t, func() {
		exit := commands.MigrateConfig([]string{"--dry-run"})
		if exit != 0 {
			t.Errorf("exit = %d, want 0", exit)
		}
	})

	// Confirm the dry-run rendered the v2 config without writing it.
	if !strings.Contains(out, `version = 2`) {
		t.Errorf("output missing v2 schema marker:\n%s", out)
	}
	if !strings.Contains(out, `selected = "cloudflare"`) {
		t.Errorf("output missing platform selection:\n%s", out)
	}
	if !strings.Contains(out, `selected = "html"`) {
		t.Errorf("output missing framework selection:\n%s", out)
	}
	if !strings.Contains(out, `cf-workers`) {
		t.Errorf("output missing cf-workers exclusion:\n%s", out)
	}
	if !strings.Contains(out, "seo") {
		t.Errorf("output missing seo domain:\n%s", out)
	}

	// Original v1 file should be UNTOUCHED in dry-run mode.
	raw, _ := os.ReadFile(filepath.Join(root, "appframes.toml"))
	if !strings.Contains(string(raw), `applied_kits = ["cf-pages-project", "security-strict"]`) {
		t.Error("v1 config should be untouched in dry-run mode")
	}
}

func TestMigrateConfig_writesBackupAndV2(t *testing.T) {
	v1Content := `
[frames]
enabled = []

[ui]
applied_kits = ["web-app", "security-strict"]
`
	root := makeProjectAt(t, v1Content)
	withCwd(t, root)

	_ = captureStdout(t, func() {
		exit := commands.MigrateConfig([]string{})
		if exit != 0 {
			t.Errorf("exit = %d, want 0", exit)
		}
	})

	// New v2 config should exist and have schema.version = 2.
	newRaw, err := os.ReadFile(filepath.Join(root, "appframes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(newRaw), `version = 2`) {
		t.Errorf("new config missing v2 schema:\n%s", newRaw)
	}

	// Backup should exist with the original v1 content.
	backup, err := os.ReadFile(filepath.Join(root, "appframes.toml.v1-backup"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(backup), `applied_kits = ["web-app", "security-strict"]`) {
		t.Errorf("backup should contain original v1 content:\n%s", backup)
	}
}

func TestMigrateConfig_refusesV2WithoutForce(t *testing.T) {
	v2Content := `
[appframes.schema]
version = 2

[framework]
selected = "html"
`
	root := makeProjectAt(t, v2Content)
	withCwd(t, root)

	// Capture stderr instead - error messages go there.
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	var exit int
	captureStdout(t, func() {
		exit = commands.MigrateConfig([]string{})
	})
	w.Close()
	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, r)
	os.Stderr = origStderr

	if exit == 0 {
		t.Error("expected non-zero exit when config is already v2 and --force absent")
	}
	if !strings.Contains(errBuf.String(), "already schema v2") {
		t.Errorf("stderr should mention already-v2; got: %s", errBuf.String())
	}
}

func TestMigrateConfig_implicitCoreWhenNoAppliedKits(t *testing.T) {
	v1Content := `
[frames]
enabled = []
`
	root := makeProjectAt(t, v1Content)
	withCwd(t, root)

	out := captureStdout(t, func() {
		exit := commands.MigrateConfig([]string{"--dry-run"})
		if exit != 0 {
			t.Errorf("exit = %d, want 0", exit)
		}
	})

	// Even without [ui] applied_kits, the implicit "core" kit should produce
	// a v2 config with Core.Enabled = true.
	if !strings.Contains(out, `enabled = true`) {
		t.Errorf("expected core enabled in output:\n%s", out)
	}
}
