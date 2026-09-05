// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestAccessBits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid := ownerOf(t, fi)

	// Owner class wins outright, even when it grants less than the group would.
	if got := accessBits(fi, uid, map[uint32]bool{gid: true}); got != 6 {
		t.Errorf("owner bits = %o, want 6", got)
	}
	// A non-owner in the file's group gets the group bits.
	if got := accessBits(fi, uid+1, map[uint32]bool{gid: true}); got != 4 {
		t.Errorf("group bits = %o, want 4", got)
	}
	// A stranger gets the other bits, which 0640 leaves empty.
	if got := accessBits(fi, uid+1, map[uint32]bool{gid + 1: true}); got != 0 {
		t.Errorf("other bits = %o, want 0", got)
	}
	// The owner of a 0000 file is still denied: this is the shape of the bug -
	// git owning gateway.toml did not make it readable to the relay user.
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	fi, _ = os.Stat(path)
	if got := accessBits(fi, uid, map[uint32]bool{gid: true}); got != 0 {
		t.Errorf("owner-of-0000 bits = %o, want 0", got)
	}
}

func TestRelaySocketFromHook(t *testing.T) {
	bare := t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(bare, "hooks", "post-receive"), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("#!/bin/sh\nexport NBG_RELAY_SOCKET=\"/run/nbg/relay.sock\"\nexec nimblegate gateway post-receive\n")
	if got := relaySocketFromHook(bare); got != "/run/nbg/relay.sock" {
		t.Errorf("socket = %q, want /run/nbg/relay.sock", got)
	}
	write("#!/bin/sh\nexec nimblegate gateway post-receive\n")
	if got := relaySocketFromHook(bare); got != "" {
		t.Errorf("inline relay must report no socket, got %q", got)
	}
	if got := relaySocketFromHook(filepath.Join(bare, "nope")); got != "" {
		t.Errorf("missing hook must report no socket, got %q", got)
	}
}

// An inline relay has one account on both sides, so the check must stay silent
// rather than reporting a boundary that does not exist.
func TestDoctorCheckRelayAccess_silentWhenRelayIsInline(t *testing.T) {
	bare, policyRoot := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bare, "hooks", "post-receive"), []byte("#!/bin/sh\nexec x\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var got []DoctorCheck
	cfg := DoctorConfig{PolicyRoot: policyRoot, Profile: ProfileContainer}
	doctorCheckRelayAccess(func(c DoctorCheck) { got = append(got, c) }, cfg, "demo", bare)
	if len(got) != 0 {
		t.Errorf("inline relay produced %d check(s): %+v", len(got), got)
	}
}

