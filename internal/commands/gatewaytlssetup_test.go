// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLookPath returns the given path/err for any name; tests use it to
// simulate "caddy installed" or "caddy missing" without touching real $PATH.
func stubLookPath(path string, err error) lookPathFunc {
	return func(name string) (string, error) { return path, err }
}

func writeFakeUnit(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nimblegate-dashboard.service")
	body := `ExecStart=/usr/local/bin/nimblegate gateway dashboard --serve --addr 0.0.0.0
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIsPlausibleDomain(t *testing.T) {
	good := []string{
		"example.com",
		"nimblegate.example.com",
		"a.b",
		"x.y.z.test",
		"my-host.example.org",
	}
	bad := []string{
		"",
		"localhost",
		"example",
		"-bad.com",
		"bad-.com",
		".example.com",
		"example.com.",
		"with spaces.com",
		"with/slash.com",
	}
	for _, d := range good {
		if !isPlausibleDomain(d) {
			t.Errorf("isPlausibleDomain(%q) = false; want true", d)
		}
	}
	for _, d := range bad {
		if isPlausibleDomain(d) {
			t.Errorf("isPlausibleDomain(%q) = true; want false", d)
		}
	}
}

func TestGatewayTLSSetup_requiresDomain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 2 {
		t.Errorf("rc = %d; want 2 (--domain missing)", rc)
	}
	if !strings.Contains(stderr.String(), "--domain is required") {
		t.Errorf("expected --domain hint; got: %s", stderr.String())
	}
}

func TestGatewayTLSSetup_rejectsInvalidDomain(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "localhost"},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 2 {
		t.Errorf("rc = %d; want 2 (invalid domain)", rc)
	}
}

func TestGatewayTLSSetup_caddyNotInstalledStops(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com"},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("", errors.New("not found")),
	)
	if rc != 1 {
		t.Errorf("rc = %d; want 1 (caddy missing)", rc)
	}
	if !strings.Contains(stderr.String(), "apt install -y caddy") {
		t.Errorf("expected install hint; got: %s", stderr.String())
	}
}

func TestGatewayTLSSetup_unitMissingStops(t *testing.T) {
	tmp := t.TempDir()
	missingUnit := filepath.Join(tmp, "no-such.service")
	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com", "--unit", missingUnit, "--caddyfile", filepath.Join(tmp, "Caddyfile")},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 1 {
		t.Errorf("rc = %d; want 1 (dashboard unit missing)", rc)
	}
	if !strings.Contains(stderr.String(), "SETUP-proxmox-trixie") {
		t.Errorf("expected LAN-install hint; got: %s", stderr.String())
	}
}

func TestGatewayTLSSetup_dryRunDoesNothing(t *testing.T) {
	unitPath := writeFakeUnit(t)
	tmp := t.TempDir()
	caddyfile := filepath.Join(tmp, "Caddyfile")

	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com", "--unit", unitPath, "--caddyfile", caddyfile, "--dry-run"},
		strings.NewReader(""), &stdout, &stderr,
		fc.run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	if _, err := os.Stat(caddyfile); !os.IsNotExist(err) {
		t.Error("dry-run should not write Caddyfile")
	}
	if len(fc.calls) != 0 {
		t.Errorf("dry-run should not call systemctl; got %v", fc.calls)
	}
	for _, want := range []string{"would write", "would call gateway bind", "would run"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("dry-run output missing %q; got: %s", want, stdout.String())
		}
	}
}

func TestGatewayTLSSetup_refusesToOverwriteWithoutForce(t *testing.T) {
	unitPath := writeFakeUnit(t)
	tmp := t.TempDir()
	caddyfile := filepath.Join(tmp, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("# preexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com", "--unit", unitPath, "--caddyfile", caddyfile},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 1 {
		t.Errorf("rc = %d; want 1 (refuse without --force)", rc)
	}
	if !strings.Contains(stderr.String(), "already exists") {
		t.Errorf("expected refuse-overwrite message; got: %s", stderr.String())
	}
}

func TestGatewayTLSSetup_writesCaddyfileAndRestartsServices(t *testing.T) {
	unitPath := writeFakeUnit(t)
	tmp := t.TempDir()
	caddyfile := filepath.Join(tmp, "Caddyfile")

	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "nimblegate.example.com", "--unit", unitPath, "--caddyfile", caddyfile},
		strings.NewReader(""), &stdout, &stderr,
		fc.run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}

	body, err := os.ReadFile(caddyfile)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"nimblegate.example.com",
		"reverse_proxy 127.0.0.1:7900",
		"Strict-Transport-Security",
		"X-Content-Type-Options",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Caddyfile missing %q in:\n%s", want, s)
		}
	}

	// Dashboard unit should now bind to localhost.
	unitBody, _ := os.ReadFile(unitPath)
	if !strings.Contains(string(unitBody), "--addr 127.0.0.1") {
		t.Errorf("dashboard unit should be rebound to 127.0.0.1; got:\n%s", unitBody)
	}

	// Systemctl calls should include daemon-reload twice (once from gateway
	// bind, once from tls-setup's restart sequence), plus restart caddy +
	// restart nimblegate-dashboard.
	if len(fc.calls) < 3 {
		t.Fatalf("expected at least 3 systemctl calls; got %v", fc.calls)
	}
	sawRestartCaddy := false
	sawRestartDashboard := false
	for _, call := range fc.calls {
		if len(call) >= 2 && call[0] == "restart" && call[1] == "caddy" {
			sawRestartCaddy = true
		}
		if len(call) >= 2 && call[0] == "restart" && call[1] == "nimblegate-dashboard" {
			sawRestartDashboard = true
		}
	}
	if !sawRestartCaddy {
		t.Errorf("expected `systemctl restart caddy`; got %v", fc.calls)
	}
	if !sawRestartDashboard {
		t.Errorf("expected `systemctl restart nimblegate-dashboard`; got %v", fc.calls)
	}
	if !strings.Contains(stdout.String(), "https://nimblegate.example.com") {
		t.Errorf("expected success URL; got: %s", stdout.String())
	}
}

func TestGatewayTLSSetup_noBindFlipSkipsRebind(t *testing.T) {
	unitPath := writeFakeUnit(t)
	tmp := t.TempDir()
	caddyfile := filepath.Join(tmp, "Caddyfile")

	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com", "--unit", unitPath, "--caddyfile", caddyfile, "--no-bind-flip"},
		strings.NewReader(""), &stdout, &stderr,
		fc.run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	// Unit should still have 0.0.0.0 (untouched).
	unitBody, _ := os.ReadFile(unitPath)
	if !strings.Contains(string(unitBody), "--addr 0.0.0.0") {
		t.Errorf("--no-bind-flip should leave the unit untouched; got:\n%s", unitBody)
	}
}

func TestGatewayTLSSetup_forceOverwritesExisting(t *testing.T) {
	unitPath := writeFakeUnit(t)
	tmp := t.TempDir()
	caddyfile := filepath.Join(tmp, "Caddyfile")
	if err := os.WriteFile(caddyfile, []byte("# preexisting\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	rc := gatewayTLSSetupWith(
		[]string{"--domain", "example.com", "--unit", unitPath, "--caddyfile", caddyfile, "--force"},
		strings.NewReader(""), &stdout, &stderr,
		(&fakeSystemctl{}).run,
		stubLookPath("/usr/bin/caddy", nil),
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(caddyfile)
	if strings.Contains(string(body), "preexisting") {
		t.Errorf("--force should overwrite; got: %s", body)
	}
	if !strings.Contains(string(body), "example.com") {
		t.Errorf("expected rendered Caddyfile; got: %s", body)
	}
}
