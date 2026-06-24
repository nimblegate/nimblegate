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

// fakeSystemctl records calls for assertion + returns a configurable error.
type fakeSystemctl struct {
	calls [][]string
	err   error
}

func (f *fakeSystemctl) run(args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{}, args...))
	return nil, f.err
}

// writeUnit returns a path to a temp file with the given content.
func writeUnit(t *testing.T, content string) string {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nimblegate-dashboard.service")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRewriteUnitAddr_replaceSpaceForm(t *testing.T) {
	body := `[Service]
ExecStart=/usr/local/bin/nimblegate gateway dashboard --serve --addr 0.0.0.0 --port 7900
`
	newBody, prev, changed := rewriteUnitAddr(body, "127.0.0.1")
	if !changed {
		t.Error("changed = false; want true")
	}
	if prev != "0.0.0.0" {
		t.Errorf("prev = %q; want 0.0.0.0", prev)
	}
	if !strings.Contains(newBody, "--addr 127.0.0.1") {
		t.Errorf("new body missing replacement; got: %s", newBody)
	}
}

func TestRewriteUnitAddr_replaceEqualsForm(t *testing.T) {
	body := `ExecStart=/bin/nimblegate gateway dashboard --addr=0.0.0.0 --serve`
	newBody, _, changed := rewriteUnitAddr(body, "127.0.0.1")
	if !changed {
		t.Error("expected change")
	}
	if !strings.Contains(newBody, "--addr=127.0.0.1") {
		t.Errorf("equals form not preserved; got: %s", newBody)
	}
}

func TestRewriteUnitAddr_idempotent(t *testing.T) {
	body := `ExecStart=/bin/nimblegate gateway dashboard --addr 127.0.0.1`
	newBody, prev, changed := rewriteUnitAddr(body, "127.0.0.1")
	if changed {
		t.Error("idempotent rewrite should report no change")
	}
	if newBody != body {
		t.Errorf("body shouldn't change; got: %s", newBody)
	}
	if prev != "127.0.0.1" {
		t.Errorf("prev = %q; want 127.0.0.1", prev)
	}
}

func TestRewriteUnitAddr_appendWhenMissing(t *testing.T) {
	body := `[Service]
ExecStart=/bin/nimblegate gateway dashboard --serve --port 7900
`
	newBody, prev, changed := rewriteUnitAddr(body, "127.0.0.1")
	if !changed {
		t.Error("missing flag should be appended")
	}
	if prev != "(absent)" {
		t.Errorf("prev = %q; want (absent)", prev)
	}
	if !strings.Contains(newBody, "--addr 127.0.0.1") {
		t.Errorf("new body missing appended flag; got: %s", newBody)
	}
}

func TestResolveBindChoice_aliasesAndLiteral(t *testing.T) {
	cases := []struct {
		choice string
		want   string
	}{
		{"localhost", "127.0.0.1"},
		{"all", "0.0.0.0"},
		{"10.0.0.5", "10.0.0.5"},
	}
	for _, c := range cases {
		got, err := resolveBindChoice(c.choice, nil, nil)
		if err != nil {
			t.Errorf("resolveBindChoice(%q) err = %v", c.choice, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolveBindChoice(%q) = %q; want %q", c.choice, got, c.want)
		}
	}
}

func TestResolveBindChoice_invalid(t *testing.T) {
	_, err := resolveBindChoice("not-an-ip", nil, nil)
	if err == nil {
		t.Error("expected error for invalid choice")
	}
}

func TestPromptBindChoice_acceptsNumericPick(t *testing.T) {
	in := strings.NewReader("2\n")
	var out bytes.Buffer
	got, err := promptBindChoice(in, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.0.0.0" {
		t.Errorf("got %q; want 0.0.0.0 (choice 2 = all)", got)
	}
}

func TestPromptBindChoice_defaultsToLocalhostOnEnter(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer
	got, err := promptBindChoice(in, &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1" {
		t.Errorf("got %q; want 127.0.0.1 (default)", got)
	}
}

func TestGatewayBindWith_refusesWhenUnitMissing(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "no-such-unit.service")
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayBindWith(
		[]string{"--unit", missing, "localhost"},
		strings.NewReader(""), &stdout, &stderr, fc.run,
	)
	if rc != 1 {
		t.Errorf("rc = %d; want 1", rc)
	}
	if !strings.Contains(stderr.String(), "unit file not found") {
		t.Errorf("expected unit-not-found message; got: %s", stderr.String())
	}
	if len(fc.calls) != 0 {
		t.Errorf("systemctl should not be called on missing unit; got %v", fc.calls)
	}
}

