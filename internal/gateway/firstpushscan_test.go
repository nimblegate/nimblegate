// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScanFirstPush_writesRecommendationJSON uses a fake scan binary (a shell
// script that prints a known JSON) as selfExe, so the test doesn't depend on
// the real nimblegate binary being built/installed.
func TestScanFirstPush_writesRecommendationJSON(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")

	// Bare repo with one commit.
	libBare := filepath.Join(reposRoot, "_repos", "foo.git")
	if err := os.MkdirAll(filepath.Join(reposRoot, "_repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", libBare).CombinedOutput(); err != nil {
		t.Fatalf("git init bare: %v\n%s", err, out)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo.git"), filepath.Join(reposRoot, "foo.git")); err != nil {
		t.Fatal(err)
	}

	// Working clone to push one commit from.
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "init"},
		{"push", "-q", filepath.Join(reposRoot, "foo.git"), "HEAD:refs/heads/main"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Policy lib for the repo.
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo"), filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatal(err)
	}

	// Fake scan binary: a shell script that ignores its args and prints the
	// expected JSON. Path becomes the "selfExe" arg.
	fakeJSON := `{"scanned_at":"2026-05-30T14:30:22Z","tree_ref":"HEAD","recommended_groups":[{"name":"@tier-1","always":true,"would_flag":0},{"name":"@web","always":false,"would_flag":1}],"recommended_linters":[],"dismissed":false}`
	fakeExe := filepath.Join(tmp, "fake-nimblegate")
	script := "#!/bin/sh\ncat <<'EOF'\n" + fakeJSON + "\nEOF\n"
	if err := os.WriteFile(fakeExe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := ScanFirstPush(filepath.Join(reposRoot, "foo.git"), "foo", policyRoot, fakeExe); err != nil {
		t.Fatalf("ScanFirstPush: %v", err)
	}

	// Recommendation file written via the activation symlink.
	data, err := os.ReadFile(filepath.Join(policyRoot, "foo", "scan-recommendation.json"))
	if err != nil {
		t.Fatalf("rec file: %v", err)
	}
	var rec struct {
		TreeRef           string                   `json:"tree_ref"`
		RecommendedGroups []map[string]interface{} `json:"recommended_groups"`
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("parse rec: %v\n%s", err, data)
	}
	if rec.TreeRef != "HEAD" || len(rec.RecommendedGroups) != 2 {
		t.Fatalf("rec mismatch: %+v", rec)
	}
}

// TestScanFirstPush_scanCommandFailureSurfaces - when the scan exec fails, the
// function returns an error and writes no rec file.
func TestScanFirstPush_scanCommandFailureSurfaces(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	libBare := filepath.Join(reposRoot, "_repos", "foo.git")
	if err := os.MkdirAll(filepath.Join(reposRoot, "_repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", libBare).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo.git"), filepath.Join(reposRoot, "foo.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo"), filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatal(err)
	}

	// Seed at least one commit so git archive HEAD has something.
	work := filepath.Join(tmp, "work")
	_ = os.MkdirAll(work, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		_, _ = c.CombinedOutput()
	}
	_ = os.WriteFile(filepath.Join(work, "x"), []byte("x"), 0o644)
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "x"},
		{"push", "-q", filepath.Join(reposRoot, "foo.git"), "HEAD:refs/heads/main"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		_, _ = c.CombinedOutput()
	}

	// Fake scan that exits non-zero.
	failExe := filepath.Join(tmp, "fail-scan")
	if err := os.WriteFile(failExe, []byte("#!/bin/sh\nexit 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := ScanFirstPush(filepath.Join(reposRoot, "foo.git"), "foo", policyRoot, failExe)
	if err == nil {
		t.Fatal("want error from failed scan exec")
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "foo", "scan-recommendation.json")); err == nil {
		t.Fatal("no rec file should be written when scan fails")
	}
}

// TestScanFirstPush_hookCwdScenario simulates the post-receive hook environment:
// the hook runs with cwd = the bare repo dir and (historically) the CLI computed
// reposRoot from a relative GIT_DIR. The fix makes ScanFirstPush take the bare
// path directly so the caller's reposRoot derivation can't break this path.
func TestScanFirstPush_hookCwdScenario(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")

	libBare := filepath.Join(reposRoot, "_repos", "foo.git")
	if err := os.MkdirAll(filepath.Join(reposRoot, "_repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", libBare).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo.git"), filepath.Join(reposRoot, "foo.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "foo"), filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatal(err)
	}

	// Seed a commit into the bare via a working clone.
	work := filepath.Join(tmp, "work")
	_ = os.MkdirAll(work, 0o755)
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "init"},
		{"push", "-q", filepath.Join(reposRoot, "foo.git"), "HEAD:refs/heads/main"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Fake scan binary.
	fakeJSON := `{"scanned_at":"2026-05-30T14:30:22Z","tree_ref":"HEAD","recommended_groups":[{"name":"@tier-1","always":true,"would_flag":0}],"dismissed":false}`
	fakeExe := filepath.Join(tmp, "fake-nimblegate")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\ncat <<'EOF'\n"+fakeJSON+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// CRUCIAL: cd INTO the bare repo before calling ScanFirstPush, mimicking
	// the post-receive hook's runtime cwd. Restore at end.
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(origWd)
	if err := os.Chdir(libBare); err != nil {
		t.Fatal(err)
	}

	// Call with the ABSOLUTE bare path (what the fixed callers do).
	if err := ScanFirstPush(libBare, "foo", policyRoot, fakeExe); err != nil {
		t.Fatalf("ScanFirstPush: %v", err)
	}

	if _, err := os.Stat(filepath.Join(policyRoot, "foo", "scan-recommendation.json")); err != nil {
		t.Fatalf("rec file not written: %v", err)
	}
}
