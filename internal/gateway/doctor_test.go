// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// Must be a REAL stdlib ID: doctor now reports entries that resolve to no
	// frame, and the old fixture used "secrets/aws-access-key", a category that
	// does not exist.
	fp := FramePolicy{Enabled: []string{"security/no-hardcoded-credentials"}, Severity: map[string]string{}}
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

// A pattern that cannot reach nested branch names is a silent hole: those
// pushes relay unchecked and leave no audit row, so doctor is the only place
// an operator can find out. "refs/heads/*" must read as OK now that a trailing
// star spans "/"; a single-segment pattern like "refs/heads/feat-*" must not.
func TestRunDoctorGatedRefsNestedCoverage(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "starall", AddOptions{UpstreamURL: "https://github.com/x/starall.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "flatonly", AddOptions{UpstreamURL: "https://github.com/x/flatonly.git", ProtectedRefs: []string{"refs/heads/feat-*"}})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	if c, ok := findCheck(rep, "starall", "Gated refs"); !ok || c.Status != DoctorOK {
		t.Fatalf("starall: want OK, got %+v ok=%v", c, ok)
	}
	c, ok := findCheck(rep, "flatonly", "Gated refs")
	if !ok || c.Status != DoctorFail {
		t.Fatalf("flatonly: want FAIL, got %+v ok=%v", c, ok)
	}
	if !strings.Contains(c.Reason, "nested") || c.Fix == "" {
		t.Errorf("flatonly: reason should name the nested-branch gap and offer a fix, got %+v", c)
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
	// An empty [frames] enabled is NOT "nothing active": engine.isFrameEnabled
	// treats an empty allowlist as every stdlib frame. This check used to report
	// FAIL "pushes relay unchecked", whose obvious remedy - apply a kit - is the
	// one action that actually reduces coverage.
	c, ok := findCheck(rep, "unframed", "Frames")
	if !ok || c.Status != DoctorOK {
		t.Fatalf("unframed: want OK, got %+v ok=%v", c, ok)
	}
	if !strings.Contains(c.Reason, "every stdlib frame") {
		t.Errorf("unframed: reason should say an empty selection runs every frame, got %q", c.Reason)
	}
}

func TestRunDoctorRelay(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "norelay", AddOptions{UpstreamURL: "https://github.com/x/norelay.git", GateAllRefs: true})
	doctorSeed(t, policyRoot, reposRoot, "healthy", AddOptions{UpstreamURL: "https://github.com/x/healthy.git", GateAllRefs: true})
	doctorSeed(t, policyRoot, reposRoot, "drifted", AddOptions{UpstreamURL: "https://github.com/x/drifted.git", GateAllRefs: true})
	doctorSeed(t, policyRoot, reposRoot, "failing", AddOptions{UpstreamURL: "https://github.com/x/failing.git", GateAllRefs: true})

	now := time.Now()
	if err := WriteRelayStatus(policyRoot, "healthy", RelayStatus{LastAttempt: now, LastSuccess: now, OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRelayStatus(policyRoot, "drifted", RelayStatus{LastAttempt: now, LastSuccess: now, OK: true, DriftedRefs: 3}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRelayStatus(policyRoot, "failing", RelayStatus{LastAttempt: now, OK: false, Error: "upstream auth failed"}); err != nil {
		t.Fatal(err)
	}

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	if c, ok := findCheck(rep, "norelay", "Relay"); !ok || c.Status != DoctorInfo {
		t.Fatalf("norelay: want INFO, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "healthy", "Relay"); !ok || c.Status != DoctorOK {
		t.Fatalf("healthy: want OK, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "drifted", "Relay"); !ok || c.Status != DoctorWarn {
		t.Fatalf("drifted: want WARN, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "failing", "Relay"); !ok || c.Status != DoctorFail {
		t.Fatalf("failing: want FAIL, got %+v ok=%v", c, ok)
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
	c, ok := findCheck(rep, "", "SSH gate")
	if !ok || c.Status != DoctorOK {
		t.Fatalf("open gate port: want OK, got %+v ok=%v", c, ok)
	}
	// The probe sees the port sshd listens on inside the container, which is not
	// the published port the operator pushes to. Only the Connect block prints a
	// push URL, so this line must not advise a port.
	if strings.Contains(c.Reason, "push to this port") {
		t.Fatalf("gate port reason advises pushing to the probed port: %q", c.Reason)
	}

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := ln2.Addr().(*net.TCPAddr).Port
	_ = ln2.Close()
	rep2 := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, GatePorts: []int{closedPort}})
	if c, ok = findCheck(rep2, "", "SSH gate"); !ok || c.Status != DoctorWarn {
		t.Fatalf("closed gate port: want WARN, got %+v ok=%v", c, ok)
	}
}

// The live push path records relay-ok/relay-failed on every push in both
// install shapes; the container has no other relay signal because it runs no
// reconcile backstop. Doctor must read it, or it reports "nothing relayed yet"
// on a gateway that has been relaying all along.
func TestRunDoctorRelayFromPushEvents(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	for _, n := range []string{"pushed", "lost", "stale-ok"} {
		doctorSeed(t, policyRoot, reposRoot, n, AddOptions{UpstreamURL: "https://github.com/x/" + n + ".git", GateAllRefs: true})
	}
	for _, e := range []Event{
		{Event: "relay-ok", Repo: "pushed", OK: true},
		{Event: "relay-ok", Repo: "lost", OK: true},
		{Event: "relay-failed", Repo: "lost", OK: false},
		{Event: "relay-failed", Repo: "stale-ok", OK: false},
	} {
		if err := AppendEvent(policyRoot, e); err != nil {
			t.Fatal(err)
		}
	}
	// A backstop record saying "fine" must not mask a live push that never
	// landed: the gate accepted something the upstream never received.
	if err := WriteRelayStatus(policyRoot, "stale-ok", RelayStatus{OK: true}); err != nil {
		t.Fatal(err)
	}

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileContainer})
	if c, ok := findCheck(rep, "pushed", "Relay"); !ok || c.Status != DoctorOK {
		t.Errorf("relayed push: want OK, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "lost", "Relay"); !ok || c.Status != DoctorFail {
		t.Errorf("last push failed to relay: want FAIL, got %+v ok=%v", c, ok)
	}
	if c, ok := findCheck(rep, "stale-ok", "Relay"); !ok || c.Status != DoctorFail {
		t.Errorf("failed push under an OK backstop record: want FAIL, got %+v ok=%v", c, ok)
	}
}

// The backstop is one service for the whole install, so it is reported once -
// and only by shapes that have one to start.
func TestRunDoctorRelayBackstopIsShapeSpecific(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "alpha", AddOptions{UpstreamURL: "https://github.com/x/alpha.git", GateAllRefs: true})

	bare := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileBareMetal})
	c, ok := findCheck(bare, "", "Relay retry")
	if !ok || c.Status != DoctorInfo {
		t.Fatalf("bare metal with no reconcile record: want INFO, got %+v ok=%v", c, ok)
	}
	// No remediation: enabling the unit without provisioning the relay user
	// leaves a service that silently reconciles nothing.
	if c.Fix != "" {
		t.Errorf("backstop check should carry no command, got %q", c.Fix)
	}
	// And no claim about the service: doctor sees missing records, not a stopped
	// process. A running backstop that cannot read its policy files also has none.
	if strings.Contains(c.Reason, "not running") {
		t.Errorf("the check must not assert the service state: %q", c.Reason)
	}
	// Written for someone meeting the tool for the first time: it has to say
	// pushes still work before it says what is missing.
	for _, jargon := range []string{"reconcile", "backstop", "drift", "ref "} {
		if strings.Contains(strings.ToLower(c.Reason), jargon) {
			t.Errorf("reason uses internal vocabulary %q: %s", jargon, c.Reason)
		}
	}

	if _, ok := findCheck(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileContainer}), "", "Relay retry"); ok {
		t.Error("container runs no backstop; it must not be told to start one")
	}

	if err := WriteRelayStatus(policyRoot, "alpha", RelayStatus{OK: true}); err != nil {
		t.Fatal(err)
	}
	if c, ok := findCheck(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileBareMetal}), "", "Relay retry"); !ok || c.Status != DoctorOK {
		t.Errorf("reconcile record present: want OK, got %+v ok=%v", c, ok)
	}
}

