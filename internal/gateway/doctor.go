// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/version"
)

// DoctorStatus is a check outcome, ordered by ascending severity.
type DoctorStatus int

const (
	DoctorOK DoctorStatus = iota
	DoctorInfo
	DoctorWarn
	DoctorFail
)

func (s DoctorStatus) String() string {
	switch s {
	case DoctorOK:
		return "OK"
	case DoctorInfo:
		return "INFO"
	case DoctorWarn:
		return "WARN"
	case DoctorFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// MarshalJSON emits the severity name so --json output is script-readable
// instead of bare integers.
func (s DoctorStatus) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.String() + `"`), nil
}

// DoctorCheck is one diagnostic line. Repo "" means a global (non-per-repo) check.
type DoctorCheck struct {
	Name   string
	Reason string
	Fix    string
	Status DoctorStatus
	Repo   string
}

// DoctorKey is one authorized SSH key, parsed from the authorized_keys file.
type DoctorKey struct {
	Type        string
	Fingerprint string
	Comment     string
}

// DoctorRepoConn is the gateway push URL a dev box points its origin at.
type DoctorRepoConn struct {
	Name    string
	PushURL string // ssh://git@<host>:<push port>/~/<name>.git
}

// DoctorReport is the full read-only preflight result.
type DoctorReport struct {
	Checks  []DoctorCheck
	Host    string
	Install string // the install shape whose remediation the checks quote
	Keys    []DoctorKey
	Repos   []DoctorRepoConn
	HasFail bool
}

// DoctorConfig drives RunDoctor. All inputs are paths/flags; nothing is mutated.
type DoctorConfig struct {
	PolicyRoot         string
	ReposRoot          string
	AuthorizedKeysPath string
	Host               string
	Version            string
	RepoFilter         string
	Offline            bool

	// Profile supplies the facts doctor cannot observe about this install (see
	// InstallProfile). The zero value resolves from the deploy path's own
	// declaration; tests set it directly.
	Profile InstallProfile

	// PushPort is the port a dev box pushes to, as declared by the deploy path
	// through NIMBLEGATE_PUBLIC_SSH_PORT. The gateway cannot always observe it:
	// inside the container the probe reaches sshd's internal 22 while operators
	// push to the published port. 0 leaves it to the install profile.
	PushPort int

	// GatePorts are the loopback ports the SSH-gate reachability check dials.
	// Empty means probe the defaults (2222 for the container publish, 22 for a
	// bare-metal sshd).
	GatePorts []int

	// UpstreamAuthCheck is a test seam. If nil, RunDoctor uses the real
	// registry-based check.
	UpstreamAuthCheck func(upstreamURL, cred string) error
}

// bareMetalGitKeys is sshd's default authorized_keys file for the git user on a
// bare-metal install. The dashboard manages its own path; on bare-metal the two
// must be bridged (symlink) or sshd never sees dashboard-added keys. A var (not
// const) so tests can point it at a temp file.
var bareMetalGitKeys = "/home/git/.ssh/authorized_keys"

