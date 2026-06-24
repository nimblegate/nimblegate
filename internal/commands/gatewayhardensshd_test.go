// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --apply writes the hardening drop-in (to a custom dir, so sshd -t is skipped).
func TestGatewayHardenSSHD_applyWritesConfig(t *testing.T) {
	dir := t.TempDir()
	if code := gatewayHardenSSHD([]string{"--apply", "--sshd-config-dir", dir}); code != 0 {
		t.Fatalf("harden-sshd --apply returned %d", code)
	}
	b, err := os.ReadFile(filepath.Join(dir, "nimblegate-git.conf"))
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	for _, want := range []string{"PasswordAuthentication no", "Match User git", "AllowTcpForwarding no", "AllowStreamLocalForwarding no", "PermitTTY no"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config missing %q:\n%s", want, b)
		}
	}
}

// Dry-run (no --apply) writes nothing.
func TestGatewayHardenSSHD_dryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	if code := gatewayHardenSSHD([]string{"--sshd-config-dir", dir}); code != 0 {
		t.Fatalf("dry-run returned %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "nimblegate-git.conf")); !os.IsNotExist(err) {
		t.Error("dry-run must not write the config file")
	}
}