// Remediation must match the install doctor is running on, never the other one.
func TestRunDoctorAdviceMatchesProfile(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	for _, tc := range []struct {
		prof    InstallProfile
		wantFix string
	}{
		{ProfileBareMetal, ProfileBareMetal.BindFix},
		{ProfileContainer, ProfileContainer.BindFix},
	} {
		rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Host: "127.0.0.1", Offline: true, Profile: tc.prof})
		c, ok := findCheck(rep, "", "Dashboard bind host")
		if !ok || c.Fix != tc.wantFix {
			t.Errorf("%s: bind fix should be the profile's, got %+v ok=%v", tc.prof.Name, c, ok)
		}
		if c, ok := findCheck(rep, "", "Install"); !ok || !strings.Contains(c.Reason, tc.prof.Name) {
			t.Errorf("%s: report should name the install shape, got %+v ok=%v", tc.prof.Name, c, ok)
		}
	}

	// Undeclared and undetected: say so rather than picking a shape.
	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileUnknown})
	if c, ok := findCheck(rep, "", "Install"); !ok || c.Status != DoctorInfo || c.Fix == "" {
		t.Errorf("unknown shape should prompt for a declaration, got %+v ok=%v", c, ok)
	}
}

// An operator who moved sshd declares the port; the probe has to use it, or it
// reports a working gate as unreachable - and on a busy host it can reach
// something unrelated on 2222 and call that the gate.
func TestDefaultGatePorts(t *testing.T) {
	for _, tc := range []struct {
		declared int
		want     []int
	}{
		{0, []int{2222, 22}},
		{2022, []int{2022, 2222, 22}},
		{2222, []int{2222, 22}},
		{22, []int{22, 2222}},
	} {
		got := defaultGatePorts(tc.declared)
		if len(got) != len(tc.want) {
			t.Errorf("declared %d: got %v want %v", tc.declared, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("declared %d: got %v want %v", tc.declared, got, tc.want)
				break
			}
		}
	}
}

