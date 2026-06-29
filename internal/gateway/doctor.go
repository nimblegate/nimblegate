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
	PushURL string // ssh://git@<host>:2222/~/<name>.git
}

// DoctorReport is the full read-only preflight result.
type DoctorReport struct {
	Checks  []DoctorCheck
	Host    string
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

	// UpstreamAuthCheck is a test seam. If nil, RunDoctor uses the real
	// registry-based check.
	UpstreamAuthCheck func(upstreamURL, cred string) error
}

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
			Fix:    "remote box? tunnel: ssh -L 7900:127.0.0.1:7900 user@host (use 127.0.0.1 not localhost - Docker publishes on IPv4), or set NIMBLEGATE_DASHBOARD_HOST=0.0.0.0 behind a proxy",
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

	if !cfg.Offline {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:2222", 2*time.Second)
		if err == nil {
			_ = conn.Close()
			add(DoctorCheck{Name: "SSH gate", Status: DoctorOK, Reason: "reachable on 127.0.0.1:2222"})
		} else {
			add(DoctorCheck{
				Name:   "SSH gate",
				Status: DoctorWarn,
				Reason: "could not reach the SSH gate on 127.0.0.1:2222 from here; if pushes fail with connection-refused, the gate is not listening",
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
		if len(rep.Keys) == 0 {
			add(DoctorCheck{
				Name:   "Authorized keys",
				Status: DoctorFail,
				Reason: "no SSH keys authorized; no dev box can push",
				Fix:    "add a dev box key at /ssh-keys",
			})
		} else {
			add(DoctorCheck{
				Name:   "Authorized keys",
				Status: DoctorOK,
				Reason: fmt.Sprintf("%d key(s) authorized", len(rep.Keys)),
			})
		}
	}

	allRepos := doctorListRepos(cfg.PolicyRoot)
	if len(allRepos) == 0 {
		add(DoctorCheck{Name: "Repos", Status: DoctorWarn, Reason: "no repos registered yet"})
	} else {
		add(DoctorCheck{Name: "Repos", Status: DoctorOK, Reason: fmt.Sprintf("%d repo(s) registered", len(allRepos))})
	}

	for _, name := range allRepos {
		if cfg.RepoFilter != "" && name != cfg.RepoFilter {
			continue
		}
		doctorCheckRepo(&rep, add, cfg, name, host)
	}

	return rep
}

func doctorCheckRepo(rep *DoctorReport, add func(DoctorCheck), cfg DoctorConfig, name, host string) {
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
			PushURL: "ssh://git@" + host + ":2222/~/" + name + ".git",
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
	default:
		add(DoctorCheck{Repo: name, Name: "Upstream URL", Status: DoctorFail, Reason: "upstream must be HTTPS; the gateway relays over HTTPS only (" + pol.UpstreamURL + ")"})
	}

	cred, _ := (FileCredentialStore{Root: cfg.PolicyRoot}).Load(name)
	if strings.TrimSpace(cred) == "" {
		add(DoctorCheck{Repo: name, Name: "Upstream credential", Status: DoctorWarn, Reason: "no upstream credential stored; relay to upstream will fail"})
	} else {
		add(DoctorCheck{Repo: name, Name: "Upstream credential", Status: DoctorOK, Reason: "credential present"})
	}

	switch {
	case len(pol.ProtectedRefs) == 0 && !pol.GateAllRefs:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorFail, Reason: "nothing gated; every push passes unchecked"})
	case !pol.GateAllRefs && len(pol.ProtectedRefs) == 1 && pol.ProtectedRefs[0] == "refs/heads/main":
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorWarn, Reason: "only main is gated; agent feature branches are unchecked and the auto-PR loop will not fire on them"})
	case pol.GateAllRefs:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorOK, Reason: "every ref is gated"})
	default:
		add(DoctorCheck{Repo: name, Name: "Gated refs", Status: DoctorOK, Reason: fmt.Sprintf("%d protected ref pattern(s): %s", len(pol.ProtectedRefs), strings.Join(pol.ProtectedRefs, ", "))})
	}

	fp, _ := LoadFramePolicy(cfg.PolicyRoot, name)
	if len(fp.Enabled) == 0 {
		add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorFail, Reason: "no frames/rules active; pushes relay unchecked"})
	} else {
		add(DoctorCheck{Repo: name, Name: "Frames", Status: DoctorOK, Reason: fmt.Sprintf("%d frame(s) active", len(fp.Enabled))})
	}

	if pol.Notification == nil || !pol.Notification.Enabled {
		add(DoctorCheck{Repo: name, Name: "Notifications", Status: DoctorInfo, Reason: "notifications off; rejected pushes will not post a PR comment (auto-PR loop inactive)"})
	} else {
		add(DoctorCheck{Repo: name, Name: "Notifications", Status: DoctorOK, Reason: "notifications on"})
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
