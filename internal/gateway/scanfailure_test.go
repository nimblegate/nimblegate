// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/engine"
)

// A push the gate cannot evaluate is an infrastructure outage, not a policy
// finding. These tests pin the three things that follow from that: the operator
// gets the real cause, the pusher gets none of it, and observe mode - which
// relays the unscanned push by design - still raises a signal.

func TestMaterializeTree_ErrorCarriesTarStderr(t *testing.T) {
	bare, sha := makeBareWithCommit(t)
	// A dest dir that does not exist makes tar fail the same structural way a
	// full temp filesystem does: it writes a real reason to stderr and exits
	// non-zero. Before tar's stderr was captured this reported only "exit
	// status 2", which tells an operator nothing.
	err := materializeTree(bare, sha, filepath.Join(t.TempDir(), "missing", "dest"), 0)
	if err == nil {
		t.Fatal("expected materialize to fail against a missing dest dir")
	}
	if !strings.Contains(err.Error(), "No such file or directory") {
		t.Errorf("error must carry tar's own reason, got: %v", err)
	}
}

func TestTarDetail_KeepsTheDiagnosisDropsTheRepetition(t *testing.T) {
	// Verbatim shape of GNU tar filling a small tmpfs: the reason an operator
	// needs is on the second line, and every line after that repeats it.
	var buf bytes.Buffer
	buf.WriteString(`tar: file-031.txt: Wrote only 4608 of 10240 bytes
tar: file-032.txt: Cannot write: No space left on device
tar: file-033.txt: Cannot write: No space left on device
tar: file-034.txt: Cannot write: No space left on device
tar: Exiting with failure status due to previous errors
`)
	got := tarDetail(buf)
	if !strings.Contains(got, "No space left on device") {
		t.Errorf("the actual diagnosis must survive, got %q", got)
	}
	if strings.Contains(got, "file-033") || strings.Contains(got, "Exiting with failure status") {
		t.Errorf("repeats and trailers must be dropped, got %q", got)
	}
	if tarDetail(bytes.Buffer{}) != "" {
		t.Error("empty stderr must add nothing to the error")
	}
}

func TestDecide_ScanFailureRejectsButKeepsDetailOperatorSide(t *testing.T) {
	p := Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}}
	refs := []RefUpdate{{Name: "refs/heads/main", OldRev: zeroRev, NewRev: "abc123"}}
	dec := Decide(p, refs, map[string][]engine.CheckResult{
		"refs/heads/main": {{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: "materialize: untar: exit status 2: tar: No space left on device"}},
	})

	if dec.Accept {
		t.Fatal("a push the gate could not scan must be rejected, not accepted")
	}
	if !dec.ScanFailed {
		t.Error("ScanFailed must be set so callers can route the outage operator-side")
	}
	for _, m := range dec.Messages {
		if strings.Contains(m, ScanFailedID) || strings.Contains(m, "untar") || strings.Contains(m, "No space") {
			t.Errorf("pusher-facing message leaked gate internals: %q", m)
		}
	}
	if len(dec.Messages) != 1 || !strings.Contains(dec.Messages[0], "refs/heads/main") {
		t.Errorf("pusher should still learn which ref was rejected, got %q", dec.Messages)
	}
	if len(dec.ScanFailures) != 1 || !strings.Contains(dec.ScanFailures[0], "No space left on device") {
		t.Errorf("operator detail must survive in ScanFailures, got %q", dec.ScanFailures)
	}
	if len(dec.Findings) != 1 || dec.Findings[0].ID != ScanFailedID || dec.Findings[0].Severity != "ERROR" {
		t.Errorf("scan failure must be a distinguishable finding, got %+v", dec.Findings)
	}
}

