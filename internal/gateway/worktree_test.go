// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/scanignore"
)

// makeBareWithCommit creates a bare repo containing one commit with file
// "hello.txt", and returns (bareDir, commitSHA). Reused by relay_test.go.
func makeBareWithCommit(t *testing.T) (string, string) {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run(work, "init", "-q")
	if err := os.WriteFile(filepath.Join(work, "hello.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", ".")
	run(work, "commit", "-qm", "first")
	var sha string
	{
		c := exec.Command("git", "rev-parse", "HEAD")
		c.Dir = work
		b, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		sha = string(b[:40])
	}
	bare := t.TempDir()
	run(bare, "init", "--bare", "-q")
	run(work, "push", "-q", bare, "HEAD:refs/heads/main")
	return bare, sha
}

func TestOverlayPolicy_wipesPushedConfig(t *testing.T) {
	destDir := t.TempDir()
	// Simulate config injected by the push.
	pushedTOML := []byte("[frames]\nenabled=[\"evil-frame\"]\n")
	if err := os.WriteFile(filepath.Join(destDir, "appframes.toml"), pushedTOML, 0o644); err != nil {
		t.Fatal(err)
	}
	appframesDir := filepath.Join(destDir, ".appframes")
	if err := os.MkdirAll(appframesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appframesDir, "evil.md"), []byte("evil"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Gateway's own enforced policy.
	policyDir := t.TempDir()
	gatewayTOML := []byte("[frames]\nenabled=[]\n")
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"), gatewayTOML, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := overlayPolicy(policyDir, destDir); err != nil {
		t.Fatalf("overlayPolicy: %v", err)
	}

	// (a) appframes.toml must be the gateway's, not the pushed one.
	got, err := os.ReadFile(filepath.Join(destDir, "appframes.toml"))
	if err != nil {
		t.Fatalf("reading appframes.toml: %v", err)
	}
	if string(got) != string(gatewayTOML) {
		t.Errorf("appframes.toml = %q, want gateway's %q", got, gatewayTOML)
	}

	// (b) Pushed .appframes/ must be gone.
	if _, err := os.Stat(filepath.Join(destDir, ".appframes")); err == nil {
		t.Error(".appframes/ should have been removed but still exists")
	}
}

func TestOverlayPolicy_wipesPushedIgnoreMarkers(t *testing.T) {
	destDir := t.TempDir()

	// Top-level .appframes-ignore pushed by the commit.
	if err := os.WriteFile(filepath.Join(destDir, scanignore.MarkerFilename), []byte("*.pem\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nested sub/.appframes-ignore - the engine discovers these tree-wide.
	subDir := filepath.Join(destDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, scanignore.MarkerFilename), []byte("*\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	policyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"), []byte("[frames]\nenabled=[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := overlayPolicy(policyDir, destDir); err != nil {
		t.Fatalf("overlayPolicy: %v", err)
	}

	// Both marker files must be gone - a push must not control scan-ignore policy.
	if _, err := os.Stat(filepath.Join(destDir, scanignore.MarkerFilename)); err == nil {
		t.Error("top-level .appframes-ignore should have been removed but still exists")
	}
	if _, err := os.Stat(filepath.Join(subDir, scanignore.MarkerFilename)); err == nil {
		t.Error("sub/.appframes-ignore should have been removed but still exists")
	}
}

func TestMaterializeTreeAndOverlay(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	dest := t.TempDir()
	if err := materializeTree(bare, sha, dest, 0); err != nil {
		t.Fatalf("materializeTree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "hello.txt")); err != nil {
		t.Errorf("expected hello.txt in materialized tree: %v", err)
	}

	policyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"), []byte("[frames]\nenabled=[]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := overlayPolicy(policyDir, dest); err != nil {
		t.Fatalf("overlayPolicy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "appframes.toml")); err != nil {
		t.Errorf("expected overlaid appframes.toml: %v", err)
	}
}

// writeRawTree stores a tree object byte for byte, bypassing the checks git
// applies when it builds trees itself - the way a hostile client can.
func writeRawTree(t *testing.T, bare string, entries [][3]string) string {
	t.Helper()
	var buf bytes.Buffer
	for _, e := range entries {
		raw, err := hex.DecodeString(e[2])
		if err != nil {
			t.Fatal(err)
		}
		buf.WriteString(e[0] + " " + e[1] + "\x00")
		buf.Write(raw)
	}
	c := exec.Command("git", "--git-dir", bare, "hash-object", "-t", "tree", "-w", "--stdin", "--literally")
	c.Stdin = &buf
	out, err := c.Output()
	if err != nil {
		t.Fatalf("hash-object: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// Trees no honest git client produces must not let the scan write outside its
// directory or read the gateway's files. receive.fsckObjects refuses them at
// the door; materializeTree is the second line for repos registered before it.
func TestMaterializeTree_hostileTreesStayInside(t *testing.T) {
	bare, _ := makeBareWithCommit(t)
	blob := func(s string) string {
		c := exec.Command("git", "--git-dir", bare, "hash-object", "-w", "--stdin")
		c.Stdin = strings.NewReader(s)
		out, err := c.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	evil := blob("evil\n")
	gitCfg := blob("[core]\n\tfsmonitor = touch /tmp/nimblegate-should-never-exist\n")
	inner := writeRawTree(t, bare, [][3]string{{"100644", "config", gitCfg}})
	escape := writeRawTree(t, bare, [][3]string{{"100644", "escaped.txt", evil}})
	cases := map[string][][3]string{
		"dot git":      {{"40000", ".git", inner}, {"100644", "a.txt", evil}},
		"dot dot":      {{"40000", "..", escape}, {"100644", "a.txt", evil}},
		"path in name": {{"100644", "x/../../escaped.txt", evil}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			tree := writeRawTree(t, bare, entries)
			c := exec.Command("git", "--git-dir", bare, "commit-tree", tree, "-m", name)
			c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			out, err := c.Output()
			if err != nil {
				t.Fatalf("commit-tree: %v", err)
			}
			parent := t.TempDir()
			dest := filepath.Join(parent, "scan")
			if err := os.Mkdir(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := materializeTree(bare, strings.TrimSpace(string(out)), dest, 0); err == nil {
				t.Error("materializeTree accepted a tree with a forbidden path; the scan must fail")
			}
			left, _ := os.ReadDir(parent)
			if len(left) != 1 {
				t.Errorf("files appeared next to the scan dir: %v", left)
			}
			if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
				t.Error("a .git directory from the push was unpacked into the scan dir")
			}
		})
	}
}
