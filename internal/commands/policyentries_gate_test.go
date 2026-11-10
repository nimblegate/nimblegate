// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway"
)

// fakeAWSKey returns a fresh AWS-format access key id, so the fixture never
// matches a known example key a frame might allowlist.
func fakeAWSKey(t *testing.T) string {
	t.Helper()
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "AKIA" + string(b)
}

// gateRepoWithSecret builds a bare repo whose main holds a file with a fresh
// AWS key, plus a policy dir holding the given appframes.toml.
func gateRepoWithSecret(t *testing.T, policyToml string) (bare, sha, policyDir string) {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(work, "init", "-q", "-b", "main")
	src := "package main\n\nconst awsKey = \"" + fakeAWSKey(t) + "\"\n"
	if err := os.WriteFile(filepath.Join(work, "config.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	run(work, "add", "-A")
	run(work, "commit", "-qm", "init")
	bare = filepath.Join(t.TempDir(), "r.git")
	run(work, "clone", "-q", "--bare", work, bare)
	policyDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"), []byte(policyToml), 0o644); err != nil {
		t.Fatal(err)
	}
	return bare, run(work, "rev-parse", "HEAD"), policyDir
}

func runGate(t *testing.T, policyToml string, observe bool) (int, string, []gateway.AuditRecord) {
	t.Helper()
	bare, sha, policyDir := gateRepoWithSecret(t, policyToml)
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	d := gateway.PreReceiveDeps{
		Policy: gateway.Policy{
			Repo: "r", Enabled: true, Observe: observe,
			ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: policyDir,
		},
		GitDir:     bare,
		Checker:    engineChecker{},
		AuditPath:  auditPath,
		ScanTmpDir: t.TempDir(),
	}
	stdin := strings.NewReader("0000000000000000000000000000000000000000 " + sha + " refs/heads/main\n")
	var out bytes.Buffer
	code := gateway.RunPreReceive(d, stdin, &out)
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	var recs []gateway.AuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var r gateway.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("audit line %q: %v", line, err)
		}
		recs = append(recs, r)
	}
	return code, out.String(), recs
}

func findingSeverity(recs []gateway.AuditRecord, id string) (string, string) {
	for _, r := range recs {
		for _, f := range r.Findings {
			if f.ID == id {
				return f.Severity, f.Message
			}
		}
	}
	return "", ""
}

func TestGate_GroupOnlyPolicyRejects(t *testing.T) {
	code, out, recs := runGate(t, "[frames]\nenabled = [\"@tier-1\"]\n", false)
	if code == 0 {
		t.Fatalf("push with a secret under an `@tier-1`-only policy was accepted; output %q", out)
	}
	sev, msg := findingSeverity(recs, engine.PolicyEntriesID)
	if sev != "BLOCK" || !strings.Contains(msg, "@tier-1") || !strings.Contains(msg, "nimblegate lint") {
		t.Errorf("want BLOCK policy/unknown-entries naming @tier-1 with the lint hint, got %q %q", sev, msg)
	}
	if strings.Contains(out, "nimblegate") || strings.Contains(out, "@tier-1") {
		t.Errorf("pusher-facing output must stay neutral, got %q", out)
	}
}

func TestGate_TypoWarnsAndValidFrameStillBlocks(t *testing.T) {
	code, out, recs := runGate(t, "[frames]\nenabled = [\"security/no-hardcoded-credentials\", \"security/no-hardcoded-credential\"]\n", false)
	if code == 0 {
		t.Fatalf("the real frame should still block the key; output %q", out)
	}
	if sev, _ := findingSeverity(recs, "security/no-hardcoded-credentials"); sev != "BLOCK" {
		t.Errorf("want security/no-hardcoded-credentials BLOCK, got %q", sev)
	}
	sev, msg := findingSeverity(recs, engine.PolicyEntriesID)
	if sev != "WARN" || !strings.Contains(msg, "security/no-hardcoded-credential") || strings.Contains(msg, "no checks") {
		t.Errorf("want WARN naming the typo, got %q %q", sev, msg)
	}
	if strings.Contains(out, engine.PolicyEntriesID) {
		t.Errorf("a WARN must not reach the pusher: %q", out)
	}
}

func TestGate_EmptyListRunsAllFramesWithoutWarning(t *testing.T) {
	code, _, recs := runGate(t, "[frames]\nenabled = []\n", false)
	if code == 0 {
		t.Fatal("an empty list means every frame; the key should block")
	}
	if sev, _ := findingSeverity(recs, engine.PolicyEntriesID); sev != "" {
		t.Errorf("empty list must not warn, got %q", sev)
	}
}

