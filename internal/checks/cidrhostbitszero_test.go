// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestCIDRHostBitsSet(t *testing.T) {
	cases := map[string]bool{
		// Should fire (host bits set):
		"142.132.208.101/24": true,
		"10.0.0.50/16":       true,
		"192.168.1.5/8":      true,
		// Should NOT fire:
		"142.132.208.0/24":   false, // proper network form
		"142.132.208.101/32": false, // single-host CIDR - host bits zero by definition
		"10.0.0.0/16":        false,
		"0.0.0.0/0":          false, // match-all
		// Malformed - return false (don't fire on garbage):
		"not-a-cidr":         false,
		"999.999.999.999/24": false,
		"142.132.208.101/-1": false,
	}
	for in, want := range cases {
		got := cidrHasHostBitsSet(in)
		if got != want {
			t.Errorf("cidrHasHostBitsSet(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestCIDRCanonicalForm(t *testing.T) {
	cases := map[string]string{
		"142.132.208.101/24": "142.132.208.0/24",
		"10.0.0.50/16":       "10.0.0.0/16",
		"192.168.1.5/8":      "192.0.0.0/8",
	}
	for in, want := range cases {
		got := cidrCanonicalForm(in)
		if got != want {
			t.Errorf("cidrCanonicalForm(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestCIDRHostBitsZero_BlocksHostBitsCIDR(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "firewall.yaml")
	body := "allow_from:\n  - 142.132.208.101/24   # WRONG\n  - 10.0.0.0/16        # OK\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CIDRHostBitsZero(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "142.132.208.101/24") {
		t.Errorf("expected reason to mention 142.132.208.101/24; got: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "10.0.0.0/16") {
		t.Errorf("10.0.0.0/16 is a valid network form - should not be flagged. reason: %s", got.Reason)
	}
}

func TestCIDRHostBitsZero_PassesCleanFile(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "firewall.yaml")
	body := "allow_from:\n  - 10.0.0.0/16\n  - 192.168.0.0/24\n  - 1.2.3.4/32\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CIDRHostBitsZero(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestCIDRHostBitsZero_RespectsLineDisableMarker(t *testing.T) {
	root := t.TempDir()
	cfg := filepath.Join(root, "firewall.yaml")
	body := "# appframes:disable-next-line network/cidr-host-bits-zero\nexample: 1.2.3.4/24\nfor_real: 10.0.0.50/16\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CIDRHostBitsZero(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (10.0.0.50/16 should still fire)\nreason: %s", got.Outcome, got.Reason)
	}
	if strings.Contains(got.Reason, "1.2.3.4/24") {
		t.Errorf("1.2.3.4/24 was on a disabled-next-line; should not be flagged. reason: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "10.0.0.50/16") {
		t.Errorf("expected 10.0.0.50/16 to be flagged; got: %s", got.Reason)
	}
}

func TestCIDRHostBitsZero_NonApplicableFilesIgnored(t *testing.T) {
	root := t.TempDir()
	// .md (documentation) - not an applicable format.
	md := filepath.Join(root, "README.md")
	if err := os.WriteFile(md, []byte("Use 1.2.3.4/24 as an example.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CIDRHostBitsZero(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (markdown is not in the applies-to set)", got.Outcome)
	}
}

func TestCIDRHostBitsZero_MultiplePlatformConfigs(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"main.tf":         `cidr = "10.0.0.50/16"`,
		"k8s/policy.yaml": `allow: 192.168.1.5/24`,
		"infra.json":      `{"cidr": "1.2.3.4/8"}`,
		"ufw-rules.conf":  `ALLOW 172.16.0.100/12`,
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := CIDRHostBitsZero(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (4 files all have host bits set)", got.Outcome)
	}
	// All four files should have surfaced hits.
	for _, want := range []string{"main.tf", "policy.yaml", "infra.json", "ufw-rules.conf"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("reason missing %s\nfull reason: %s", want, got.Reason)
		}
	}
}
