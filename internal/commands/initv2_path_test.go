// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeSnapstaticShape lays down enough files in tmp that detectSignals
// reports HTML + wrangler.toml (the canonical myapp shape).
func makeSnapstaticShape(t *testing.T, tmp string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(tmp, "wrangler.toml"), []byte("name = \"x\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitV2_cleanSnapstaticShapeWritesV2Config(t *testing.T) {
	tmp := t.TempDir()
	makeSnapstaticShape(t, tmp)

	var stdout, stderr bytes.Buffer
	rc := initAtV2(tmp, "", "", strings.NewReader(""), &stdout, &stderr, false)
	if rc != 0 {
		t.Fatalf("init returned %d, want 0; stderr=%s", rc, stderr.String())
	}

	body, err := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"[appframes.schema]",
		"version = 2",
		`selected = "html"`,
		`selected = "cloudflare"`,
		"[core]",
		"enabled = true",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("config missing %q in:\n%s", want, s)
		}
	}
}

func TestInitV2_refusesToOverwriteExistingConfig(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "appframes.toml"), []byte("# preexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	rc := initAtV2(tmp, "", "", strings.NewReader(""), &stdout, &stderr, false)
	if rc != 1 {
		t.Errorf("rc = %d, want 1 (refuse-overwrite); stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("expected refuse-overwrite message; got: %s", stderr.String())
	}
}

func TestInitV2_flagOverridesEmptyDetection(t *testing.T) {
	tmp := t.TempDir() // no signals
	var stdout, stderr bytes.Buffer
	rc := initAtV2(tmp, "go", "static-host", strings.NewReader(""), &stdout, &stderr, false)
	if rc != 0 {
		t.Fatalf("rc=%d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	s := string(body)
	if !strings.Contains(s, `selected = "go"`) {
		t.Errorf("expected framework=go; got:\n%s", s)
	}
	if !strings.Contains(s, `selected = "static-host"`) {
		t.Errorf("expected platform=static-host; got:\n%s", s)
	}
}

func TestInitV2_conflictWithoutTTYErrorsOut(t *testing.T) {
	tmp := t.TempDir()
	// Force a framework conflict: both Svelte and Astro signals.
	if err := os.WriteFile(filepath.Join(tmp, "svelte.config.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "astro.config.mjs"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// tty=false → expect error-out path.
	rc := initAtV2(tmp, "", "", strings.NewReader(""), &stdout, &stderr, false)
	if rc != 1 {
		t.Errorf("rc=%d, want 1 (non-TTY conflict); stderr=%s", rc, stderr.String())
	}
	for _, want := range []string{"not a TTY", "--framework=", "svelte", "astro"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("expected error containing %q; got: %s", want, stderr.String())
		}
	}
	// And no config should have been written.
	if _, err := os.Stat(filepath.Join(tmp, "appframes.toml")); !os.IsNotExist(err) {
		t.Errorf("config should NOT exist on error path; stat err=%v", err)
	}
}

func TestInitV2_conflictWithTTYPromptsAndAccepts(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "svelte.config.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "astro.config.mjs"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sorted: 1=astro, 2=svelte. Pick 2 → svelte.
	stdin := strings.NewReader("2\n")
	var stdout, stderr bytes.Buffer
	rc := initAtV2(tmp, "", "", stdin, &stdout, &stderr, true)
	if rc != 0 {
		t.Fatalf("rc=%d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	if !strings.Contains(string(body), `selected = "svelte"`) {
		t.Errorf("expected svelte pick in config; got:\n%s", body)
	}
	if !strings.Contains(stdout.String(), "Pick one") {
		t.Errorf("expected prompt in stdout; got: %s", stdout.String())
	}
}

func TestInitV2_conflictDefusedByFlag(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "svelte.config.js"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "astro.config.mjs"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// Conflict present, but --framework=astro defuses it. Non-TTY OK.
	rc := initAtV2(tmp, "astro", "", strings.NewReader(""), &stdout, &stderr, false)
	if rc != 0 {
		t.Fatalf("rc=%d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	if !strings.Contains(string(body), `selected = "astro"`) {
		t.Errorf("expected astro pick; got:\n%s", body)
	}
}

func TestInit_v2FlagRoutesToV2Path(t *testing.T) {
	// Verify the --v2 flag in initAtWith dispatches to initAtV2 (not the
	// v1 kit-based path). Use empty tmp dir + --framework=html flag so no
	// detection ambiguity surfaces.
	tmp := t.TempDir()
	var stdout, stderr bytes.Buffer
	rc := initAtWith(tmp, []string{"--v2", "--framework=html", "--platform=static-host"},
		strings.NewReader(""), &stdout, &stderr, false)
	if rc != 0 {
		t.Fatalf("rc=%d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	s := string(body)
	if !strings.Contains(s, "[appframes.schema]") || !strings.Contains(s, "version = 2") {
		t.Errorf("expected v2 schema header; got:\n%s", s)
	}
	// v1 markers should NOT appear in v2 output.
	if strings.Contains(s, "[ui]\napplied_kits") {
		t.Errorf("v1 [ui] section leaked into v2 path:\n%s", s)
	}
}

// Suppress unused-import warning for io if the test file ever drops its
// io.Reader/Writer uses during refactors.
var _ = io.EOF
