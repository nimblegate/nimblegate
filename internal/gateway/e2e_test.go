// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"nimblegate/internal/config"
)

// e2eGoBin is prepended to PATH so child `go` / hook invocations resolve the
// toolchain that may not be on the default PATH in this environment.
var e2eGoBin = detectGoBin()

// detectGoBin locates the directory holding the `go` binary, preferring an
// explicit GO_BIN override, then a `go` already on PATH, then GOROOT/bin.
func detectGoBin() string {
	if v := os.Getenv("GO_BIN"); v != "" {
		return v
	}
	if p, err := exec.LookPath("go"); err == nil {
		return filepath.Dir(p)
	}
	return filepath.Join(runtime.GOROOT(), "bin")
}

// e2eEnv returns the current environment with e2eGoBin prepended to PATH and
// deterministic git identity set, suitable for both the binary build and the
// git commands the test drives.
func e2eEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+5)
	for _, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			out = append(out, "PATH="+e2eGoBin+string(os.PathListSeparator)+strings.TrimPrefix(kv, "PATH="))
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"GIT_AUTHOR_NAME=e2e", "GIT_AUTHOR_EMAIL=e2e@e2e",
		"GIT_COMMITTER_NAME=e2e", "GIT_COMMITTER_EMAIL=e2e@e2e")
	return out
}

// tryGit runs a git command in dir and returns combined output + error.
// Used where a non-zero exit is an expected outcome (the BLOCK push).
func tryGit(dir string, args ...string) (string, error) {
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = e2eEnv()
	out, err := c.CombinedOutput()
	return string(out), err
}

// mustGit runs a git command in dir and fatals on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := tryGit(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

