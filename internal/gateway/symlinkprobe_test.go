// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestSymlinkProbe proves the load-bearing assumption that gateway operations
// work correctly when policy dir + bare repo are reached via symlinks.
// This must pass before refactoring AddRepo to the symlink-farm layout.
func TestSymlinkProbe(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy-root")
	reposRoot := filepath.Join(tmp, "repos-root")

	// 1. Real policy dir under _repos/.
	libPolicy := filepath.Join(policyRoot, "_repos", "foo")
	if err := os.MkdirAll(libPolicy, 0o755); err != nil {
		t.Fatalf("mkdir lib policy: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libPolicy, "gateway.toml"),
		[]byte("repo = \"foo\"\nupstream_url = \"\"\nenabled = true\n"), 0o644); err != nil {
		t.Fatalf("write gateway.toml: %v", err)
	}

	// 2. Activation symlink.
	linkPolicy := filepath.Join(policyRoot, "foo")
	if err := os.Symlink(filepath.Join("_repos", "foo"), linkPolicy); err != nil {
		t.Fatalf("symlink policy: %v", err)
	}

	// 3. Glob via symlink finds gateway.toml.
	matches, err := filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("glob through symlink: matches=%v err=%v", matches, err)
	}
	if filepath.Base(filepath.Dir(matches[0])) != "foo" {
		t.Fatalf("glob result wrong: %s", matches[0])
	}

	// 4. FilePolicyStore.Load reads through symlink.
	pol, err := (FilePolicyStore{Root: policyRoot}).Load("foo")
	if err != nil {
		t.Fatalf("PolicyStore.Load: %v", err)
	}
	if pol.Repo != "foo" {
		t.Fatalf("policy repo: got %q want foo", pol.Repo)
	}

	// 5. Real bare repo under _repos/.
	libBare := filepath.Join(reposRoot, "_repos", "foo.git")
	if err := os.MkdirAll(filepath.Dir(libBare), 0o755); err != nil {
		t.Fatalf("mkdir lib repos parent: %v", err)
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", libBare).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v\n%s", err, out)
	}

	// 6. Install sentinel hooks that touch a file so we can prove they fired.
	preReceiveSentinel := filepath.Join(libBare, "pre-receive-fired")
	postReceiveSentinel := filepath.Join(libBare, "post-receive-fired")
	preHook := "#!/bin/sh\ntouch " + preReceiveSentinel + "\n"
	postHook := "#!/bin/sh\ntouch " + postReceiveSentinel + "\n"
	if err := os.WriteFile(filepath.Join(libBare, "hooks", "pre-receive"), []byte(preHook), 0o755); err != nil {
		t.Fatalf("write pre-receive: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libBare, "hooks", "post-receive"), []byte(postHook), 0o755); err != nil {
		t.Fatalf("write post-receive: %v", err)
	}

	// 7. Activation symlink for bare repo.
	linkBare := filepath.Join(reposRoot, "foo.git")
	if err := os.Symlink(filepath.Join("_repos", "foo.git"), linkBare); err != nil {
		t.Fatalf("symlink bare: %v", err)
	}

	// 8. Create a working clone with one commit, push to the symlinked bare URL.
	workdir := filepath.Join(tmp, "work")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir work: %v", err)
	}
	mustRun(t, workdir, "git", "init", "-q", "-b", "main")
	mustRun(t, workdir, "git", "config", "user.email", "probe@local")
	mustRun(t, workdir, "git", "config", "user.name", "probe")
	if err := os.WriteFile(filepath.Join(workdir, "README"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, workdir, "git", "add", ".")
	mustRun(t, workdir, "git", "commit", "-q", "-m", "init")
	mustRun(t, workdir, "git", "push", "-q", linkBare, "HEAD:refs/heads/main")

	// 9. Confirm both hooks fired (sentinels exist).
	if _, err := os.Stat(preReceiveSentinel); err != nil {
		t.Fatalf("pre-receive sentinel missing: %v", err)
	}
	if _, err := os.Stat(postReceiveSentinel); err != nil {
		t.Fatalf("post-receive sentinel missing: %v", err)
	}

	// 10. Unlink the activation symlink - files in _repos/ stay readable.
	if err := os.Remove(linkPolicy); err != nil {
		t.Fatalf("unlink policy: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(libPolicy, "gateway.toml")); err != nil {
		t.Fatalf("lib still readable after unlink: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	if len(matches) != 0 {
		t.Fatalf("glob after unlink: want 0 got %v", matches)
	}

	// 11. Re-create symlink - listing comes back.
	if err := os.Symlink(filepath.Join("_repos", "foo"), linkPolicy); err != nil {
		t.Fatalf("re-symlink: %v", err)
	}
	matches, _ = filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	if len(matches) != 1 {
		t.Fatalf("glob after restore: want 1 got %v", matches)
	}
}

func mustRun(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}
