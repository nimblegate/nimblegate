// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func doctorSeed(t *testing.T, policyRoot, reposRoot, name string, o AddOptions) {
	t.Helper()
	o.Name = name
	o.PolicyRoot = policyRoot
	o.ReposRoot = reposRoot
	o.Enabled = true
	if o.SelfExe == "" {
		o.SelfExe = "/bin/true"
	}
	if err := AddRepo(o); err != nil {
		t.Fatalf("AddRepo %s: %v", name, err)
	}
}

func doctorEnableFrames(t *testing.T, policyRoot, repo string) {
	t.Helper()
	fp := FramePolicy{Enabled: []string{"secrets/aws-access-key"}, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, repo); err != nil {
		t.Fatalf("enable frames for %s: %v", repo, err)
	}
}

func doctorRoots(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	return policyRoot, reposRoot
}

func findCheck(rep DoctorReport, repo, name string) (DoctorCheck, bool) {
	for _, c := range rep.Checks {
		if c.Repo == repo && c.Name == name {
			return c, true
		}
	}
	return DoctorCheck{}, false
}

func writeKeysFile(t *testing.T, dir, comment string) (string, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	if comment != "" {
		line += " " + comment
	}
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, ssh.FingerprintSHA256(sshPub)
}

func TestRunDoctorGatedRefs(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "ungated", AddOptions{UpstreamURL: "https://github.com/x/ungated.git"})
	doctorSeed(t, policyRoot, reposRoot, "mainonly", AddOptions{UpstreamURL: "https://github.com/x/mainonly.git", ProtectedRefs: []string{"refs/heads/main"}})
	doctorSeed(t, policyRoot, reposRoot, "allrefs", AddOptions{UpstreamURL: "https://github.com/x/allrefs.git", GateAllRefs: true})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	if c, ok := findCheck(rep, "ungated", "Gated refs"); !ok || c.Status != DoctorFail {
		t.Fatalf("ungated: want FAIL, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "mainonly", "Gated refs"); !ok || c.Status != DoctorWarn {
		t.Fatalf("mainonly: want WARN, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "allrefs", "Gated refs"); !ok || c.Status != DoctorOK {
		t.Fatalf("allrefs: want OK, got %+v ok=%v", c, ok)
	}
}

func TestRunDoctorFrames(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "framed", AddOptions{UpstreamURL: "https://github.com/x/framed.git", GateAllRefs: true})
	doctorEnableFrames(t, policyRoot, "framed")
	doctorSeed(t, policyRoot, reposRoot, "unframed", AddOptions{UpstreamURL: "https://github.com/x/unframed.git", GateAllRefs: true})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	if c, ok := findCheck(rep, "framed", "Frames"); !ok || c.Status != DoctorOK {
		t.Fatalf("framed: want OK, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "unframed", "Frames"); !ok || c.Status != DoctorFail {
		t.Fatalf("unframed: want FAIL, got %+v ok=%v", c, ok)
	}
}

func TestRunDoctorHasFail(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	keysPath, _ := writeKeysFile(t, t.TempDir(), "dev@box")
	doctorSeed(t, policyRoot, reposRoot, "clean", AddOptions{UpstreamURL: "https://github.com/x/clean.git", GateAllRefs: true})
	doctorEnableFrames(t, policyRoot, "clean")

	clean := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, AuthorizedKeysPath: keysPath, Offline: true})
	if clean.HasFail {
		for _, c := range clean.Checks {
			if c.Status == DoctorFail {
				t.Logf("unexpected FAIL: %+v", c)
			}
		}
		t.Fatalf("clean config should not have HasFail")
	}

	doctorSeed(t, policyRoot, reposRoot, "broken", AddOptions{UpstreamURL: "https://github.com/x/broken.git"})
	broken := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, AuthorizedKeysPath: keysPath, Offline: true})
	if !broken.HasFail {
		t.Fatalf("ungated repo should set HasFail")
	}
}

