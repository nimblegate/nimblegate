// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/gateway"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func curlPipeBlocks(results []engine.CheckResult) bool {
	for _, r := range results {
		if r.FrameID == "commands/curl-pipe-shell" && r.Outcome == engine.OutcomeBlock {
			return true
		}
	}
	return false
}

func TestEngineCheckerAppliesWhitelist(t *testing.T) {
	base := map[string]string{
		"deploy.sh":      "#!/bin/sh\ncurl https://example.com/i.sh | sh\n",
		"appframes.toml": "[frames]\nenabled = [\"commands/curl-pipe-shell\"]\n",
	}

	// Without a whitelist: the frame fires, nothing suppressed.
	root := writeTree(t, base)
	res, supp, err := engineChecker{}.Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if !curlPipeBlocks(res) {
		t.Fatalf("expected curl-pipe-shell BLOCK without whitelist; got %+v", res)
	}
	if len(supp) != 0 {
		t.Errorf("no suppressions expected without whitelist, got %+v", supp)
	}

	// With a gateway-held whitelist exempting deploy.sh: filtered + recorded.
	withWL := map[string]string{}
	for k, v := range base {
		withWL[k] = v
	}
	withWL[".appframes/_canonical/whitelist.toml"] = "[[entry]]\nframe = \"commands/curl-pipe-shell\"\npath = \"deploy.sh\"\nreason = \"intentional installer bootstrap\"\n"
	root2 := writeTree(t, withWL)
	res2, supp2, err := engineChecker{}.Check(root2)
	if err != nil {
		t.Fatal(err)
	}
	if curlPipeBlocks(res2) {
		t.Errorf("curl-pipe-shell should be suppressed by whitelist; still present in %+v", res2)
	}
	if len(supp2) == 0 {
		t.Error("expected the suppression to be recorded in the returned log")
	}
}

func TestEngineCheckerMalformedWhitelistErrors(t *testing.T) {
	root := writeTree(t, map[string]string{
		"appframes.toml":                       "[frames]\nenabled = [\"commands/curl-pipe-shell\"]\n",
		".appframes/_canonical/whitelist.toml": "[[entry]]\nframe = \"commands/curl-pipe-shell\"\n", // missing required reason
	})
	_, _, err := engineChecker{}.Check(root)
	if err == nil {
		t.Error("expected an error for a malformed whitelist (missing reason)")
	}
}

func TestGatewayArchive_CLI(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	rc := gatewayArchive([]string{
		"--name", "foo",
		"--policy-root", policyRoot,
		"--repos-root", reposRoot,
	})
	if rc != 0 {
		t.Fatalf("rc: %d", rc)
	}
	// Symlinks gone.
	if _, err := os.Lstat(filepath.Join(policyRoot, "foo")); err == nil {
		t.Fatal("policy symlink should be gone")
	}
	// Event written.
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "archive" })
	if len(evs) != 1 || evs[0].Repo != "foo" {
		t.Fatalf("event: %+v", evs)
	}
	// _archived.md regenerated.
	if _, err := os.Stat(filepath.Join(policyRoot, "_archived.md")); err != nil {
		t.Fatalf("_archived.md missing: %v", err)
	}
}

func TestGatewayRestore_CLI(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	_ = gateway.AddRepo(gateway.AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	})
	_ = gateway.ArchiveRepo(gateway.ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot})
	rc := gatewayRestore([]string{
		"--name", "foo",
		"--policy-root", policyRoot,
		"--repos-root", reposRoot,
	})
	if rc != 0 {
		t.Fatalf("rc: %d", rc)
	}
	if _, err := os.Lstat(filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatalf("symlink should be back: %v", err)
	}
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "restore" })
	if len(evs) != 1 {
		t.Fatalf("event: %+v", evs)
	}
}

func TestGatewayMigrateLayout_CLI(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	// Pre-migration shape: real dirs at <root>/<name>/.
	if err := os.MkdirAll(filepath.Join(policyRoot, "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "legacy", "gateway.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(reposRoot, "legacy.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	rc := gatewayMigrateLayout([]string{
		"--policy-root", policyRoot,
		"--repos-root", reposRoot,
	})
	if rc != 0 {
		t.Fatalf("rc: %d", rc)
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "legacy", "gateway.toml")); err != nil {
		t.Fatalf("post-migrate lib missing: %v", err)
	}
	fi, _ := os.Lstat(filepath.Join(policyRoot, "legacy"))
	if fi == nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink missing")
	}
	// migrate-layout event (only when work was done).
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "migrate-layout" })
	if len(evs) != 1 {
		t.Fatalf("event: %+v", evs)
	}
}

func TestGatewayRescan_CLI(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	// Set up an active repo with a real bare and one pushed commit.
	if err := gateway.AddRepo(gateway.AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	// Seed a commit into the bare via a working clone.
	work := filepath.Join(tmp, "w")
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
	_ = os.WriteFile(filepath.Join(work, "index.html"), []byte("<html></html>"), 0o644)
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-q", "-m", "x"},
		{"push", "-q", filepath.Join(reposRoot, "foo.git"), "HEAD:refs/heads/main"},
	} {
		c := exec.Command("git", args...)
		c.Dir = work
		_, _ = c.CombinedOutput()
	}
	// Fake scan binary.
	fakeJSON := `{"scanned_at":"2026-05-30T14:30:22Z","tree_ref":"HEAD","recommended_groups":[{"name":"@tier-1","always":true,"would_flag":0}],"dismissed":false}`
	fakeExe := filepath.Join(tmp, "fake")
	if err := os.WriteFile(fakeExe, []byte("#!/bin/sh\ncat <<'EOF'\n"+fakeJSON+"\nEOF\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	rc := gatewayRescan([]string{
		"--name", "foo",
		"--policy-root", policyRoot,
		"--repos-root", reposRoot,
		"--self-exe", fakeExe,
	})
	if rc != 0 {
		t.Fatalf("rc: %d", rc)
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "foo", "scan-recommendation.json")); err != nil {
		t.Fatalf("rec missing: %v", err)
	}
	evs, _ := gateway.ReadEvents(policyRoot, func(e gateway.Event) bool { return e.Event == "scan-rescan" })
	if len(evs) != 1 {
		t.Fatalf("event: %+v", evs)
	}
}