func TestDecide_PolicyBlockStillReachesThePusher(t *testing.T) {
	// The counterpart: a real frame finding is exactly what the dev needs, so
	// the withholding above must not bleed into the normal reject path.
	p := Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}}
	refs := []RefUpdate{{Name: "refs/heads/main", OldRev: zeroRev, NewRev: "abc123"}}
	dec := Decide(p, refs, map[string][]engine.CheckResult{
		"refs/heads/main": {{FrameID: "security/x", Outcome: engine.OutcomeBlock, Reason: "key.pem:1 - PEM key"}},
	})
	if dec.ScanFailed || len(dec.ScanFailures) != 0 {
		t.Error("a frame finding is not a scan failure")
	}
	if len(dec.Messages) != 1 || !strings.Contains(dec.Messages[0], "PEM key") {
		t.Errorf("policy findings must still reach the pusher, got %q", dec.Messages)
	}
}

func TestRunPreReceive_ScanFailureIsCamouflagedAndRecorded(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t, nil, nil)
	deps.Checker = errChecker{}
	deps.Orchestrator = nil

	var out bytes.Buffer
	if code := RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), &out); code != 1 {
		t.Fatalf("a push that could not be scanned must fail closed, got code %d", code)
	}
	s := out.String()
	if !strings.Contains(s, "push rejected by repository policy") {
		t.Errorf("pusher should see an ordinary host reject:\n%s", s)
	}
	low := strings.ToLower(s)
	for _, leak := range []string{"nimblegate", "gateway", "relay", "upstream", "materialize", "untar", "tar:", "afgw", "scan-failed"} {
		if strings.Contains(low, leak) {
			t.Errorf("scan-failure reject leaked %q to the pusher:\n%s", leak, s)
		}
	}

	recs := tailParse(deps.AuditPath, 10)
	if len(recs) != 1 {
		t.Fatalf("expected one audit record, got %d", len(recs))
	}
	if !strings.Contains(strings.Join(recs[0].Messages, " "), "boom") {
		t.Errorf("audit record must carry the real cause, got %q", recs[0].Messages)
	}
	if len(recs[0].Findings) != 1 || recs[0].Findings[0].ID != ScanFailedID {
		t.Errorf("audit finding must be identifiable as a scan failure, got %+v", recs[0].Findings)
	}

	evs, err := ReadEvents(policyRoot, func(e Event) bool { return e.Event == "scan-failed" })
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected one scan-failed event, got %d", len(evs))
	}
	if detail, _ := evs[0].Payload["detail"].(string); !strings.Contains(detail, "boom") {
		t.Errorf("event must carry the cause, got %q", detail)
	}
	if relayed, _ := evs[0].Payload["relayed"].(bool); relayed {
		t.Error("under enforcement the push was rejected, so relayed must be false")
	}
}

func TestRunPreReceive_ObserveScanFailureRelaysButStillSignals(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t, nil, nil)
	deps.Checker = errChecker{}
	deps.Orchestrator = nil
	deps.Policy.Observe = true

	var out bytes.Buffer
	if code := RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), &out); code != 0 {
		t.Fatalf("observe never rejects, even when the scan failed; got code %d", code)
	}
	if out.String() != "" {
		t.Errorf("observe mode stays silent to the pusher, got:\n%s", out.String())
	}

	// The push went upstream unscanned. Nothing else would tell the operator
	// that scanning has stopped happening, so the event is the whole signal.
	evs, err := ReadEvents(policyRoot, func(e Event) bool { return e.Event == "scan-failed" })
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("expected one scan-failed event in observe mode, got %d", len(evs))
	}
	if relayed, _ := evs[0].Payload["relayed"].(bool); !relayed {
		t.Error("observe relayed the unscanned push, so relayed must be true")
	}
}

func TestRunPreReceive_ObserveScanFailureFiresTheRail(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t, nil, nil)
	deps.Checker = errChecker{}
	deps.Orchestrator = nil
	deps.Policy.Observe = true
	deps.NotificationConfig = &NotificationConfig{Enabled: true, UpstreamKind: "stub"}

	if code := RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), new(bytes.Buffer)); code != 0 {
		t.Fatalf("observe must still relay, got code %d", code)
	}
	q, err := os.ReadFile(filepath.Join(policyRoot, "demo", "pr-comment-queue.jsonl"))
	if err != nil {
		t.Fatalf("observe-mode scan failure must reach the notification rail: %v", err)
	}
	if !strings.Contains(string(q), ScanFailedID) {
		t.Errorf("queued notification should name the scan failure, got: %s", q)
	}
}