func TestDoctorCheckRelayAccess_okAndUnreadablePolicy(t *testing.T) {
	bare, policyRoot, sockDir := t.TempDir(), t.TempDir(), t.TempDir()
	sock := filepath.Join(sockDir, "relay.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer ln.Close()

	if err := os.MkdirAll(filepath.Join(bare, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	hook := fmt.Sprintf("#!/bin/sh\nexport NBG_RELAY_SOCKET=%q\nexec x\n", sock)
	if err := os.WriteFile(filepath.Join(bare, "hooks", "post-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(policyRoot, "demo")
	if err := os.MkdirAll(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	toml := filepath.Join(dir, "gateway.toml")
	if err := os.WriteFile(toml, []byte("upstream-url = \"x\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	run := func() DoctorCheck {
		t.Helper()
		var got []DoctorCheck
		doctorCheckRelayAccess(func(c DoctorCheck) { got = append(got, c) }, DoctorConfig{PolicyRoot: policyRoot, Profile: ProfileContainer}, "demo", bare)
		if len(got) != 1 {
			t.Fatalf("want exactly one check, got %+v", got)
		}
		return got[0]
	}

	// The socket is ours, so "the relay user" is this test process: readable
	// policy, writable dir, no credential file - all fine.
	if c := run(); c.Status != DoctorOK {
		t.Errorf("status = %v, want OK: %s", c.Status, c.Reason)
	}

	// Now reproduce the failure: a policy file its own owner cannot read.
	if err := os.Chmod(toml, 0); err != nil {
		t.Fatal(err)
	}
	c := run()
	if c.Status != DoctorFail {
		t.Fatalf("status = %v, want FAIL", c.Status)
	}
	// The remediation is the point: the check exists because doctor previously
	// blamed the upstream credential for a chmod.
	if !strings.Contains(c.Reason, "gateway.toml") {
		t.Errorf("reason must name the file:\n%s", c.Reason)
	}
	if !strings.Contains(c.Fix, "chmod") || !strings.Contains(c.Fix, toml) {
		t.Errorf("fix must be a runnable chmod on the file:\n%s", c.Fix)
	}
}

// ownerOf reads the numeric owner of fi, skipping the test where the platform
// does not expose it (the check itself degrades the same way).
func ownerOf(t *testing.T, fi os.FileInfo) (uid, gid uint32) {
	t.Helper()
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("no numeric file ownership on this platform")
	}
	return st.Uid, st.Gid
}

// The failure that prompted this check lived on a gateway whose hooks carried
// no socket at all: pushes relayed inline, so nothing looked
// privilege-separated, while the reconcile backstop ran as another user and
// silently delivered nothing. The unit's User= is the only way to see that
// account before the backstop has written anything.
func TestDoctorCheckRelayAccess_findsBackstopUserWithNoSocketInHook(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("no current user: %v", err)
	}
	bare, policyRoot, unitDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(bare, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	// An inline-relay hook: no NBG_RELAY_SOCKET anywhere in it.
	if err := os.WriteFile(filepath.Join(bare, "hooks", "post-receive"), []byte("#!/bin/sh\nexec x\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(unitDir, "nimblegate-relay.service")
	if err := os.WriteFile(unit, []byte("[Service]\nUser="+me.Username+"\nExecStart=/usr/local/bin/nimblegate\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := ProfileBareMetal
	profile.RelayUnit = unit

	dir := filepath.Join(policyRoot, "demo")
	if err := os.MkdirAll(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	toml := filepath.Join(dir, "gateway.toml")
	if err := os.WriteFile(toml, []byte("upstream-url = \"x\"\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	run := func() DoctorCheck {
		t.Helper()
		var got []DoctorCheck
		doctorCheckRelayAccess(func(c DoctorCheck) { got = append(got, c) },
			DoctorConfig{PolicyRoot: policyRoot, Profile: profile}, "demo", bare)
		if len(got) != 1 {
			t.Fatalf("want exactly one check, got %+v", got)
		}
		return got[0]
	}
	if c := run(); c.Status != DoctorOK {
		t.Errorf("status = %v, want OK: %s", c.Status, c.Reason)
	}
	if err := os.Chmod(toml, 0); err != nil {
		t.Fatal(err)
	}
	if c := run(); c.Status != DoctorFail {
		t.Errorf("status = %v, want FAIL when the backstop user cannot read the policy", c.Status)
	}
}

// A shape with no backstop and no socket has one account on both sides.
func TestRelayAccount_silentWithoutSocketOrUnit(t *testing.T) {
	bare := t.TempDir()
	_, from, err := relayAccount(ProfileContainer, bare)
	if from != "" || err != nil {
		t.Errorf("container with no socket: from=%q err=%v, want silence", from, err)
	}
}

// RunDoctor resolves the install shape when the caller declares none, and every
// downstream check reads it from the config. Leaving the detected profile in a
// local shipped a relay-access check that skipped on the exact install it was
// written for: bare metal, backstop running, no socket in the hooks.
func TestRunDoctor_relayAccessSeesTheDetectedProfile(t *testing.T) {
	me, err := user.Current()
	if err != nil {
		t.Skipf("no current user: %v", err)
	}
	dir := t.TempDir()
	unit := filepath.Join(dir, "nimblegate-relay.service")
	if err := os.WriteFile(unit, []byte("[Service]\nUser="+me.Username+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Detection must land on bare metal, and its profile must name the unit.
	t.Setenv("NIMBLEGATE_INSTALL", "")
	oldS6, oldSystemd, oldUnit := s6MarkerPath, systemdMarkerPath, ProfileBareMetal.RelayUnit
	s6MarkerPath, systemdMarkerPath, ProfileBareMetal.RelayUnit = filepath.Join(dir, "absent"), dir, unit
	defer func() {
		s6MarkerPath, systemdMarkerPath, ProfileBareMetal.RelayUnit = oldS6, oldSystemd, oldUnit
	}()

	policyRoot, reposRoot := doctorRoots(t)
	doctorSeed(t, policyRoot, reposRoot, "alpha", AddOptions{UpstreamURL: "file:///tmp/up.git"})

	// No Profile field: exactly how the CLI and dashboard call it.
	rep := RunDoctor(DoctorConfig{PolicyRoot: policyRoot, ReposRoot: reposRoot, Offline: true})
	for _, c := range rep.Checks {
		if c.Name == "Relay access" {
			return
		}
	}
	t.Errorf("no Relay access check in a bare-metal report:\n%+v", rep.Checks)
}
