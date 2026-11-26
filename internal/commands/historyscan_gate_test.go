// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// historyRepo is a work repo plus a bare gateway repo holding its first commit
// under refs/heads/main, as registration leaves it.
type historyRepo struct {
	t               *testing.T
	work, bare, pol string
}

func newHistoryRepo(t *testing.T, policyToml string) *historyRepo {
	t.Helper()
	h := &historyRepo{t: t, work: t.TempDir(), pol: t.TempDir()}
	h.git(h.work, "init", "-q", "-b", "main")
	h.commit("README.md", "hello\n", "init")
	h.bare = filepath.Join(t.TempDir(), "r.git")
	h.git(h.work, "clone", "-q", "--bare", h.work, h.bare)
	if err := os.WriteFile(filepath.Join(h.pol, "appframes.toml"), []byte(policyToml), 0o644); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *historyRepo) git(dir string, args ...string) string {
	h.t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (h *historyRepo) commit(path, content, msg string) string {
	h.t.Helper()
	p := filepath.Join(h.work, path)
	if content == "" {
		h.git(h.work, "rm", "-q", path)
	} else {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			h.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			h.t.Fatal(err)
		}
		h.git(h.work, "add", path)
	}
	h.git(h.work, "commit", "-qm", msg)
	return h.git(h.work, "rev-parse", "HEAD")
}

// push stages the work repo's main into the bare repo the way receive-pack
// does before pre-receive: objects present, ref not yet moved.
func (h *historyRepo) push(observe bool) (int, string) {
	h.t.Helper()
	old := h.git(h.bare, "rev-parse", "refs/heads/main")
	h.git(h.bare, "fetch", "-q", h.work, "main")
	newRev := h.git(h.work, "rev-parse", "HEAD")
	d := gateway.PreReceiveDeps{
		Policy: gateway.Policy{
			Repo: "r", Enabled: true, Observe: observe,
			ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: h.pol,
		},
		GitDir:     h.bare,
		Checker:    engineChecker{},
		AuditPath:  filepath.Join(h.t.TempDir(), "audit.jsonl"),
		ScanTmpDir: h.t.TempDir(),
	}
	var out bytes.Buffer
	code := gateway.RunPreReceive(d, strings.NewReader(old+" "+newRev+" refs/heads/main\n"), &out)
	return code, out.String()
}

// accept moves the bare ref, as receive-pack does after pre-receive passes.
func (h *historyRepo) accept() {
	h.git(h.bare, "update-ref", "refs/heads/main", h.git(h.work, "rev-parse", "HEAD"))
}

const credsPolicy = "[frames]\nenabled = [\"security/no-hardcoded-credentials\"]\n"

func TestHistoryScan_SecretAddedThenRemovedIsBlocked(t *testing.T) {
	h := newHistoryRepo(t, credsPolicy)
	leak := h.commit("config.go", "package main\n\nconst k = \""+fakeAWSKey(t)+"\"\n", "add config")
	h.commit("config.go", "", "remove config")

	code, out := h.push(false)
	if code == 0 {
		t.Fatalf("a secret in an earlier commit of the push must block; output:\n%s", out)
	}
	if !strings.Contains(out, "security/no-hardcoded-credentials") || !strings.Contains(out, "config.go") {
		t.Errorf("rejection should name the frame and file:\n%s", out)
	}
	if !strings.Contains(out, leak[:10]) || !strings.Contains(out, "remove it from history") {
		t.Errorf("rejection should name commit %s and say to rewrite history:\n%s", leak[:10], out)
	}
}

func TestHistoryScan_ObserveRecordsAndRelays(t *testing.T) {
	h := newHistoryRepo(t, credsPolicy)
	h.commit("config.go", "package main\n\nconst k = \""+fakeAWSKey(t)+"\"\n", "add config")
	h.commit("config.go", "", "remove config")
	if code, out := h.push(true); code != 0 || out != "" {
		t.Fatalf("observe relays silently; got %d %q", code, out)
	}
}

func TestHistoryScan_WhitelistedPathPasses(t *testing.T) {
	h := newHistoryRepo(t, credsPolicy)
	wl := filepath.Join(h.pol, ".appframes", "_canonical")
	if err := os.MkdirAll(wl, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := "[[entry]]\nframe = \"security/no-hardcoded-credentials\"\npath = \"fixtures/config.go\"\nreason = \"test fixture\"\n"
	if err := os.WriteFile(filepath.Join(wl, "whitelist.toml"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}
	h.commit("fixtures/config.go", "package main\n\nconst k = \""+fakeAWSKey(t)+"\"\n", "add fixture")
	h.commit("fixtures/config.go", "", "remove fixture")
	if code, out := h.push(false); code != 0 {
		t.Fatalf("a whitelisted path must pass in history too:\n%s", out)
	}
}

func TestHistoryScan_HistoryAlreadyOnGatewayIsNotRescanned(t *testing.T) {
	h := newHistoryRepo(t, credsPolicy)
	h.commit("config.go", "package main\n\nconst k = \""+fakeAWSKey(t)+"\"\n", "add config")
	h.commit("config.go", "", "remove config")
	// Already relayed earlier (or imported from upstream at registration).
	h.git(h.bare, "fetch", "-q", h.work, "main:refs/heads/main")

	h.commit("notes.md", "clean change\n", "clean")
	if code, out := h.push(false); code != 0 {
		t.Fatalf("history the gateway already holds must not block a new push:\n%s", out)
	}
}

func TestHistoryScan_NewBranchScansOnlyItsOwnCommits(t *testing.T) {
	h := newHistoryRepo(t, credsPolicy)
	h.commit("config.go", "package main\n\nconst k = \""+fakeAWSKey(t)+"\"\n", "add config")
	h.commit("config.go", "fixed\n", "replace config")
	if code, _ := h.push(false); code == 0 {
		t.Fatal("a secret replaced within the push must block")
	}
	h.git(h.work, "reset", "-q", "--hard", "HEAD~2")
	h.commit("app.go", "package main\n", "clean app")
	code, out := h.push(false)
	if code != 0 {
		t.Fatalf("after rewriting history the push must pass:\n%s", out)
	}
	h.accept()
}

func TestHistoryScan_StyleFramesDoNotRunOnHistory(t *testing.T) {
	h := newHistoryRepo(t, "[frames]\nenabled = [\"security/no-hardcoded-credentials\", \"encoding/no-bom\"]\n")
	h.commit("a.txt", "\xef\xbb\xbfbom file\n", "bom")
	h.commit("a.txt", "fixed\n", "fix bom")
	if code, out := h.push(false); code != 0 {
		t.Fatalf("a non-secret finding in history must not block:\n%s", out)
	}
}
