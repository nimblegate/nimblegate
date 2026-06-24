// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateReceiveCap(t *testing.T) {
	good := []string{"", "0", "100", "100k", "100K", "500m", "500M", "1g", "1G", "999999"}
	bad := []string{"100b", "1.5g", "500 m", "500M ", "g", "-500", "500g500", "infinity"}
	for _, s := range good {
		if err := ValidateReceiveCap(s); err != nil {
			t.Errorf("ValidateReceiveCap(%q) err = %v; want nil", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateReceiveCap(s); err == nil {
			t.Errorf("ValidateReceiveCap(%q) err = nil; want error", s)
		}
	}
}

func TestApplyReceiveCap_setsConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	if err := ApplyReceiveCap(bare, "500m"); err != nil {
		t.Fatalf("ApplyReceiveCap: %v", err)
	}

	out, err := exec.Command("git", "-C", bare, "config", "--get", "receive.maxInputSize").CombinedOutput()
	if err != nil {
		t.Fatalf("verify config: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != "500m" {
		t.Errorf("receive.maxInputSize = %q; want 500m", got)
	}
}

func TestApplyReceiveCap_emptyUnsets(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := ApplyReceiveCap(bare, "500m"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyReceiveCap(bare, ""); err != nil {
		t.Fatalf("ApplyReceiveCap(\"\"): %v", err)
	}
	out, _ := exec.Command("git", "-C", bare, "config", "--get", "receive.maxInputSize").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("expected empty after unset; got %q", out)
	}
}

func TestApplyReceiveCap_unsetWhenAbsentIsOK(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	exec.Command("git", "init", "--bare", bare).Run()

	if err := ApplyReceiveCap(bare, ""); err != nil {
		t.Errorf("unset on absent key should be no-op; got %v", err)
	}
}

func TestApplyReceiveCap_invalidRejected(t *testing.T) {
	tmp := t.TempDir()
	bare := filepath.Join(tmp, "x.git")
	exec.Command("git", "init", "--bare", bare).Run()

	if err := ApplyReceiveCap(bare, "500zz"); err == nil {
		t.Error("expected validation error for invalid size")
	}
}

func TestAddRepo_appliesReceiveCap(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")

	err := AddRepo(AddOptions{
		Name:        "demo",
		UpstreamURL: "https://example.com/demo.git",
		Enabled:     true,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     "/bin/true",
	})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}

	bare := filepath.Join(reposRoot, "_repos", "demo.git")
	out, _ := exec.Command("git", "-C", bare, "config", "--get", "receive.maxInputSize").CombinedOutput()
	if strings.TrimSpace(string(out)) != DefaultReceiveMaxInputSize {
		t.Errorf("receive.maxInputSize on bare = %q; want %q", out, DefaultReceiveMaxInputSize)
	}

	pol, err := FilePolicyStore{Root: policyRoot}.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.MaxInputSize != DefaultReceiveMaxInputSize {
		t.Errorf("Policy.MaxInputSize = %q; want %q", pol.MaxInputSize, DefaultReceiveMaxInputSize)
	}
}