// RunDoctor assembles the diagnostics report. Every check is read-only: it never
// reconciles, writes, or mutates upstream.
func RunDoctor(cfg DoctorConfig) DoctorReport {
	host := cfg.Host
	if host == "" {
		host = "<host>"
	}
	rep := DoctorReport{Host: host}
	add := func(c DoctorCheck) {
		if c.Status == DoctorFail {
			rep.HasFail = true
		}
		rep.Checks = append(rep.Checks, c)
	}

	prof := cfg.Profile
	if prof.Name == "" {
		prof = ResolveInstallProfile()
	}
	rep.Install = prof.Name
	if prof.Name == ProfileUnknown.Name {
		add(DoctorCheck{
			Name:   "Install",
			Status: DoctorInfo,
			Reason: "install shape not declared; the advice below names every shape rather than yours",
			Fix:    "set NIMBLEGATE_INSTALL=container or bare-metal so doctor prints only the commands that work here",
		})
	} else {
		add(DoctorCheck{Name: "Install", Status: DoctorOK, Reason: prof.Name + " (remediation below is written for this shape)"})
	}

	// A [gateway] knob in the wrong file parses cleanly and does nothing, so the
	// only symptom is a setting that never takes effect. Name it here too: the
	// CLI is the lifeline when the dashboard is unreachable.
	if _, issues := InspectGatewayConfig(cfg.PolicyRoot); len(issues) > 0 {
		for _, issue := range issues {
			add(DoctorCheck{
				Name:   "Gateway config",
				Status: DoctorWarn,
				Reason: issue,
				Fix:    "machine-level knobs live in <policy-root>/gateway.toml under a [gateway] header",
			})
		}
	}

	ver := cfg.Version
	if ver == "" {
		ver = version.Resolved()
	}
	add(DoctorCheck{
		Name:   "Version",
		Status: DoctorInfo,
		Reason: ver,
		Fix:    "stale binary? confirm this matches what you deployed",
	})

	switch {
	case isLoopbackHostHint(cfg.Host):
		add(DoctorCheck{
			Name:   "Dashboard bind host",
			Status: DoctorWarn,
			Reason: "dashboard reached on a loopback address (" + cfg.Host + ")",
			Fix:    prof.BindFix,
		})
	case cfg.Host == "":
		add(DoctorCheck{
			Name:   "Dashboard bind host",
			Status: DoctorInfo,
			Reason: "host not supplied; connect URLs below use a placeholder - substitute your gateway's reachable address",
		})
	default:
		add(DoctorCheck{
			Name:   "Dashboard bind host",
			Status: DoctorOK,
			Reason: "reachable host " + cfg.Host,
		})
	}

	probedPort := 0
	if !cfg.Offline {
		ports := cfg.GatePorts
		if len(ports) == 0 {
			ports = []int{2222, 22}
		}
		reached := 0
		for _, p := range ports {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", p), 2*time.Second)
			if err == nil {
				_ = conn.Close()
				reached = p
				break
			}
		}
		probedPort = reached
		if reached != 0 {
			add(DoctorCheck{Name: "SSH gate", Status: DoctorOK, Reason: fmt.Sprintf("reachable on 127.0.0.1:%d", reached)})
		} else {
			add(DoctorCheck{
				Name:   "SSH gate",
				Status: DoctorWarn,
				Reason: fmt.Sprintf("could not reach the SSH gate on 127.0.0.1 (tried %s) from here; if pushes fail with connection-refused, the gate is not listening", joinPorts(ports)),
			})
		}
	}

	if cfg.AuthorizedKeysPath == "" {
		add(DoctorCheck{
			Name:   "Authorized keys",
			Status: DoctorFail,
			Reason: "SSH key management not configured (no authorized_keys path); no dev box can be authorized",
		})
	} else {
		rep.Keys = parseAuthorizedKeys(cfg.AuthorizedKeysPath)
		switch {
		case len(rep.Keys) > 0:
			add(DoctorCheck{
				Name:   "Authorized keys",
				Status: DoctorOK,
				Reason: fmt.Sprintf("%d key(s) authorized at %s", len(rep.Keys), cfg.AuthorizedKeysPath),
			})
		case splitKeysAt(cfg.AuthorizedKeysPath) != nil:
			// Bare-metal split: the dashboard manages an empty/absent path while
			// sshd reads keys from /home/git/.ssh/authorized_keys directly.
			bm := splitKeysAt(cfg.AuthorizedKeysPath)
			rep.Keys = bm
			add(DoctorCheck{
				Name:   "Authorized keys",
				Status: DoctorWarn,
				Reason: fmt.Sprintf("%d key(s) found at %s (what sshd reads), but the dashboard manages %s; pushes work, yet dashboard key changes will not take effect", len(bm), bareMetalGitKeys, cfg.AuthorizedKeysPath),
				Fix:    fmt.Sprintf("unify so the dashboard manages the file sshd reads (preserves existing keys): mkdir -p %s; cp %s %s; chown git:git %s; chmod 600 %s; ln -sf %s %s; chown -h git:git %s", filepath.Dir(cfg.AuthorizedKeysPath), bareMetalGitKeys, cfg.AuthorizedKeysPath, cfg.AuthorizedKeysPath, cfg.AuthorizedKeysPath, cfg.AuthorizedKeysPath, bareMetalGitKeys, bareMetalGitKeys),
			})
		default:
			add(DoctorCheck{
				Name:   "Authorized keys",
				Status: DoctorFail,
				Reason: "no SSH keys authorized; no dev box can push",
				Fix:    "add a dev box key on the dashboard's /ssh-keys page",
			})
		}
	}

	// Latest live relay outcome per repo. post-receive records one per push in
	// both install shapes, so this is the only relay signal a container ever
	// has - it runs no reconcile backstop. Read once; ReadEvents is
	// chronological, so the last write per repo wins.
	lastRelayOK := map[string]bool{}
	if evs, err := ReadEvents(cfg.PolicyRoot, func(e Event) bool {
		return e.Event == "relay-ok" || e.Event == "relay-failed"
	}); err == nil {
		for _, e := range evs {
			lastRelayOK[e.Repo] = e.Event == "relay-ok"
		}
	}

	pushPort := prof.PushPort(cfg.PushPort, probedPort)

	allRepos := doctorListRepos(cfg.PolicyRoot)
	if len(allRepos) == 0 {
		add(DoctorCheck{Name: "Repos", Status: DoctorWarn, Reason: "no repos registered yet"})
	} else {
		add(DoctorCheck{Name: "Repos", Status: DoctorOK, Reason: fmt.Sprintf("%d repo(s) registered", len(allRepos))})
	}

	// Drift recovery is one service for the whole install, so report it once
	// rather than per repo. A shape that runs no backstop has nothing to start
	// and gets no check instead of advice it cannot follow.
	if prof.RelayBackstop != "" && len(allRepos) > 0 {
		ran := false
		for _, name := range allRepos {
			if _, ok := ReadRelayStatus(cfg.PolicyRoot, name); ok {
				ran = true
				break
			}
		}
		if ran {
			add(DoctorCheck{Name: "Relay backstop", Status: DoctorOK, Reason: "reconcile records present"})
		} else {
			add(DoctorCheck{
				Name:   "Relay backstop",
				Status: DoctorInfo,
				Reason: "never run: no repo has a reconcile record, so a ref the upstream missed is not re-pushed automatically",
				Fix:    prof.RelayBackstop,
			})
		}
	}

	for _, name := range allRepos {
		if cfg.RepoFilter != "" && name != cfg.RepoFilter {
			continue
		}
		doctorCheckRepo(&rep, add, cfg, name, host, pushPort, lastRelayOK)
	}

	return rep
}