func TestRunDoctorProbesDeclaredPort(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	moved := ln.Addr().(*net.TCPAddr).Port

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, PushPort: moved, Profile: ProfileBareMetal})
	c, ok := findCheck(rep, "", "SSH gate")
	if !ok || c.Status != DoctorOK {
		t.Fatalf("declared port should be probed: got %+v ok=%v", c, ok)
	}
	if !strings.Contains(c.Reason, fmt.Sprintf(":%d", moved)) {
		t.Errorf("probe should report the declared port, got %q", c.Reason)
	}
}

// The gate loads the repo's whitelist before any frame runs and refuses the
// push when it cannot, reporting only a bare "rejected" to the pusher because
// the cause names gateway internals. Removing a linter while its suppressions
// remained did exactly that here: every push failed and the reason was only in
// the events file. Doctor now says it in one line.
func TestRunDoctorWhitelist(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	for _, n := range []string{"stale", "valid", "linter-entry", "absent"} {
		doctorSeed(t, policyRoot, reposRoot, n, AddOptions{UpstreamURL: "https://github.com/x/" + n + ".git", GateAllRefs: true})
	}
	writeWL := func(repo, body string) {
		t.Helper()
		dir := filepath.Join(policyRoot, repo, ".appframes", "_canonical")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "whitelist.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeWL("stale", "[[entry]]\nframe = \"app-correctness/todo-markers\"\npath = \"appframes.toml\"\nreason = \"linter since removed\"\n")
	writeWL("valid", "[[entry]]\nframe = \"security/no-hardcoded-credentials\"\npath = \"internal/checks/*_test.go\"\nreason = \"fixtures are fake by design\"\n")
	writeWL("linter-entry", "[[entry]]\nframe = \"app-correctness/no-em-dash\"\npath = \"docs/**\"\nreason = \"prose uses them deliberately\"\n")
	// The linter that entry names must be enabled for the id to be known.
	lintCfg := "[frames]\nenabled = []\n\n[linters]\n  [linters.no-em-dash]\n    kind = \"regex\"\n    enabled = true\n    severity = \"WARN\"\n    patterns = [\"*\"]\n    regex = \"x\"\n"
	if err := os.WriteFile(filepath.Join(policyRoot, "linter-entry", "appframes.toml"), []byte(lintCfg), 0o600); err != nil {
		t.Fatal(err)
	}

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true, Profile: ProfileBareMetal})
	for repo, want := range map[string]DoctorStatus{
		"stale":        DoctorFail,
		"valid":        DoctorOK,
		"linter-entry": DoctorOK,
		"absent":       DoctorInfo,
	} {
		c, ok := findCheck(rep, repo, "Whitelist")
		if !ok {
			t.Errorf("%s: no Whitelist check", repo)
			continue
		}
		if c.Status != want {
			t.Errorf("%s: got %v want %v (%s)", repo, c.Status, want, c.Reason)
		}
	}
	if c, _ := findCheck(rep, "stale", "Whitelist"); c.Fix == "" || !strings.Contains(c.Reason, "todo-markers") {
		t.Errorf("a stale entry must name itself and carry a fix, got %+v", c)
	}
}