func TestRunDoctorAuthorizedKeys(t *testing.T) {
	// Point the bare-metal default at a guaranteed-absent path so the empty /
	// no-path cases deterministically FAIL rather than picking up a real
	// /home/git/.ssh/authorized_keys on the test host.
	orig := bareMetalGitKeys
	bareMetalGitKeys = filepath.Join(t.TempDir(), "absent_git_keys")
	defer func() { bareMetalGitKeys = orig }()

	policyRoot, reposRoot := doctorRoots(t)
	keysPath, wantFP := writeKeysFile(t, t.TempDir(), "alice@box")

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, AuthorizedKeysPath: keysPath, Offline: true})
	if len(rep.Keys) != 1 {
		t.Fatalf("want 1 key, got %d", len(rep.Keys))
	}
	k := rep.Keys[0]
	if k.Fingerprint != wantFP {
		t.Fatalf("fingerprint mismatch: want %s got %s", wantFP, k.Fingerprint)
	}
	if k.Comment != "alice@box" {
		t.Fatalf("comment mismatch: got %q", k.Comment)
	}
	if k.Type != "ssh-ed25519" {
		t.Fatalf("type mismatch: got %q", k.Type)
	}
	if c, ok := findCheck(rep, "", "Authorized keys"); !ok || c.Status != DoctorOK {
		t.Fatalf("authorized-keys check: want OK, got %+v ok=%v", c, ok)
	}

	empty := filepath.Join(t.TempDir(), "empty_keys")
	if err := os.WriteFile(empty, []byte("# only a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repEmpty := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, AuthorizedKeysPath: empty, Offline: true})
	if c, ok := findCheck(repEmpty, "", "Authorized keys"); !ok || c.Status != DoctorFail {
		t.Fatalf("empty keys: want FAIL, got %+v ok=%v", c, ok)
	}

	repNoPath := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})
	if c, ok := findCheck(repNoPath, "", "Authorized keys"); !ok || c.Status != DoctorFail {
		t.Fatalf("no keys path: want FAIL, got %+v ok=%v", c, ok)
	}
}

func TestRunDoctorBareMetalSplit(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	// sshd reads keys from the bare-metal default; the dashboard manages a
	// different, absent path. Expect a WARN with the bridge fix, not a FAIL.
	bmPath, _ := writeKeysFile(t, t.TempDir(), "dev@box")
	orig := bareMetalGitKeys
	bareMetalGitKeys = bmPath
	defer func() { bareMetalGitKeys = orig }()

	dashPath := filepath.Join(t.TempDir(), "authorized_keys")
	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, AuthorizedKeysPath: dashPath, Offline: true})

	c, ok := findCheck(rep, "", "Authorized keys")
	if !ok || c.Status != DoctorWarn {
		t.Fatalf("split: want WARN, got %+v ok=%v", c, ok)
	}
	if len(rep.Keys) != 1 {
		t.Fatalf("split: keys from the sshd path should be surfaced, got %d", len(rep.Keys))
	}
	if !strings.Contains(c.Fix, "ln -sf") {
		t.Fatalf("split WARN should carry the bridge fix, got %q", c.Fix)
	}
	if rep.HasFail {
		t.Fatalf("split is a WARN, not a FAIL")
	}
}

func TestRunDoctorGatePort(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	openPort := ln.Addr().(*net.TCPAddr).Port

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, GatePorts: []int{openPort}})
	if c, ok := findCheck(rep, "", "SSH gate"); !ok || c.Status != DoctorOK {
		t.Fatalf("open gate port: want OK, got %+v ok=%v", c, ok)
	}

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := ln2.Addr().(*net.TCPAddr).Port
	_ = ln2.Close()
	rep2 := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, GatePorts: []int{closedPort}})
	if c, ok := findCheck(rep2, "", "SSH gate"); !ok || c.Status != DoctorWarn {
		t.Fatalf("closed gate port: want WARN, got %+v ok=%v", c, ok)
	}
}

func TestRunDoctorRepoFilter(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "alpha", AddOptions{UpstreamURL: "https://github.com/x/alpha.git", GateAllRefs: true})
	doctorSeed(t, policyRoot, reposRoot, "beta", AddOptions{UpstreamURL: "https://github.com/x/beta.git", GateAllRefs: true})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, RepoFilter: "alpha", Offline: true})
	if _, ok := findCheck(rep, "alpha", "Bare repo"); !ok {
		t.Fatalf("alpha checks should be present")
	}
	if _, ok := findCheck(rep, "beta", "Bare repo"); ok {
		t.Fatalf("beta checks should be filtered out")
	}
}

func TestRunDoctorUpstreamAuthInjection(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "auth", AddOptions{UpstreamURL: "https://github.com/x/auth.git", GateAllRefs: true})

	ok := RunDoctor(DoctorConfig{
		PolicyRoot:        policyRoot,
		ReposRoot:         reposRoot,
		Offline:           false,
		UpstreamAuthCheck: func(_, _ string) error { return nil },
	})
	if c, found := findCheck(ok, "auth", "Upstream auth"); !found || c.Status != DoctorOK {
		t.Fatalf("success path: want OK, got %+v found=%v", c, found)
	}

	bad := RunDoctor(DoctorConfig{
		PolicyRoot:        policyRoot,
		ReposRoot:         reposRoot,
		Offline:           false,
		UpstreamAuthCheck: func(_, _ string) error { return errors.New("403 Forbidden") },
	})
	c, found := findCheck(bad, "auth", "Upstream auth")
	if !found || c.Status != DoctorFail {
		t.Fatalf("error path: want FAIL, got %+v found=%v", c, found)
	}
	if c.Fix == "" {
		t.Fatalf("403 error should attach a scope-hint Fix")
	}
	if !bad.HasFail {
		t.Fatalf("auth FAIL should set HasFail")
	}
}