func doctorCheckRepo(rep *DoctorReport, add func(DoctorCheck), cfg DoctorConfig, name, host string, pushPort int, lastRelayOK map[string]bool) {
	if barePath, err := resolveRepoBare(cfg.ReposRoot, name); err != nil {
		add(DoctorCheck{
			Repo:   name,
			Name:   "Bare repo",
			Status: DoctorFail,
			Reason: fmt.Sprintf("bare repo missing/not active: %v; register or Sync from upstream", err),
		})
	} else {
		add(DoctorCheck{Repo: name, Name: "Bare repo", Status: DoctorOK, Reason: barePath})
		rep.Repos = append(rep.Repos, DoctorRepoConn{
			Name:    name,
			PushURL: fmt.Sprintf("ssh://git@%s:%d/~/%s.git", host, pushPort, name),
		})
	}

	pol, err := (FilePolicyStore{Root: cfg.PolicyRoot}).Load(name)
	if err != nil {
		add(DoctorCheck{Repo: name, Name: "Policy", Status: DoctorFail, Reason: fmt.Sprintf("load policy: %v", err)})
		return
	}

	switch {
	case pol.UpstreamURL == "":
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorFail, Reason: "no upstream URL configured; accepted pushes have nowhere to relay"})
	case strings.HasPrefix(pol.UpstreamURL, "https://"):
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorOK, Reason: pol.UpstreamURL})
	case IsSSHUpstream(pol.UpstreamURL):
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorOK, Reason: pol.UpstreamURL + " (relays over SSH with the gateway's own identity)"})
	case strings.HasPrefix(pol.UpstreamURL, "http://"):
		// Supported on purpose for LAN gitea / on-prem upstreams (relay.go
		// injects the token for http as well as https). Not a failure - the
		// relay works - but the credential crosses the network in cleartext,
		// which the operator should know rather than be blocked over.
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorWarn,
			Reason: "upstream is plain HTTP (" + pol.UpstreamURL + "); the relay works, but the credential travels in cleartext",
			Fix:    "prefer https:// where the upstream offers it; http is intended for LAN/on-prem hosts"})
	default:
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorFail,
			Reason: "unsupported upstream scheme (" + pol.UpstreamURL + "); the relay speaks https, http and ssh"})
	}

	cred, _ := (FileCredentialStore{Root: cfg.PolicyRoot}).Load(name)
	switch {
	case IsSSHUpstream(pol.UpstreamURL):
		// SSH upstreams authenticate with the gateway's key, so a per-repo
		// credential file is not part of that path at all.
		add(DoctorCheck{Repo: name, Name: "Upstream credential", Status: DoctorInfo, Reason: "not applicable: SSH upstream authenticates with the gateway's key"})
	case strings.TrimSpace(cred) == "":
		add(DoctorCheck{Repo: name, Name: "Upstream credential", Status: DoctorWarn, Reason: "no upstream credential stored; relay to upstream will fail"})
	default:
		add(DoctorCheck{Repo: name, Name: "Upstream credential", Status: DoctorOK, Reason: "credential present"})
	}

	switch {
	case len(pol.ProtectedRefs) == 0 && !pol.GateAllRefs:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorFail, Reason: "nothing gated; every push passes unchecked"})
	case !pol.GateAllRefs && len(pol.ProtectedRefs) == 1 && pol.ProtectedRefs[0] == "refs/heads/main":
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorWarn, Reason: "only main is gated; agent feature branches are unchecked and the auto-PR loop will not fire on them"})
	case pol.GateAllRefs:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorOK, Reason: "every ref is gated"})
	case !isGatedRef(pol, "refs/heads/agent/example"):
		// Probe with a nested name rather than inspecting the patterns: whatever
		// the operator wrote, this is the question that matters, and an ungated
		// push leaves no audit row to notice after the fact.
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorFail,
			Reason: fmt.Sprintf("nested branch names (agent/x, feature/x, dependabot/x/y) are NOT gated by %s; those pushes relay unchecked and the auto-PR loop cannot fire on them", strings.Join(pol.ProtectedRefs, ", ")),
			Fix:    "use a pattern ending in /* (e.g. refs/heads/*), which gates every branch at any depth"})
	default:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorOK, Reason: fmt.Sprintf("%d protected ref pattern(s): %s", len(pol.ProtectedRefs), strings.Join(pol.ProtectedRefs, ", "))})
	}

	fp, _ := LoadFramePolicy(cfg.PolicyRoot, name)
	switch {
	case len(fp.Enabled) == 0:
		add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorOK,
			Reason: "no explicit selection, so every stdlib frame is active (an empty allowlist is not consulted)"})
	default:
		// An entry that resolves to no frame matches nothing. Counting it as
		// active overstates coverage, and a list of ONLY such entries means no
		// frames run at all - worse than the empty list it replaced.
		dead := UnresolvableFrameEntries(fp.Enabled)
		live := len(fp.Enabled) - len(dead)
		switch {
		case live == 0:
			add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorFail,
				Reason: fmt.Sprintf("%d entr(ies) in the allowlist and NONE name a frame (%s); nothing is checked and pushes relay unchecked",
					len(fp.Enabled), strings.Join(dead, ", ")),
				Fix: "remove those entries (an empty list runs every frame), or apply a kit so real frame IDs are written"})
		case len(dead) > 0:
			add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorWarn,
				Reason: fmt.Sprintf("%d frame(s) active of %d entries - %s name no frame and do nothing",
					live, len(fp.Enabled), strings.Join(dead, ", ")),
				Fix: "remove the dead entries; if they are kit names, apply the kit so its frame IDs are written"})
		default:
			add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorOK,
				Reason: fmt.Sprintf("%d frame(s) active - an explicit allowlist, so only these run", len(fp.Enabled))})
		}
	}

	if pol.Notification == nil || !pol.Notification.Enabled {
		add(DoctorCheck{Repo: name, Name: "Notifications", Status: DoctorInfo, Reason: "notifications off; rejected pushes will not post a PR comment (auto-PR loop inactive)"})
	} else {
		add(DoctorCheck{Repo: name, Name: "Notifications", Status: DoctorOK, Reason: "notifications on"})
	}

	// Relay health from two read-only sources, no network: every push records
	// its own outcome (the signal /repos reads, and the only one a container
	// has), and the reconcile backstop records drift where it runs. A failure
	// from either wins - the point of the check is a gate that accepts pushes
	// the upstream never receives.
	rs, haveStatus := ReadRelayStatus(cfg.PolicyRoot, name)
	pushOK, havePush := lastRelayOK[name]
	switch {
	case haveStatus && !rs.OK:
		add(DoctorCheck{Repo: name, Name: "Relay", Status: DoctorFail, Reason: "relay failing: " + rs.Error, Fix: "check the upstream token/host; see gateway logs"})
	case havePush && !pushOK:
		add(DoctorCheck{
			Repo:   name,
			Name:   "Relay",
			Status: DoctorFail,
			Reason: "the last accepted push did not reach the upstream; the gate accepted it and the upstream never got it",
			Fix:    "check the upstream credential and that the upstream host is reachable from the gateway; see gateway logs",
		})
	case haveStatus && rs.DriftedRefs > 0:
		add(DoctorCheck{Repo: name, Name: "Relay", Status: DoctorWarn, Reason: fmt.Sprintf("last reconcile re-pushed %d ref(s) the upstream was missing", rs.DriftedRefs)})
	case havePush:
		add(DoctorCheck{Repo: name, Name: "Relay", Status: DoctorOK, Reason: "last push relayed to the upstream"})
	case haveStatus:
		add(DoctorCheck{Repo: name, Name: "Relay", Status: DoctorOK, Reason: "relay healthy"})
	default:
		add(DoctorCheck{Repo: name, Name: "Relay", Status: DoctorInfo, Reason: "nothing relayed yet; push once to see relay health here"})
	}

	if !cfg.Offline && strings.HasPrefix(pol.UpstreamURL, "https://") {
		check := cfg.UpstreamAuthCheck
		if check == nil {
			check = func(u, c string) error { return realUpstreamAuthCheck(u, name, c) }
		}
		if err := check(pol.UpstreamURL, cred); err != nil {
			c := DoctorCheck{Repo: name, Name: "Upstream auth", Status: DoctorFail, Reason: fmt.Sprintf("upstream auth failed: %v", err)}
			if doctorPermissionError(err) {
				c.Fix = doctorScopeHint(pol.UpstreamURL)
			}
			add(c)
		} else {
			add(DoctorCheck{Repo: name, Name: "Upstream auth", Status: DoctorOK, Reason: "upstream reachable, token authenticates"})
		}
	}
}