func TestRunDoctorPushURLPort(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "alpha", AddOptions{UpstreamURL: "https://github.com/x/alpha.git", GateAllRefs: true})

	pushURL := func(rep DoctorReport) string {
		t.Helper()
		for _, r := range rep.Repos {
			if r.Name == "alpha" {
				return r.PushURL
			}
		}
		t.Fatal("no connect entry for alpha")
		return ""
	}

	// A declared port wins: the container publishes 2222 while its probe can
	// only ever reach sshd's internal port.
	got := pushURL(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Host: "gw", PushPort: 2222, Offline: true, Profile: ProfileBareMetal}))
	if want := fmt.Sprintf("ssh://git@gw:2222%s/alpha.git", reposRoot); got != want {
		t.Fatalf("declared port: got %q want %q", got, want)
	}

	// Nothing declared, bare metal: the probe reached the host's own sshd, which
	// is the port dev boxes use.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	open := ln.Addr().(*net.TCPAddr).Port
	got = pushURL(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Host: "gw", GatePorts: []int{open}, Profile: ProfileBareMetal}))
	if want := fmt.Sprintf("ssh://git@gw:%d%s/alpha.git", open, reposRoot); got != want {
		t.Fatalf("probed port: got %q want %q", got, want)
	}

	// Offline (the dashboard's default view) has no probe, so the shape decides.
	// Getting this wrong is what sent a bare-metal operator's dev box at 2222.
	got = pushURL(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Host: "gw", Offline: true, Profile: ProfileBareMetal}))
	if want := fmt.Sprintf("ssh://git@gw:22%s/alpha.git", reposRoot); got != want {
		t.Fatalf("bare-metal fallback: got %q want %q", got, want)
	}
	got = pushURL(RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Host: "gw", Offline: true, Profile: ProfileContainer}))
	if want := fmt.Sprintf("ssh://git@gw:2222%s/alpha.git", reposRoot); got != want {
		t.Fatalf("container fallback: got %q want %q", got, want)
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

// The relay speaks https, http (LAN/on-prem, documented at relay.go's authedURL)
// and ssh. Doctor accepted only https, so a working LAN gitea upstream reported
// FAIL and took doctor's exit code with it - the worst thing a diagnostic can do
// is call a healthy gateway broken.
func TestRunDoctorUpstreamSchemes(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "tls", AddOptions{UpstreamURL: "https://github.com/x/tls.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "lan", AddOptions{UpstreamURL: "http://192.0.2.10:3000/x/lan.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "sshscp", AddOptions{UpstreamURL: "git@example.test:x/sshscp.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "sshurl", AddOptions{UpstreamURL: "ssh://git@example.test/x/sshurl.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "bogus", AddOptions{UpstreamURL: "file:///srv/mirror.git", ProtectedRefs: []string{"refs/heads/*"}})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	for repo, want := range map[string]DoctorStatus{
		"tls":    DoctorOK,
		"lan":    DoctorWarn, // relays fine; the credential is in cleartext
		"sshscp": DoctorOK,
		"sshurl": DoctorOK,
		"bogus":  DoctorFail, // the relay genuinely cannot use this
	} {
		c, ok := findCheck(rep, repo, "Upstream URL")
		if !ok {
			t.Fatalf("%s: no Upstream URL check", repo)
		}
		if c.Status != want {
			t.Errorf("%s: Upstream URL = %v, want %v (%s)", repo, c.Status, want, c.Reason)
		}
	}

	// No supported upstream may contribute a FAIL - that is what took doctor's
	// exit code down on a healthy LAN gateway. (Other checks fail here because
	// the seed helper writes no frame policy; this is scoped to the upstream.)
	for _, c := range rep.Checks {
		if c.Name != "Upstream URL" && c.Name != "Upstream credential" {
			continue
		}
		if c.Status == DoctorFail && c.Repo != "bogus" {
			t.Errorf("supported upstream reported FAIL: %s/%s - %s", c.Repo, c.Name, c.Reason)
		}
	}
}

// An SSH upstream authenticates with the gateway's key, so the per-repo
// credential file is not part of that path - warning that "relay will fail"
// sent operators hunting for a token they never needed.
func TestRunDoctorCredentialNotApplicableForSSH(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "sshrepo", AddOptions{UpstreamURL: "git@example.test:x/sshrepo.git", ProtectedRefs: []string{"refs/heads/*"}})
	doctorSeed(t, policyRoot, reposRoot, "httpsrepo", AddOptions{UpstreamURL: "https://github.com/x/httpsrepo.git", ProtectedRefs: []string{"refs/heads/*"}})

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	if c, _ := findCheck(rep, "sshrepo", "Upstream credential"); c.Status != DoctorInfo {
		t.Errorf("ssh upstream: want INFO, got %v (%s)", c.Status, c.Reason)
	}
	// An HTTPS upstream with no credential still warns - there the token is required.
	if c, _ := findCheck(rep, "httpsrepo", "Upstream credential"); c.Status != DoctorWarn {
		t.Errorf("https upstream without credential: want WARN, got %v (%s)", c.Status, c.Reason)
	}
}

