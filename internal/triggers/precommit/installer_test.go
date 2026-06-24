// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package precommit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall_WritesHookFile(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err != nil {
		t.Fatalf("Install: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(gitDir, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "nimblegate check --trigger=pre-commit") {
		t.Errorf("hook does not contain expected invocation; got:\n%s", data)
	}
	info, err := os.Stat(filepath.Join(gitDir, "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&0o111 == 0 {
		t.Errorf("hook is not executable: %v", info.Mode())
	}
}

func TestInstall_NoGitDirFails(t *testing.T) {
	root := t.TempDir()
	if err := Install(root); err == nil {
		t.Fatal("expected error when .git is missing")
	}
}

func TestInstall_RefusesToOverwriteForeignHook(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git", "hooks")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "pre-commit"), []byte("#!/bin/bash\n# not ours\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(root); err == nil {
		t.Fatal("expected refusal to overwrite foreign hook")
	}
}