func TestGate_GroupOnlyPolicyUnderObserveIsRecordedAndRelayed(t *testing.T) {
	code, out, recs := runGate(t, "[frames]\nenabled = [\"@tier-1\"]\n", true)
	if code != 0 || out != "" {
		t.Fatalf("observe mode relays silently; got code %d output %q", code, out)
	}
	if len(recs) != 1 || !recs[0].Observed {
		t.Fatalf("want one observed audit record, got %+v", recs)
	}
	if sev, _ := findingSeverity(recs, engine.PolicyEntriesID); sev != "BLOCK" {
		t.Errorf("want the BLOCK recorded, got %q", sev)
	}
}

func TestPreview_ShowsUnknownEntries(t *testing.T) {
	bare, _, policyDir := gateRepoWithSecret(t, "[frames]\nenabled = [\"security/no-hardcoded-credentials\"]\n")
	preview := func(enabled []string) gateway.PreviewResult {
		return gateway.PreviewPolicy(gateway.PreviewInput{
			Policy:     gateway.Policy{Repo: "r", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: policyDir},
			GitDir:     bare,
			Checker:    engineChecker{},
			Enabled:    enabled,
			ScanTmpDir: t.TempDir(),
		})
	}
	has := func(rows []gateway.PreviewFinding, frame string) bool {
		for _, r := range rows {
			if r.Frame == frame {
				return true
			}
		}
		return false
	}

	groups := preview([]string{"@tier-1"})
	if !groups.WouldReject() || !has(groups.Blocks, engine.PolicyEntriesID) {
		t.Errorf("group-only list: want a %s block, got %+v", engine.PolicyEntriesID, groups)
	}
	typo := preview([]string{"security/no-hardcoded-credentials", "security/no-hardcoded-credential"})
	if !has(typo.Warns, engine.PolicyEntriesID) || has(typo.Blocks, engine.PolicyEntriesID) || !has(typo.Blocks, "security/no-hardcoded-credentials") {
		t.Errorf("typo list: want the frame block plus a %s warn, got %+v", engine.PolicyEntriesID, typo)
	}
}

func TestPolicyPage_UnknownEntriesBanner(t *testing.T) {
	render := func(enabled []string) string {
		root := t.TempDir()
		_ = gateway.FramePolicy{Enabled: enabled}.Save(root, "demo")
		_ = (gateway.FilePolicyStore{Root: root}).Save(gateway.Policy{Repo: "demo", UpstreamURL: "u", Enabled: true})
		var buf bytes.Buffer
		if err := renderPolicyPage(&buf, buildPolicyView(root, "demo", enabled), policyPageOpts{Repos: []string{"demo"}, PolicyRoot: root}); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}

	none := render([]string{"@tier-1", "@web"})
	for _, want := range []string{"gw-unknown-entries", "enables no checks", "<code>@tier-1</code>", "<code>@web</code>", "nimblegate lint"} {
		if !strings.Contains(none, want) {
			t.Errorf("group-only banner missing %q", want)
		}
	}
	typo := render([]string{"git/no-force-push-main", "git/no-force-push-mian"})
	if !strings.Contains(typo, "<code>git/no-force-push-mian</code>") || strings.Contains(typo, "enables no checks") || strings.Contains(typo, "nimblegate lint") {
		t.Error("typo banner should name the entry as a warning only, without the group hint")
	}
	for _, enabled := range [][]string{{"git/no-force-push-main", "security/*"}, nil} {
		if strings.Contains(render(enabled), "gw-unknown-entries") {
			t.Errorf("no banner expected for %q", enabled)
		}
	}
}

func runCheckCLI(t *testing.T, enabled string) (int, string) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"appframes.toml": "[frames]\nenabled = " + enabled + "\n",
		"config.go":      "package main\n\nconst awsKey = \"" + fakeAWSKey(t) + "\"\n",
	})
	t.Setenv("HOME", t.TempDir())
	t.Chdir(root)
	var rc int
	out := captureStdout(t, func() int { rc = Check([]string{"--no-linters"}); return rc })
	return rc, out
}

func TestCheckCLI_UnknownEntries(t *testing.T) {
	rc, out := runCheckCLI(t, `["@tier-1"]`)
	if rc == 0 || !strings.Contains(out, "policy enables no checks") || !strings.Contains(out, "@tier-1") {
		t.Errorf("group-only: want non-zero exit and the no-checks line, got %d\n%s", rc, out)
	}

	rc, out = runCheckCLI(t, `["git/no-force-push-main", "git/no-force-push-mian"]`)
	if rc != 0 || !strings.Contains(out, engine.PolicyEntriesID) || !strings.Contains(out, "git/no-force-push-mian") {
		t.Errorf("typo beside a valid frame: want exit 0 with the warning, got %d\n%s", rc, out)
	}

	rc, out = runCheckCLI(t, `["security/no-hardcoded-credentials"]`)
	if rc == 0 || strings.Contains(out, engine.PolicyEntriesID) {
		t.Errorf("valid list: want the frame to block and no policy warning, got %d\n%s", rc, out)
	}
}