// A frame allowlist holding only kit names runs NO frames, because a non-empty
// list replaces the empty-means-everything default. Doctor used to count those
// entries as active frames and report OK.
func TestRunDoctorFramesReportsDeadEntries(t *testing.T) {
	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "onlykits", AddOptions{UpstreamURL: "https://github.com/x/onlykits.git", GateAllRefs: true})
	fp := FramePolicy{Enabled: []string{"core", "web-app"}, Severity: map[string]string{}}
	if err := fp.Save(policyRoot, "onlykits"); err != nil {
		t.Fatal(err)
	}
	doctorSeed(t, policyRoot, reposRoot, "mixed", AddOptions{UpstreamURL: "https://github.com/x/mixed.git", GateAllRefs: true})
	fp2 := FramePolicy{Enabled: []string{"security/no-hardcoded-credentials", "core"}, Severity: map[string]string{}}
	if err := fp2.Save(policyRoot, "mixed"); err != nil {
		t.Fatal(err)
	}

	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})

	c, ok := findCheck(rep, "onlykits", "Frames")
	if !ok || c.Status != DoctorFail {
		t.Fatalf("onlykits: want FAIL (nothing is checked), got %+v ok=%v", c, ok)
	}
	if !strings.Contains(c.Reason, "NONE name a frame") {
		t.Errorf("onlykits: reason should say nothing is checked, got %q", c.Reason)
	}

	c2, ok := findCheck(rep, "mixed", "Frames")
	if !ok || c2.Status != DoctorWarn {
		t.Fatalf("mixed: want WARN, got %+v ok=%v", c2, ok)
	}
	if !strings.Contains(c2.Reason, "1 frame(s) active of 2 entries") {
		t.Errorf("mixed: reason should separate live from dead, got %q", c2.Reason)
	}
}
