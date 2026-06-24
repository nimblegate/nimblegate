// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skeletonRoots stands up a paired policy-root + repos-root pair under t.TempDir
// matching the on-disk layout AddRepo writes - both with the _repos/ container.
func skeletonRoots(t *testing.T) (policyRoot, reposRoot string) {
	t.Helper()
	tmp := t.TempDir()
	policyRoot = filepath.Join(tmp, "cfg")
	reposRoot = filepath.Join(tmp, "repos")
	for _, d := range []string{policyRoot, reposRoot, filepath.Join(policyRoot, "_repos"), filepath.Join(reposRoot, "_repos")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	return policyRoot, reposRoot
}

// addRepoForTest stands up a freshly-registered repo via the real AddRepo path
// so the test exercises exactly what production does. Returns selfExe path
// (irrelevant for these tests - the hooks are written, not run).
func addRepoForTest(t *testing.T, policyRoot, reposRoot, name, upstreamURL string) {
	t.Helper()
	err := AddRepo(AddOptions{
		Name:          name,
		UpstreamURL:   upstreamURL,
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		Observe:       false,
		PolicyRoot:    policyRoot,
		ReposRoot:     reposRoot,
		SelfExe:       "/usr/local/bin/nimblegate",
	})
	if err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
}

func TestSkeletonGenerateSeedsAppframesTOML(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "demo"), filepath.Join(policyRoot, "demo")); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	if err := sk.Generate("demo"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(policyRoot, "demo", "appframes.toml"))
	if err != nil {
		t.Fatalf("read seeded file: %v", err)
	}
	if !strings.Contains(string(body), "[frames]") {
		t.Errorf("seeded appframes.toml missing [frames] section: %q", body)
	}
}

func TestSkeletonGenerateIdempotent(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("_repos", "demo"), filepath.Join(policyRoot, "demo")); err != nil {
		t.Fatal(err)
	}
	preExisting := []byte("[frames]\nenabled = [\"already/here\"]\n")
	if err := os.WriteFile(filepath.Join(policyRoot, "demo", "appframes.toml"), preExisting, 0o644); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	if err := sk.Generate("demo"); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(policyRoot, "demo", "appframes.toml"))
	if string(body) != string(preExisting) {
		t.Errorf("Generate overwrote existing file: want %q, got %q", preExisting, body)
	}
}

// TestSkeletonAddRepoLeavesNoIssuesForSSH is the regression gate for the
// per [[self-enforcing-security-patterns]] contract: any future addition
// of a required file MUST be caught here by `AddRepo` failing to satisfy
// `Verify`. If you add a file to the skeleton spec, AddRepo (via Generate)
// must seed it. SSH upstream sidesteps the credential check.
func TestSkeletonAddRepoLeavesNoIssuesForSSH(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	issues, err := sk.Verify("demo")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("Verify after AddRepo with SSH upstream returned %d issue(s); want 0: %+v", len(issues), issues)
	}
}

// TestSkeletonAddRepoLeavesOneIssueForHTTPWithoutCred is the credential-leg
// counterpart: with an HTTP upstream and no credential file, Verify surfaces
// exactly the missing-credential blocking issue - and no other.
func TestSkeletonAddRepoLeavesOneIssueForHTTPWithoutCred(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "http://192.0.2.20:3000/you/demo.git")
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	issues, err := sk.Verify("demo")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(issues) != 1 || issues[0].File != "credential" || issues[0].Severity != IssueBlocking {
		t.Errorf("Verify after AddRepo with HTTP upstream + no cred: want 1 blocking 'credential' issue, got %+v", issues)
	}
}

// TestSkeletonAddRepoCleanWhenCredInstalled completes the trio: HTTP upstream
// with a credential file installed → Verify clean.
func TestSkeletonAddRepoCleanWhenCredInstalled(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "http://192.0.2.20:3000/you/demo.git")
	credPath := filepath.Join(policyRoot, "demo", "credential")
	if err := os.WriteFile(credPath, []byte("user:pat-abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	issues, err := sk.Verify("demo")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("Verify after AddRepo with HTTP upstream + cred installed returned %d issue(s); want 0: %+v", len(issues), issues)
	}
}

func TestSkeletonVerifyDetectsMissingAppframesTOML(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	if err := os.Remove(filepath.Join(policyRoot, "demo", "appframes.toml")); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	issues, err := sk.Verify("demo")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(issues) != 1 || issues[0].File != "appframes.toml" || issues[0].Repair != "regen-nimblegate-toml" {
		t.Errorf("Verify with missing appframes.toml: want 1 'appframes.toml' issue with regen repair, got %+v", issues)
	}
}

func TestSkeletonVerifyDetectsMissingBareRepo(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	if err := os.RemoveAll(filepath.Join(reposRoot, "_repos", "demo.git")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(reposRoot, "demo.git")); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	issues, err := sk.Verify("demo")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	var hasBare bool
	for _, iss := range issues {
		if iss.File == "demo.git" && iss.Severity == IssueBlocking {
			hasBare = true
		}
	}
	if !hasBare {
		t.Errorf("Verify with missing bare didn't surface a 'demo.git' blocking issue: %+v", issues)
	}
}

func TestSkeletonRepairRegenAppframesTOML(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	target := filepath.Join(policyRoot, "demo", "appframes.toml")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	if err := sk.Repair("demo", "regen-nimblegate-toml"); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("Repair did not recreate appframes.toml: %v", err)
	}
}

func TestSkeletonRepairUnknownOperation(t *testing.T) {
	policyRoot, reposRoot := skeletonRoots(t)
	addRepoForTest(t, policyRoot, reposRoot, "demo", "git@gitea.internal:you/demo.git")
	sk := Skeleton{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	if err := sk.Repair("demo", "format-c"); err == nil {
		t.Errorf("Repair('format-c') succeeded; want error")
	}
	if err := sk.Repair("demo", ""); err == nil {
		t.Errorf("Repair('') succeeded; want error on empty operation")
	}
}