// TestEndToEnd_gateThenRelay drives the whole policy gateway through real git:
// build the binary, register a repo, push clean (accepted + relayed), push a
// private key on the protected branch (rejected + NOT relayed), and push the
// same dirty commit to a non-protected branch (accepted + relayed). This is the
// regression guard for the fix that file-scanning frames only run under
// TriggerCLI, which engineChecker now uses.
func TestEndToEnd_gateThenRelay(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e real-git harness skipped in -short mode")
	}

	// repo root = <root>/internal/gateway/e2e_test.go → ../..
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// 1. Build the nimblegate binary.
	exe := filepath.Join(t.TempDir(), "nimblegate")
	build := exec.Command("go", "build", "-o", exe, "./cmd/nimblegate")
	build.Dir = repoRoot
	build.Env = e2eEnv()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build nimblegate: %v\n%s", err, out)
	}

	// 2. Scratch upstream bare repo.
	upstreamDir := t.TempDir()
	mustGit(t, upstreamDir, "init", "--bare", "-q", upstreamDir)

	// 3. Register the repo on the gateway via the built binary.
	gwRoot := t.TempDir()
	reposRoot := t.TempDir()
	add := exec.Command(exe, "gateway", "add",
		"--name", "demo",
		"--upstream", "file://"+upstreamDir,
		"--protect", "refs/heads/main",
		"--policy-root", gwRoot,
		"--repos-root", reposRoot)
	add.Env = e2eEnv()
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("gateway add: %v\n%s", err, out)
	}
	gatewayBare := filepath.Join(reposRoot, "demo.git")
	if _, err := os.Stat(filepath.Join(gatewayBare, "hooks", "pre-receive")); err != nil {
		t.Fatalf("expected pre-receive hook: %v", err)
	}

	// 4. Gateway-held policy: enable the frames needed for this test -
	// security/no-private-keys-in-repo (a content-BLOCK frame that exercises
	// the TriggerCLI tree-walk) plus the other catastrophic-prevention frames
	// from the core kit.
	policyDir := filepath.Join(gwRoot, "demo")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"),
		[]byte("[frames]\nenabled = [\n    \"security/no-private-keys-in-repo\",\n    \"security/no-hardcoded-credentials\",\n    \"git/no-bypass-pre-commit\",\n    \"git/no-force-push-main\",\n    \"git/no-amend-pushed-commits\",\n    \"git/folder-branch-lock\",\n    \"filesystem/rm-rf-protected-paths\",\n    \"commands/curl-pipe-shell\",\n    \"commands/apt-purge-preview\",\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Working clone. Initialize fresh and point origin at the gateway bare repo
	// (clone-of-empty warns; a plain init + remote add is cleaner).
	work := t.TempDir()
	mustGit(t, work, "init", "-q", work)
	mustGit(t, work, "remote", "add", "origin", gatewayBare)

	// 5. Clean push → accepted + relayed.
	if err := os.WriteFile(filepath.Join(work, "ok.txt"), []byte("harmless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "ok.txt")
	mustGit(t, work, "commit", "-qm", "clean commit")
	cleanSHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	if out, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("clean push should succeed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != cleanSHA {
		t.Fatalf("upstream main = %q after clean push, want relayed %q", got, cleanSHA)
	}

	// 6. Dirty push (private key) on protected main → rejected + NOT relayed.
	if err := os.WriteFile(filepath.Join(work, "leak.pem"),
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZQ==\n-----END OPENSSH PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "leak.pem")
	mustGit(t, work, "commit", "-qm", "leak a key")
	dirtySHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	dirtyOut, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main")
	if err == nil {
		t.Fatalf("dirty push to protected main should FAIL but succeeded\n%s", dirtyOut)
	}
	if !strings.Contains(dirtyOut, "no-private-keys-in-repo") {
		t.Fatalf("rejection output should name the blocking frame; got:\n%s", dirtyOut)
	}
	if !strings.Contains(dirtyOut, "rejected") {
		t.Fatalf("rejection output should say 'rejected'; got:\n%s", dirtyOut)
	}
	// Upstream main must be UNCHANGED - the bad commit did not relay.
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != cleanSHA {
		t.Fatalf("upstream main = %q after rejected push, want unchanged %q (dirty=%s)", got, cleanSHA, dirtySHA)
	}

	// 7. Non-protected branch → accepted + relayed without gating.
	if out, err := tryGit(work, "push", "origin", "HEAD:refs/heads/feature/x"); err != nil {
		t.Fatalf("dirty push to non-protected feature branch should succeed: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/feature/x")); got != dirtySHA {
		t.Fatalf("upstream feature/x = %q, want relayed dirty %q", got, dirtySHA)
	}
}

// TestEndToEnd_tuningDemotesGate proves the policy-tuning linchpin: a severity
// override that demotes a BLOCK frame to WARN causes a previously-rejected push
// to be accepted. This guards the file→gate chain: the gateway reads the
// appframes.toml policy file and applies severity overrides before scanning, so
// writing the override directly to the file exercises the same path the
// /policy/severity HTTP handler would take.
func TestEndToEnd_tuningDemotesGate(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e real-git harness skipped in -short mode")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// 1. Build the nimblegate binary.
	exe := filepath.Join(t.TempDir(), "nimblegate")
	build := exec.Command("go", "build", "-o", exe, "./cmd/nimblegate")
	build.Dir = repoRoot
	build.Env = e2eEnv()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build nimblegate: %v\n%s", err, out)
	}

	// 2. Scratch upstream bare repo.
	upstreamDir := t.TempDir()
	mustGit(t, upstreamDir, "init", "--bare", "-q", upstreamDir)

	// 3. Register the repo on the gateway via the built binary.
	gwRoot := t.TempDir()
	reposRoot := t.TempDir()
	add := exec.Command(exe, "gateway", "add",
		"--name", "demo",
		"--upstream", "file://"+upstreamDir,
		"--protect", "refs/heads/main",
		"--policy-root", gwRoot,
		"--repos-root", reposRoot)
	add.Env = e2eEnv()
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("gateway add: %v\n%s", err, out)
	}
	gatewayBare := filepath.Join(reposRoot, "demo.git")

	// 4. Write policy enabling the core catastrophic-prevention frames
	// (BLOCK severity for security/no-private-keys-in-repo by default).
	if err := (FramePolicy{Enabled: []string{
		"security/no-private-keys-in-repo",
		"security/no-hardcoded-credentials",
		"git/no-bypass-pre-commit",
		"git/no-force-push-main",
		"git/no-amend-pushed-commits",
		"git/folder-branch-lock",
		"filesystem/rm-rf-protected-paths",
		"commands/curl-pipe-shell",
		"commands/apt-purge-preview",
	}}).Save(gwRoot, "demo"); err != nil {
		t.Fatal(err)
	}

	// 5. Working clone. Push a clean baseline so upstream main exists.
	work := t.TempDir()
	mustGit(t, work, "init", "-q", work)
	mustGit(t, work, "remote", "add", "origin", gatewayBare)
	if err := os.WriteFile(filepath.Join(work, "ok.txt"), []byte("harmless\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "ok.txt")
	mustGit(t, work, "commit", "-qm", "baseline")
	baseSHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	if out, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("baseline push should succeed: %v\n%s", err, out)
	}

	// 6. Push a commit containing a private key → must be REJECTED (BLOCK).
	if err := os.WriteFile(filepath.Join(work, "leak.pem"),
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZQ==\n-----END OPENSSH PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "leak.pem")
	mustGit(t, work, "commit", "-qm", "add private key")
	keySHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	rejectOut, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main")
	if err == nil {
		t.Fatalf("push with private key should be REJECTED but was accepted\n%s", rejectOut)
	}
	if !strings.Contains(rejectOut, "no-private-keys-in-repo") {
		t.Fatalf("rejection output should name no-private-keys-in-repo; got:\n%s", rejectOut)
	}
	// Upstream main must still be the baseline - key commit was not relayed.
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != baseSHA {
		t.Fatalf("upstream main = %q after rejected push, want unchanged %q (key=%s)", got, baseSHA, keySHA)
	}

	// 7. TUNE: demote security/no-private-keys-in-repo from BLOCK to WARN.
	// This is exactly what the /policy/severity HTTP handler writes; testing at
	// the file level proves the appframes.toml → gate chain end-to-end.
	if err := (FramePolicy{
		Enabled: []string{
			"security/no-private-keys-in-repo",
			"security/no-hardcoded-credentials",
			"git/no-bypass-pre-commit",
			"git/no-force-push-main",
			"git/no-amend-pushed-commits",
			"git/folder-branch-lock",
			"filesystem/rm-rf-protected-paths",
			"commands/curl-pipe-shell",
			"commands/apt-purge-preview",
		},
		Severity: map[string]string{"security/no-private-keys-in-repo": "WARN"},
	}).Save(gwRoot, "demo"); err != nil {
		t.Fatal(err)
	}

	// 8. Re-push the same key commit (gateway bare still at baseSHA so the ref
	// advances; the gate re-runs). This time the frame is WARN → must be ACCEPTED
	// and the key commit must reach upstream.
	acceptOut, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main")
	if err != nil {
		t.Fatalf("push after severity demotion should SUCCEED: %v\n%s", err, acceptOut)
	}
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != keySHA {
		t.Fatalf("upstream main = %q after tuned push, want relayed key commit %q", got, keySHA)
	}
}

// TestEndToEnd_authoredRegexCheckWarns proves the operator-authoring loop:
// a kind="regex" check authored at WARN runs inside the real gate on a real
// push, does NOT block (push accepted, ref relayed), and is recorded as a WARN
// finding in the repo's audit log. This closes the chain LinterPolicy.Save →
// appframes.toml → gate scan → Decision.Findings → audit.log.
func TestEndToEnd_authoredRegexCheckWarns(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e real-git harness skipped in -short mode")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// 1. Build the nimblegate binary.
	exe := filepath.Join(t.TempDir(), "nimblegate")
	build := exec.Command("go", "build", "-o", exe, "./cmd/nimblegate")
	build.Dir = repoRoot
	build.Env = e2eEnv()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build nimblegate: %v\n%s", err, out)
	}

	// 2. Scratch upstream bare repo.
	upstreamDir := t.TempDir()
	mustGit(t, upstreamDir, "init", "--bare", "-q", upstreamDir)

	// 3. Register the repo on the gateway via the built binary.
	gwRoot := t.TempDir()
	reposRoot := t.TempDir()
	add := exec.Command(exe, "gateway", "add",
		"--name", "demo",
		"--upstream", "file://"+upstreamDir,
		"--protect", "refs/heads/main",
		"--policy-root", gwRoot,
		"--repos-root", reposRoot)
	add.Env = e2eEnv()
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("gateway add: %v\n%s", err, out)
	}
	gatewayBare := filepath.Join(reposRoot, "demo.git")

	// 4. Author the operator's check: a kind="regex" WARN linter that flags
	// unowned TODOs in .go files. Save preserves the (empty) frames section.
	lp, err := LoadLinterPolicy(gwRoot, "demo")
	if err != nil {
		t.Fatal(err)
	}
	lp = lp.With("no-owner-todos", config.LinterConfig{
		Enabled:  true,
		Kind:     "regex",
		Severity: "warn",
		Patterns: []string{"*.go"},
		Regex:    `TODO\(no-owner\)`,
	})
	if err := lp.Save(gwRoot, "demo"); err != nil {
		t.Fatal(err)
	}

	// 5. Working clone; push a commit with a .go file tripping the check.
	work := t.TempDir()
	mustGit(t, work, "init", "-q", work)
	mustGit(t, work, "remote", "add", "origin", gatewayBare)
	if err := os.WriteFile(filepath.Join(work, "main.go"),
		[]byte("package main\n\n// TODO(no-owner) fix this\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "main.go")
	mustGit(t, work, "commit", "-qm", "add code with an unowned TODO")
	todoSHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))

	// 6. WARN is non-blocking → push must be ACCEPTED and relayed upstream.
	if out, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("push tripping a WARN check should be ACCEPTED: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != todoSHA {
		t.Fatalf("upstream main = %q after accepted push, want relayed %q", got, todoSHA)
	}

	// 7. Read the repo's audit log (<policyRoot>/<repo>/audit.log) and assert an
	// accepted record carries the WARN finding for the authored check.
	auditPath := filepath.Join(gwRoot, "demo", "audit.log")
	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit log %s: %v", auditPath, err)
	}
	defer f.Close()
	var foundWARN bool
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("audit line not valid JSON: %v\n%s", err, sc.Text())
		}
		if !rec.Accept {
			continue
		}
		for _, fnd := range rec.Findings {
			if fnd.ID == "app-correctness/no-owner-todos" && fnd.Severity == "WARN" {
				foundWARN = true
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan audit log: %v", err)
	}
	if !foundWARN {
		t.Fatalf("audit log %s has no accepted record with a WARN finding app-correctness/no-owner-todos", auditPath)
	}
}

