// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"strings"
)

// InstallProfile carries the facts doctor cannot observe about the install it
// is running on: how this shape exposes the dashboard, where its convention
// puts sshd's keys, whether it runs a reconcile backstop. Anything doctor CAN
// observe - the port a probe reached, the keys file that exists on disk - is
// reported from the observation and never from here.
//
// One code path serves both shapes: the checks are identical, only the
// remediation text differs, so shape knowledge lives in this file rather than
// in a branch at every message site.
type InstallProfile struct {
	// Name is what the report calls this install ("container", "bare metal").
	Name string

	// AuthorizedKeys is the keys file this shape's dashboard manages when no
	// flag overrides it.
	AuthorizedKeys string

	// BindFix says how to reach a dashboard bound to loopback.
	BindFix string

	// RelayUnit is the systemd unit that runs this shape's reconcile backstop.
	// Its User= is the account the backstop runs as, which the shared policy
	// files must be reachable by even when no repo routes its pushes through
	// the relay socket. Empty where the shape ships no backstop.
	RelayUnit string

	// HasRelayBackstop says whether this shape ships a reconcile backstop at
	// all. False suppresses the check rather than reporting a service that does
	// not exist here. No remediation travels with it: starting the backstop
	// needs the relay user provisioned first (docs/server/README.md), so a
	// one-line command would be advice that does not work.
	HasRelayBackstop bool

	// DefaultPushPort is this shape's convention for the port a dev box pushes
	// to, used when nothing declares one.
	DefaultPushPort int

	// TrustProbedPort is true when nothing maps ports between the gateway and
	// the dev box, so a port the gate probe reached is also the port to push
	// to. False where a mapping exists the gateway cannot see - the container
	// probe reaches sshd's 22 while the dev box uses the published port.
	TrustProbedPort bool
}

// PushPort resolves the port to print in connect URLs. declared wins (each
// deploy path states it), then the probe where this shape's observation is
// meaningful, then the shape's convention.
func (p InstallProfile) PushPort(declared, probed int) int {
	if declared != 0 {
		return declared
	}
	if p.TrustProbedPort && probed != 0 {
		return probed
	}
	return p.DefaultPushPort
}

// ProfileContainer is the published image: s6 supervises sshd and the
// dashboard, compose publishes the ports, and no relay-service runs - the
// container relays inline from post-receive.
var ProfileContainer = InstallProfile{
	Name:           "container",
	AuthorizedKeys: "/srv/gateway/ssh/authorized_keys",
	BindFix:        "set NIMBLEGATE_DASHBOARD_HOST=0.0.0.0 in compose (only behind a proxy that authenticates), or tunnel: ssh -L 7900:127.0.0.1:7900 user@host (127.0.0.1, not localhost - Docker publishes on IPv4)",

	// The image supervises sshd and the dashboard only - no relay-service - so
	// it relays inline from post-receive and has no backstop to report.
	//
	// compose publishes the gate on 2222 by default. The probe is no use here:
	// it reaches sshd's port inside the container, not the published one.
	DefaultPushPort: 2222,
}

// ProfileBareMetal is the systemd install from docs/server/README.md: the host
// sshd reads /home/git/.ssh/authorized_keys, and nimblegate-relay.service is
// the privilege-separated relay plus reconcile backstop.
var ProfileBareMetal = InstallProfile{
	Name:             "bare metal",
	AuthorizedKeys:   "/home/git/.ssh/authorized_keys",
	BindFix:          "nimblegate gateway bind <gateway-ip> (or `all`), then systemctl restart nimblegate-dashboard; or tunnel: ssh -L 7900:127.0.0.1:7900 user@host",
	RelayUnit:        "/etc/systemd/system/nimblegate-relay.service",
	HasRelayBackstop: true,

	// The host's own sshd, so the port the probe reaches is the port dev boxes
	// use; 22 is the convention when the probe has not run (an offline report).
	DefaultPushPort: 22,
	TrustProbedPort: true,
}

// ProfileUnknown is used when nothing declares the shape and nothing is
// detected. It keeps the historical defaults and its advice names both shapes
// rather than guessing one.
var ProfileUnknown = InstallProfile{
	Name:           "not declared",
	AuthorizedKeys: "/srv/gateway/ssh/authorized_keys",
	BindFix:        "expose it the way this install does - NIMBLEGATE_DASHBOARD_HOST in compose, `nimblegate gateway bind` on bare metal; or tunnel: ssh -L 7900:127.0.0.1:7900 user@host",

	// The advertised install is compose, so its published port is the least
	// surprising guess when nothing better is known.
	DefaultPushPort: 2222,
	TrustProbedPort: true,
}

// Markers used to detect the shape when nothing declares it. Vars so tests can
// point them at a temp dir.
var (
	s6MarkerPath      = "/run/s6"
	systemdMarkerPath = "/run/systemd/system"
)

// ResolveInstallProfile picks the profile from the declaration each deploy path
// makes (NIMBLEGATE_INSTALL in compose and in the systemd unit), falling back
// to detecting the supervisor that is actually running. The declaration wins so
// an operator can correct a detection that guessed wrong.
func ResolveInstallProfile() InstallProfile {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("NIMBLEGATE_INSTALL"))) {
	case "container", "docker", "compose":
		return ProfileContainer
	case "bare-metal", "bare_metal", "baremetal", "systemd":
		return ProfileBareMetal
	}
	if _, err := os.Stat(s6MarkerPath); err == nil {
		return ProfileContainer
	}
	if _, err := os.Stat(systemdMarkerPath); err == nil {
		return ProfileBareMetal
	}
	return ProfileUnknown
}
