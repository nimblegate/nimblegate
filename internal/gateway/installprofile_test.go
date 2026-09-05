// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"testing"
)

// Conformance: every profile must answer every question doctor asks it. A new
// shape that leaves a field blank prints an empty remediation line, which reads
// as "no fix known" rather than "not applicable here".
func TestInstallProfilesAreComplete(t *testing.T) {
	for _, p := range []InstallProfile{ProfileContainer, ProfileBareMetal, ProfileUnknown} {
		if p.Name == "" {
			t.Error("profile with no name")
			continue
		}
		if p.AuthorizedKeys == "" {
			t.Errorf("%s: no authorized_keys path", p.Name)
		}
		if p.BindFix == "" {
			t.Errorf("%s: no bind remediation", p.Name)
		}
		if p.DefaultPushPort == 0 {
			t.Errorf("%s: no default push port", p.Name)
		}
	}
	// The backstop is reported only where one exists, and never with a command:
	// starting it needs the relay user provisioned first, so a one-liner would
	// be advice that silently does nothing.
	if ProfileContainer.HasRelayBackstop {
		t.Error("the image ships no relay-service; it must not report a backstop")
	}
	if !ProfileBareMetal.HasRelayBackstop {
		t.Error("bare metal ships nimblegate-relay.service; the backstop is reportable there")
	}
	// A shape with a backstop must name its unit: that file's User= is the only
	// way to learn which account the backstop runs as before it has run once.
	if ProfileBareMetal.RelayUnit == "" {
		t.Error("bare metal names no relay unit; the relay-access check cannot find the backstop's user")
	}
	if ProfileContainer.RelayUnit != "" {
		t.Error("the image ships no relay unit")
	}
}

// Each shape's advice must be written for that shape only - the whole point of
// the split is that a bare-metal operator never reads compose instructions.
func TestInstallProfileAdviceDoesNotCrossShapes(t *testing.T) {
	if got := ProfileBareMetal.BindFix; contains(got, "compose") || contains(got, "NIMBLEGATE_DASHBOARD_HOST") {
		t.Errorf("bare-metal bind advice names container config: %q", got)
	}
	if got := ProfileContainer.BindFix; contains(got, "systemctl") || contains(got, "gateway bind") {
		t.Errorf("container bind advice names bare-metal commands: %q", got)
	}
}

func TestResolveInstallProfile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent")
	s6 := filepath.Join(dir, "s6")
	systemd := filepath.Join(dir, "systemd")
	for _, p := range []string{s6, systemd} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	set := func(s6Path, systemdPath string) {
		s6MarkerPath, systemdMarkerPath = s6Path, systemdPath
	}
	origS6, origSystemd := s6MarkerPath, systemdMarkerPath
	t.Cleanup(func() { set(origS6, origSystemd) })

	// A declaration wins over whatever is on disk, so an operator can correct a
	// detection that guessed wrong.
	t.Setenv("NIMBLEGATE_INSTALL", "bare-metal")
	set(s6, missing)
	if got := ResolveInstallProfile(); got.Name != ProfileBareMetal.Name {
		t.Errorf("declaration should beat s6 detection, got %q", got.Name)
	}

	t.Setenv("NIMBLEGATE_INSTALL", "container")
	set(missing, systemd)
	if got := ResolveInstallProfile(); got.Name != ProfileContainer.Name {
		t.Errorf("declaration should beat systemd detection, got %q", got.Name)
	}

	t.Setenv("NIMBLEGATE_INSTALL", "")
	set(s6, systemd)
	if got := ResolveInstallProfile(); got.Name != ProfileContainer.Name {
		t.Errorf("s6 wins when both markers exist (our image runs s6), got %q", got.Name)
	}

	set(missing, systemd)
	if got := ResolveInstallProfile(); got.Name != ProfileBareMetal.Name {
		t.Errorf("systemd alone means bare metal, got %q", got.Name)
	}

	set(missing, missing)
	if got := ResolveInstallProfile(); got.Name != ProfileUnknown.Name {
		t.Errorf("nothing declared or detected should be unknown, got %q", got.Name)
	}
}

// An offline report has no probe to lean on, so the shape's own convention has
// to be right: printing the compose port to a bare-metal operator sends their
// dev box at a port nothing listens on.
func TestInstallProfilePushPort(t *testing.T) {
	for _, tc := range []struct {
		name             string
		prof             InstallProfile
		declared, probed int
		want             int
	}{
		{"declared beats everything (bare metal)", ProfileBareMetal, 2345, 22, 2345},
		{"declared beats everything (container)", ProfileContainer, 2345, 22, 2345},
		{"bare metal trusts its own sshd", ProfileBareMetal, 0, 2022, 2022},
		{"bare metal offline falls back to 22", ProfileBareMetal, 0, 0, 22},
		{"container ignores the probe it cannot trust", ProfileContainer, 0, 22, 2222},
		{"container offline falls back to the published default", ProfileContainer, 0, 0, 2222},
		{"unknown shape keeps the historical behaviour", ProfileUnknown, 0, 2022, 2022},
		{"unknown shape with no probe guesses compose", ProfileUnknown, 0, 0, 2222},
	} {
		if got := tc.prof.PushPort(tc.declared, tc.probed); got != tc.want {
			t.Errorf("%s: got %d want %d", tc.name, got, tc.want)
		}
	}
}