func TestGatewayBindWith_rewritesAndReloads(t *testing.T) {
	path := writeUnit(t, `ExecStart=/bin/nimblegate gateway dashboard --addr 0.0.0.0
`)
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayBindWith(
		[]string{"--unit", path, "localhost"},
		strings.NewReader(""), &stdout, &stderr, fc.run,
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "--addr 127.0.0.1") {
		t.Errorf("unit not rewritten; got: %s", body)
	}
	if len(fc.calls) != 1 || fc.calls[0][0] != "daemon-reload" {
		t.Errorf("expected one systemctl daemon-reload call; got %v", fc.calls)
	}
	if !strings.Contains(stdout.String(), "systemctl restart nimblegate-dashboard") {
		t.Errorf("expected restart instruction; got: %s", stdout.String())
	}
}

func TestGatewayBindWith_idempotentNoSystemctl(t *testing.T) {
	path := writeUnit(t, `ExecStart=/bin/nimblegate gateway dashboard --addr 127.0.0.1
`)
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayBindWith(
		[]string{"--unit", path, "localhost"},
		strings.NewReader(""), &stdout, &stderr, fc.run,
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "already set") {
		t.Errorf("expected 'already set' message; got: %s", stdout.String())
	}
	if len(fc.calls) != 0 {
		t.Errorf("idempotent run should skip daemon-reload; got %v", fc.calls)
	}
}

func TestGatewayBindWith_noReloadFlag(t *testing.T) {
	path := writeUnit(t, `ExecStart=/bin/nimblegate gateway dashboard --addr 0.0.0.0
`)
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayBindWith(
		[]string{"--unit", path, "--no-reload", "localhost"},
		strings.NewReader(""), &stdout, &stderr, fc.run,
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	if len(fc.calls) != 0 {
		t.Errorf("--no-reload should skip systemctl; got %v", fc.calls)
	}
}

func TestGatewayBindWith_systemctlErrorReturnsNonzero(t *testing.T) {
	path := writeUnit(t, `ExecStart=/bin/nimblegate gateway dashboard --addr 0.0.0.0
`)
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{err: errors.New("simulated systemctl fail")}
	rc := gatewayBindWith(
		[]string{"--unit", path, "localhost"},
		strings.NewReader(""), &stdout, &stderr, fc.run,
	)
	if rc != 2 {
		t.Errorf("rc = %d; want 2 (systemctl failed)", rc)
	}
	if !strings.Contains(stderr.String(), "reload manually") {
		t.Errorf("expected manual-reload hint; got: %s", stderr.String())
	}
	// Unit was already rewritten before systemctl call - confirm.
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "--addr 127.0.0.1") {
		t.Errorf("unit should be rewritten even when systemctl fails; got: %s", body)
	}
}

func TestGatewayBindWith_interactivePromptWhenNoArg(t *testing.T) {
	path := writeUnit(t, `ExecStart=/bin/nimblegate gateway dashboard --addr 0.0.0.0
`)
	var stdout, stderr bytes.Buffer
	fc := &fakeSystemctl{}
	rc := gatewayBindWith(
		[]string{"--unit", path},
		strings.NewReader("1\n"), &stdout, &stderr, fc.run,
	)
	if rc != 0 {
		t.Fatalf("rc = %d; stderr=%s", rc, stderr.String())
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "--addr 127.0.0.1") {
		t.Errorf("interactive choice 1 should produce 127.0.0.1; got: %s", body)
	}
}