func TestResolveScanTmpDir(t *testing.T) {
	reposRoot := t.TempDir()
	got := ResolveScanTmpDir("", reposRoot)
	want := filepath.Join(reposRoot, ScanTmpDirName)
	if got != want {
		t.Errorf("default should sit beside the bare repos, got %q want %q", got, want)
	}
	if fi, err := os.Stat(want); err != nil || !fi.IsDir() {
		t.Errorf("resolved dir must exist and be a dir: %v", err)
	}

	// A hook resolves GIT_DIR through the <name>.git symlink into _repos/, so
	// the derived root is one level too deep; it must still land on the same
	// directory the maintenance sweeper cleans.
	internals := filepath.Join(reposRoot, reposInternalsDir)
	if err := os.MkdirAll(internals, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ResolveScanTmpDir("", internals); got != want {
		t.Errorf("_repos layout must resolve to the same dir, got %q want %q", got, want)
	}

	configured := filepath.Join(t.TempDir(), "elsewhere")
	if got := ResolveScanTmpDir(configured, reposRoot); got != configured {
		t.Errorf("configured value must win, got %q", got)
	}
	// Nothing usable: fall back to $TMPDIR rather than break every push.
	if got := ResolveScanTmpDir("", ""); got != "" {
		t.Errorf("with no repos root the caller should stay on $TMPDIR, got %q", got)
	}
	if got := ResolveScanTmpDir("/proc/nope/cannot-create", ""); got != "" {
		t.Errorf("an uncreatable dir must degrade to $TMPDIR, got %q", got)
	}
}

func TestLoadGatewayConfig(t *testing.T) {
	root := t.TempDir()
	// No file at all: the defaults are what an operator gets, and they are the
	// protective values, not "unlimited".
	cfg, err := LoadGatewayConfig(root)
	if err != nil {
		t.Fatalf("absent gateway.toml is not an error: %v", err)
	}
	if cfg.ScanTmpDir != "" || cfg.MaxTreeBytes != DefaultMaxTreeBytes || cfg.ScanTimeout != DefaultScanTimeout {
		t.Errorf("defaults = %+v", cfg)
	}

	if err := os.WriteFile(filepath.Join(root, "gateway.toml"), []byte(
		"[maintenance]\nenabled = true\n\n[gateway]\nscan_tmpdir = \"/srv/scan\"\nmax_tree_bytes = 1048576\nscan_timeout = \"90s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadGatewayConfig(root)
	if err != nil {
		t.Fatalf("parse alongside [maintenance]: %v", err)
	}
	if cfg.ScanTmpDir != "/srv/scan" || cfg.MaxTreeBytes != 1048576 || cfg.ScanTimeout != 90*time.Second {
		t.Errorf("got %+v", cfg)
	}

	// Explicit zero means unlimited, and must not be mistaken for absent.
	zero := t.TempDir()
	if err := os.WriteFile(filepath.Join(zero, "gateway.toml"),
		[]byte("[gateway]\nmax_tree_bytes = 0\nscan_timeout = \"0s\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadGatewayConfig(zero)
	if err != nil {
		t.Fatalf("explicit zero: %v", err)
	}
	if cfg.MaxTreeBytes != 0 || cfg.ScanTimeout != 0 {
		t.Errorf("explicit 0 must disable the limit, got %+v", cfg)
	}

	// An operator typo must surface rather than silently revert to a default.
	for name, body := range map[string]string{
		"malformed":     "[gateway\n",
		"negative-size": "[gateway]\nmax_tree_bytes = -1\n",
		"bad-duration":  "[gateway]\nscan_timeout = \"soon\"\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadGatewayConfig(dir); err == nil {
			t.Errorf("%s config should report an error", name)
		}
	}
}

func TestScanStagingDir_HonorsConfigAndFallsBack(t *testing.T) {
	reposRoot := t.TempDir()
	bare := filepath.Join(reposRoot, "demo.git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatal(err)
	}
	// No policy root known (the dashboard preview path before it is wired):
	// still lands next to the repos rather than on $TMPDIR.
	if got := ScanStagingDir(bare, ""); got != filepath.Join(reposRoot, ScanTmpDirName) {
		t.Errorf("default staging dir = %q", got)
	}

	policyRoot := t.TempDir()
	configured := filepath.Join(t.TempDir(), "fast-disk")
	if err := os.WriteFile(filepath.Join(policyRoot, "gateway.toml"),
		[]byte("[gateway]\nscan_tmpdir = \""+configured+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ScanStagingDir(bare, policyRoot); got != configured {
		t.Errorf("configured scan_tmpdir must win, got %q want %q", got, configured)
	}
}

// A pushed tree is attacker-controlled. Extraction cannot write outside the
// staging dir (a git tree holds no ".." entry), but the scan reads what it
// finds, so a symlink aimed out of the tree turns the gate into a file-read
// oracle against anything the gateway user can read.

func TestNeutralizeEscapingSymlinks(t *testing.T) {
	outside := t.TempDir()
	secretPath := filepath.Join(outside, "credential")
	if err := os.WriteFile(secretPath, []byte("ghp_"+strings.Repeat("T", 36)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	staged := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(staged, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	link := func(target, name string) {
		if err := os.Symlink(target, filepath.Join(staged, name)); err != nil {
			t.Fatal(err)
		}
	}
	write("real.txt", "harmless")
	link(secretPath, "absolute-escape.txt")           // straight out
	link("../../etc/passwd", "relative-escape.txt")   // out by traversal
	link("real.txt", "inside-link.txt")               // legitimate content
	link("does-not-exist.txt", "dangling-inside.txt") // broken, but ours
	link("inside-link.txt", "chained-inside.txt")     // chain that stays in
	link("absolute-escape.txt", "chained-escape.txt") // chain that leaves

	if err := neutralizeEscapingSymlinks(staged); err != nil {
		t.Fatalf("neutralize: %v", err)
	}

	for _, name := range []string{"absolute-escape.txt", "relative-escape.txt", "chained-escape.txt"} {
		fi, err := os.Lstat(filepath.Join(staged, name))
		if err != nil {
			t.Fatalf("%s must still exist so filename-keyed frames still see it: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is still a symlink; the read oracle is open", name)
		}
		if fi.Size() != 0 {
			t.Errorf("%s should be empty, got %d bytes", name, fi.Size())
		}
	}
	// The chained case is the one a lexical-only check would miss: it points at
	// a sibling inside the tree that itself points out.
	if b, _ := os.ReadFile(filepath.Join(staged, "chained-escape.txt")); strings.Contains(string(b), "ghp_") {
		t.Error("chained escape still reads the outside file")
	}

	for _, name := range []string{"inside-link.txt", "dangling-inside.txt", "chained-inside.txt"} {
		fi, err := os.Lstat(filepath.Join(staged, name))
		if err != nil {
			t.Fatalf("lstat %s: %v", name, err)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s points inside the tree and is ordinary content; it must survive", name)
		}
	}

	// Nothing outside was touched.
	if b, err := os.ReadFile(secretPath); err != nil || !strings.Contains(string(b), "ghp_") {
		t.Errorf("the outside file must be left exactly as it was: %v", err)
	}
}

func TestMaterializeTree_NeutralizesOnTheWayIn(t *testing.T) {
	// The gate's own path: whatever materializeTree hands to the checker is
	// already free of outward links, so no caller has to remember to do it.
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("AKIAZZZZTESTFIXTURE0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	git := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", ".")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(work, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "hostile")

	dest := t.TempDir()
	if err := materializeTree(filepath.Join(work, ".git"), "HEAD", dest, 0); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "leak.txt"))
	if err != nil {
		t.Fatalf("read staged leak.txt: %v", err)
	}
	if strings.Contains(string(b), "AKIA") {
		t.Error("the scan would read a file outside the pushed repo")
	}
}

// bigRepo builds a bare repo whose tree extracts to well over sizeHint bytes
// from a pack far smaller than that - the shape receive.maxInputSize cannot see.
func bigRepo(t *testing.T, bytesOfZeros int) (bare, sha string) {
	t.Helper()
	work := t.TempDir()
	run := func(args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = work
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", ".")
	if err := os.WriteFile(filepath.Join(work, "zeros.bin"), make([]byte, bytesOfZeros), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "big")
	return filepath.Join(work, ".git"), run("rev-parse", "HEAD")
}

func TestMaterializeTree_CapsWhatOnePushExpandsTo(t *testing.T) {
	// 4 MiB of zeros compresses to a few KB: the pack cap would never see this,
	// which is exactly why the limit belongs on the extracted stream.
	bare, sha := bigRepo(t, 4<<20)

	dest := t.TempDir()
	err := materializeTree(bare, sha, dest, 1<<20)
	if err == nil {
		t.Fatal("a tree past the cap must fail rather than extract")
	}
	if !errors.Is(err, errTreeTooLarge) {
		t.Errorf("want errTreeTooLarge, got %v", err)
	}
	// Nothing usable was left behind for a caller to scan by mistake.
	if b, err := os.ReadFile(filepath.Join(dest, "zeros.bin")); err == nil && len(b) >= 4<<20 {
		t.Error("the oversize file was extracted in full despite the cap")
	}

	// The same tree under a generous cap extracts normally.
	ok := t.TempDir()
	if err := materializeTree(bare, sha, ok, 64<<20); err != nil {
		t.Fatalf("a tree inside the cap must extract: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(ok, "zeros.bin")); err != nil || fi.Size() != 4<<20 {
		t.Errorf("expected the full file, got %v (err %v)", fi, err)
	}

	// 0 means unlimited.
	unlimited := t.TempDir()
	if err := materializeTree(bare, sha, unlimited, 0); err != nil {
		t.Errorf("0 must disable the cap: %v", err)
	}
}

func TestRunPreReceive_OversizeTreeIsAScanFailure(t *testing.T) {
	// End to end: the cap lands on the scan-failure path, so it rejects under
	// enforcement and stays camouflaged to the pusher.
	deps, policyRoot, _, _ := newPreReceiveHarness(t, nil, nil)
	deps.Orchestrator = nil
	bare, sha := bigRepo(t, 4<<20)
	deps.GitDir = bare
	deps.MaxTreeBytes = 1 << 20

	var out bytes.Buffer
	if code := RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), &out); code != 1 {
		t.Fatalf("oversize push must fail closed, got code %d", code)
	}
	if strings.Contains(strings.ToLower(out.String()), "tree") || strings.Contains(out.String(), "byte") {
		t.Errorf("the cap is an operator detail, not pusher-facing:\n%s", out.String())
	}
	evs, err := ReadEvents(policyRoot, func(e Event) bool { return e.Event == "scan-failed" })
	if err != nil || len(evs) != 1 {
		t.Fatalf("expected one scan-failed event, got %d (err %v)", len(evs), err)
	}
	if detail, _ := evs[0].Payload["detail"].(string); !strings.Contains(detail, "size cap") {
		t.Errorf("the operator should learn it was the cap, got %q", detail)
	}
}

type hangingChecker struct{ started chan struct{} }

func (h hangingChecker) Check(string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	close(h.started)
	select {} // never returns: the shape a pathological tree produces
}

func TestRunPreReceive_ScanTimeoutIsAScanFailure(t *testing.T) {
	deps, policyRoot, sha, _ := newPreReceiveHarness(t, nil, nil)
	deps.Orchestrator = nil
	deps.Checker = hangingChecker{started: make(chan struct{})}
	deps.ScanTimeout = 100 * time.Millisecond

	done := make(chan int, 1)
	go func() {
		done <- RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), new(bytes.Buffer))
	}()
	select {
	case code := <-done:
		if code != 1 {
			t.Fatalf("a scan that never finished must fail closed, got %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the deadline did not fire; the push would hold the pusher forever")
	}

	evs, err := ReadEvents(policyRoot, func(e Event) bool { return e.Event == "scan-failed" })
	if err != nil || len(evs) != 1 {
		t.Fatalf("expected one scan-failed event, got %d (err %v)", len(evs), err)
	}
	if detail, _ := evs[0].Payload["detail"].(string); !strings.Contains(detail, "exceeded") {
		t.Errorf("detail should name the deadline, got %q", detail)
	}
}

func TestCheckWithDeadline_ZeroMeansNoDeadline(t *testing.T) {
	// Opting out must actually opt out, not collapse to an immediate timeout.
	want := []engine.CheckResult{{FrameID: "x", Outcome: engine.OutcomePass}}
	got, _, err := checkWithDeadline(fakeChecker{results: want}, t.TempDir(), 0)
	if err != nil || len(got) != 1 || got[0].FrameID != "x" {
		t.Errorf("got %v err %v", got, err)
	}
}

func TestInspectStagingDir_isReadOnlyAndMatchesTheResolver(t *testing.T) {
	reposRoot := t.TempDir()
	want := filepath.Join(reposRoot, ScanTmpDirName)

	// Read-only: rendering a page must not create directories.
	info := InspectStagingDir("", reposRoot)
	if info.Dir != want {
		t.Errorf("Dir = %q, want %q", info.Dir, want)
	}
	if info.Exists {
		t.Error("nothing has created it yet")
	}
	if _, err := os.Stat(want); !os.IsNotExist(err) {
		t.Error("InspectStagingDir must not create the directory")
	}

	// After the resolver runs, the same path is reported as existing - the two
	// must not disagree about where staging happens.
	got, err := ResolveScanTmpDirChecked("", reposRoot)
	if err != nil || got != want {
		t.Fatalf("resolver: %q %v", got, err)
	}
	if info = InspectStagingDir("", reposRoot); !info.Exists || info.Dir != want {
		t.Errorf("after resolve: %+v", info)
	}

	// The _repos layout normalises the same way on both paths.
	internals := filepath.Join(reposRoot, reposInternalsDir)
	if err := os.MkdirAll(internals, 0o755); err != nil {
		t.Fatal(err)
	}
	if info = InspectStagingDir("", internals); info.Dir != want {
		t.Errorf("_repos layout: Dir = %q, want %q", info.Dir, want)
	}

	// A configured dir is reported verbatim, existing or not.
	cfg := filepath.Join(t.TempDir(), "fast-disk")
	if info = InspectStagingDir(cfg, reposRoot); info.Dir != cfg || info.Configured != cfg || info.Exists {
		t.Errorf("configured: %+v", info)
	}
}

func TestResolveScanTmpDirChecked_reportsWhyItGaveUp(t *testing.T) {
	// The silent-fallback case: without an error here, a typo'd scan_tmpdir
	// quietly restores RAM staging and nothing anywhere says so.
	dir, err := ResolveScanTmpDirChecked("/proc/nope/cannot-create", "")
	if err == nil {
		t.Fatal("an uncreatable configured dir must report why")
	}
	if dir != "" {
		t.Errorf("dir = %q, want empty (caller falls back to $TMPDIR)", dir)
	}
	if !strings.Contains(err.Error(), "/proc/nope/cannot-create") {
		t.Errorf("error should name the path: %v", err)
	}
	if _, err := ResolveScanTmpDirChecked("", ""); err == nil {
		t.Error("no repos root and no configured dir is also a fallback")
	}
}

func TestInspectGatewayConfig_namesKnobsThatDoNothing(t *testing.T) {
	// Every one of these is valid TOML that parses without error and has no
	// effect - the failure an operator experiences as "I set it and nothing
	// changed", with nothing anywhere to explain it.
	for name, body := range map[string]string{
		"no [gateway] header":    "observe = false\nscan_tmpdir = \"/tmp/test/\"\n",
		"under another section":  "[maintenance]\nenabled = true\nscan_tmpdir = \"/tmp/test/\"\n",
		"above its own header":   "max_tree_bytes = 1\n[gateway]\n",
		"timeout in wrong place": "[maintenance]\nscan_timeout = \"5m\"\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, issues := InspectGatewayConfig(dir)
		if len(issues) == 0 {
			t.Errorf("%s: silently ignored, no issue reported", name)
		}
		if cfg.ScanTmpDir != "" {
			t.Errorf("%s: the misplaced key must not take effect, got %q", name, cfg.ScanTmpDir)
		}
	}

	// The correct shape reports nothing, and unrelated sections are not judged.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"),
		[]byte("[maintenance]\nenabled = true\ninterval = \"168h\"\n\n[gateway]\nscan_tmpdir = \"/srv/scan\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, issues := InspectGatewayConfig(dir)
	if len(issues) != 0 {
		t.Errorf("a correct config must be quiet, got %v", issues)
	}
	if cfg.ScanTmpDir != "/srv/scan" {
		t.Errorf("ScanTmpDir = %q", cfg.ScanTmpDir)
	}
}

func TestInspectGatewayConfig_findsKnobsInARepoPolicyFile(t *testing.T) {
	// The real report: <policy-root>/<repo>/gateway.toml is the per-repo policy,
	// one directory below the machine-level file of the same name, and it is the
	// one an operator is far more likely to have open.
	policyRoot := t.TempDir()
	repoDir := filepath.Join(policyRoot, "nimblegate")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoDir, "gateway.toml"), []byte(
		"upstream-url = \"https://example.test/x\"\nobserve = false\nmax-input-size = \"500m\"\nscan_tmpdir = \"/tmp/test/\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, issues := InspectGatewayConfig(policyRoot)
	if len(issues) != 1 {
		t.Fatalf("want one issue, got %v", issues)
	}
	for _, want := range []string{"scan_tmpdir", "nimblegate", filepath.Join(policyRoot, "gateway.toml")} {
		if !strings.Contains(issues[0], want) {
			t.Errorf("issue should mention %q: %s", want, issues[0])
		}
	}

	// An ordinary repo policy with no machine-level knobs stays quiet.
	if err := os.WriteFile(filepath.Join(repoDir, "gateway.toml"),
		[]byte("upstream-url = \"https://example.test/x\"\nobserve = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, issues = InspectGatewayConfig(policyRoot); len(issues) != 0 {
		t.Errorf("a normal repo policy must not be flagged: %v", issues)
	}
}

func TestInspectGatewayConfig_surfacesParseErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gateway.toml"), []byte("[gateway\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, issues := InspectGatewayConfig(dir); len(issues) == 0 {
		t.Error("a malformed file must be reported, not silently defaulted")
	}
}

func TestStagingDirPathsAreCleaned(t *testing.T) {
	// A trailing slash must not survive into display, comparison, or
	// filepath.Dir - which returns the directory itself rather than the parent
	// when one does.
	reposRoot := t.TempDir()
	want := filepath.Join(t.TempDir(), "scan")

	if got := InspectStagingDir(want+"/", reposRoot); got.Dir != want {
		t.Errorf("InspectStagingDir = %q, want %q", got.Dir, want)
	}
	got, err := ResolveScanTmpDirChecked(want+"/", reposRoot)
	if err != nil || got != want {
		t.Errorf("ResolveScanTmpDirChecked = %q (%v), want %q", got, err, want)
	}
	// The two must still agree - the resolver creates, the inspector reports.
	if info := InspectStagingDir(want+"/", reposRoot); !info.Exists || info.Dir != got {
		t.Errorf("resolver and inspector disagree: %+v vs %q", info, got)
	}
}