func realUpstreamAuthCheck(upstreamURL, repo, cred string) error {
	var adapter upstream.Upstream
	if strings.Contains(upstreamURL, "github.com") {
		adapter = upstream.NewGitHubAdapter(upstreamURL, cred)
	} else {
		adapter = upstream.NewGiteaAdapter(upstreamURL, cred)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_, err := adapter.FindPRForRef(ctx, repo, "refs/heads/main")
	return err
}

func doctorPermissionError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "403") || strings.Contains(s, "forbidden") || strings.Contains(s, "permission")
}

func doctorScopeHint(upstreamURL string) string {
	switch {
	case strings.Contains(upstreamURL, "github.com"):
		return "token scope: classic token with repo, or fine-grained with Contents read+write, Issues read+write, Pull requests read"
	case strings.Contains(strings.ToLower(upstreamURL), "gitlab"):
		return "token scope: api"
	default:
		return "token scope (Gitea): write"
	}
}

// doctorListRepos enumerates policy-configured repos the same way the dashboard
// chrome does (one gateway.toml per repo dir under policyRoot).
func doctorListRepos(policyRoot string) []string {
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	var out []string
	for _, m := range matches {
		out = append(out, filepath.Base(filepath.Dir(m)))
	}
	sort.Strings(out)
	return out
}

func parseAuthorizedKeys(path string) []DoctorKey {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []DoctorKey
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pk, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}
		out = append(out, DoctorKey{
			Type:        pk.Type(),
			Fingerprint: ssh.FingerprintSHA256(pk),
			Comment:     comment,
		})
	}
	return out
}

func joinPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf("%d", p)
	}
	return strings.Join(parts, ", ")
}

// splitKeysAt reports a bare-metal keys-path split: keys present at sshd's
// default git authorized_keys file while the dashboard manages a different
// (empty/absent) path. Returns nil when there is no split (same path, or no keys
// at the sshd default).
func splitKeysAt(configuredPath string) []DoctorKey {
	if configuredPath == bareMetalGitKeys {
		return nil
	}
	keys := parseAuthorizedKeys(bareMetalGitKeys)
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func isLoopbackHostHint(h string) bool {
	h = strings.TrimSpace(strings.ToLower(h))
	if h == "" {
		return false
	}
	if h == "localhost" || h == "::1" || strings.HasPrefix(h, "127.") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}
