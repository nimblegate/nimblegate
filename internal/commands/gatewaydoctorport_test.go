// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"strings"
	"testing"

	"nimblegate/internal/gateway"
)

// The published gate port is declared by the deploy path, never inferred. A
// missing or nonsense value must read as "unknown" (0) so doctor falls back to
// the probed port rather than printing a port nothing listens on.
func TestPublicSSHPort(t *testing.T) {
	for _, tc := range []struct {
		env  string
		want int
	}{
		{"2222", 2222},
		{"22", 22},
		{"", 0},
		{"no", 0},
		{"0", 0},
		{"-1", 0},
		{"70000", 0},
	} {
		t.Setenv("NIMBLEGATE_PUBLIC_SSH_PORT", tc.env)
		if got := publicSSHPort(); got != tc.want {
			t.Errorf("NIMBLEGATE_PUBLIC_SSH_PORT=%q: got %d want %d", tc.env, got, tc.want)
		}
	}
}

// The connect block must print the port the engine resolved, not one the
// renderer decided for itself. Two builders naming the same port independently
// is how doctor came to report 22 and 2222 in one output.
func TestRenderDoctorTextUsesResolvedPushURL(t *testing.T) {
	rep := gateway.DoctorReport{
		Host:  "gw.example",
		Repos: []gateway.DoctorRepoConn{{Name: "alpha", PushURL: "ssh://git@gw.example:22/srv/gateway/repos/alpha.git"}},
	}
	var buf bytes.Buffer
	renderDoctorText(&buf, rep)
	out := buf.String()
	if !strings.Contains(out, "ssh://git@gw.example:22/srv/gateway/repos/alpha.git") {
		t.Fatalf("connect block does not use the resolved push URL:\n%s", out)
	}
	if strings.Contains(out, ":2222/") {
		t.Fatalf("connect block invented a port:\n%s", out)
	}
}

// The connect steps are printed by the CLI as well as the dashboard, so their
// wording cannot assume a dashboard the reader just loaded.
func TestPointOriginNote(t *testing.T) {
	placeholder := pointOriginNote("ssh://git@<host>:22/srv/gateway/repos/alpha.git")
	if !strings.Contains(placeholder, "<host>") {
		t.Errorf("a placeholder URL must tell the reader to substitute: %q", placeholder)
	}
	known := pointOriginNote("ssh://git@gw.example:22/srv/gateway/repos/alpha.git")
	if strings.Contains(known, "dashboard") || strings.Contains(known, "<host>") {
		t.Errorf("a resolved URL needs no substitution and no dashboard reference: %q", known)
	}
}

func TestDoctorConnectStepsUpstream(t *testing.T) {
	base := gateway.DoctorRepoConn{Name: "alpha", PushURL: "ssh://git@gw:22/srv/gateway/repos/alpha.git"}
	if n := len(doctorConnectSteps(base)); n != 4 {
		t.Fatalf("without an upstream: %d steps, want 4", n)
	}
	withUp := base
	withUp.UpstreamCheck = "sudo -u git git ls-remote https://github.com/x/alpha.git HEAD"
	withUp.UpstreamAsksCred = true
	steps := doctorConnectSteps(withUp)
	if len(steps) != 5 {
		t.Fatalf("with an upstream: %d steps, want 5", len(steps))
	}
	last := steps[4]
	if len(last.Cmds) != 1 || last.Cmds[0] != withUp.UpstreamCheck {
		t.Errorf("last step command: %+v", last.Cmds)
	}
	if !strings.Contains(last.Note, "on the gateway") || !strings.Contains(last.Note, "credential") {
		t.Errorf("last step note: %q", last.Note)
	}
	var buf bytes.Buffer
	renderDoctorText(&buf, gateway.DoctorReport{Host: "gw", Repos: []gateway.DoctorRepoConn{withUp}})
	if !strings.Contains(buf.String(), "5. Compare with the upstream") {
		t.Errorf("CLI text lacks the upstream step:\n%s", buf.String())
	}
}
