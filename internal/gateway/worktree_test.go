// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
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
	if err := materializeTree(bare, sha, dest); err != nil {
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