// TestEndToEnd_ignoreMarkerCannotBypass is the regression guard for the
// scan-suppression bypass: a push that includes a .appframes-ignore file
// containing "*.pem" must still be rejected when it also contains a private
// key. The gateway must strip pushed ignore markers before scanning.
func TestEndToEnd_ignoreMarkerCannotBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e real-git harness skipped in -short mode")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	// 1. Build the nimblegate binary.
	exe := filepath.Join(t.TempDir(), "nimblegate")
	build := exec.Command("go", "build", "-o", exe, "./cmd/nimblegate")
	build.Dir = repoRoot
	build.Env = e2eEnv()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build nimblegate: %v\n%s", err, out)
	}

	// 2. Scratch upstream bare repo.
	upstreamDir := t.TempDir()
	mustGit(t, upstreamDir, "init", "--bare", "-q", upstreamDir)

	// 3. Register gateway repo protecting refs/heads/main with @tier-1.
	gwRoot := t.TempDir()
	reposRoot := t.TempDir()
	add := exec.Command(exe, "gateway", "add",
		"--name", "bypass-demo",
		"--upstream", "file://"+upstreamDir,
		"--protect", "refs/heads/main",
		"--policy-root", gwRoot,
		"--repos-root", reposRoot)
	add.Env = e2eEnv()
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("gateway add: %v\n%s", err, out)
	}
	gatewayBare := filepath.Join(reposRoot, "bypass-demo.git")

	// 4. Gateway-held policy: core catastrophic-prevention frames (includes no-private-keys-in-repo).
	policyDir := filepath.Join(gwRoot, "bypass-demo")
	if err := os.MkdirAll(policyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"),
		[]byte("[frames]\nenabled = [\n    \"security/no-private-keys-in-repo\",\n    \"security/no-hardcoded-credentials\",\n    \"git/no-bypass-pre-commit\",\n    \"git/no-force-push-main\",\n    \"git/no-amend-pushed-commits\",\n    \"git/folder-branch-lock\",\n    \"filesystem/rm-rf-protected-paths\",\n    \"commands/curl-pipe-shell\",\n    \"commands/apt-purge-preview\",\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 5. Working clone with a clean commit on main so upstream is not empty.
	work := t.TempDir()
	mustGit(t, work, "init", "-q", work)
	mustGit(t, work, "remote", "add", "origin", gatewayBare)
	if err := os.WriteFile(filepath.Join(work, "ok.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "ok.txt")
	mustGit(t, work, "commit", "-qm", "baseline")
	cleanSHA := strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	if out, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("baseline push should succeed: %v\n%s", err, out)
	}

	// 6. Push a commit containing BOTH a private key AND a .appframes-ignore
	//    that tries to suppress *.pem scanning.
	if err := os.WriteFile(filepath.Join(work, "leak.pem"),
		[]byte("-----BEGIN OPENSSH PRIVATE KEY-----\nZmFrZQ==\n-----END OPENSSH PRIVATE KEY-----\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".appframes-ignore"), []byte("*.pem\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit(t, work, "add", "leak.pem", ".appframes-ignore")
	mustGit(t, work, "commit", "-qm", "attempt bypass via .appframes-ignore")

	bypassOut, err := tryGit(work, "push", "origin", "HEAD:refs/heads/main")
	if err == nil {
		t.Fatalf("bypass push should be REJECTED but was accepted\n%s", bypassOut)
	}
	if !strings.Contains(bypassOut, "no-private-keys-in-repo") {
		t.Fatalf("rejection output should name no-private-keys-in-repo; got:\n%s", bypassOut)
	}
	if !strings.Contains(bypassOut, "rejected") {
		t.Fatalf("rejection output should say 'rejected'; got:\n%s", bypassOut)
	}

	// Upstream main must remain on the clean baseline - the bypass commit did not relay.
	if got := strings.TrimSpace(mustGit(t, upstreamDir, "--git-dir", upstreamDir, "rev-parse", "refs/heads/main")); got != cleanSHA {
		t.Fatalf("upstream main = %q after rejected bypass push, want unchanged %q", got, cleanSHA)
	}
}
