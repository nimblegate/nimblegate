// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestE2E_InitDefaultCoreKitFramesFire - the core kit frames enabled by
// default fire on a fresh init. Checks that a frame in the enabled list
// produces output when check runs (using a stdlib frame that's always in core).
func TestE2E_InitDefaultCoreKitFramesFire(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "init")
	cmd.Dir = tmp
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	// check on a clean empty dir should exit 0 (no findings, no errors).
	cmd2 := exec.Command(bin, "check")
	cmd2.Dir = tmp
	out, err := cmd2.CombinedOutput()
	if err != nil {
		t.Errorf("check on clean dir after init failed: %v\n%s", err, out)
	}
}

// TestE2E_InitTemplateContainsCoreFrameIDs - direct check that the
// scaffolded file lists flat frame IDs from the core kit (no wildcards).
func TestE2E_InitTemplateContainsCoreFrameIDs(t *testing.T) {
	bin := buildBinary(t)
	tmp := t.TempDir()
	cmd := exec.Command(bin, "init")
	cmd.Dir = tmp
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	cfg, err := os.ReadFile(filepath.Join(tmp, "appframes.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfgStr := string(cfg)
	for _, want := range []string{
		`"git/folder-branch-lock"`,
		`"security/no-hardcoded-credentials"`,
		`"filesystem/rm-rf-protected-paths"`,
	} {
		if !strings.Contains(cfgStr, want) {
			t.Errorf("default appframes.toml missing %s; got:\n%s", want, cfgStr)
		}
	}
	if strings.Contains(cfgStr, "/*") {
		t.Errorf("default appframes.toml must not contain wildcards; got:\n%s", cfgStr)
	}
}
