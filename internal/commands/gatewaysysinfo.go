// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"nimblegate/internal/version"
)

// sysInfo is the read-only system metadata shown on the Settings page.
// Answers "what kind of install is this?" / "where do my files live?" / "what
// version am I running?" without needing to look at systemd unit files or
// command-line flags.
type sysInfo struct {
	Version     string // resolved binary version (ldflags or VCS-embedded)
	BuildSHA    string // short SHA if extractable
	BuildDirty  bool   // vcs.modified at build time - uncommitted edits baked in
	BuildVCS    string // "git" if VCS info available
	BuildDate   string // build timestamp from runtime/debug if available
	GoVersion   string // e.g. "go1.25"
	OS          string // runtime.GOOS
	Arch        string // runtime.GOARCH
	Hostname    string // os.Hostname() - acts as instance ID
	InstallType string // "container (nimblegate image)" | "container (other)" | "bare metal" | "unknown"
	Uptime      string // dashboard process uptime
	PolicyRoot  string
	ReposRoot   string
	AuthMode    string // "setup-token" | "off"
	AllowEdits  bool
	Repos       int // count of registered repos
}

// collectSysInfo gathers the metadata once per page render. Read-only; safe
// to call from a GET handler.
func collectSysInfo(policyRoot, reposRoot, authMode string, allowEdits bool, startTime, now time.Time) sysInfo {
	info := sysInfo{
		Version:    version.Resolved(),
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Uptime:     formatUptime(now.Sub(startTime)),
		PolicyRoot: policyRoot,
		ReposRoot:  reposRoot,
		AuthMode:   authMode,
		AllowEdits: allowEdits,
	}

	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// Build details from the version package's VCS introspection.
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs":
				info.BuildVCS = s.Value
			case "vcs.revision":
				if len(s.Value) >= 7 {
					info.BuildSHA = s.Value[:7]
				} else {
					info.BuildSHA = s.Value
				}
			case "vcs.time":
				info.BuildDate = s.Value
			case "vcs.modified":
				info.BuildDirty = s.Value == "true"
			}
		}
	}
	if info.BuildSHA == "" {
		// version.Resolved() may itself be the SHA (when ldflags is used).
		if v := info.Version; len(v) >= 7 && isHex(v[:7]) {
			info.BuildSHA = v[:7]
		}
	}

	info.InstallType = detectInstallType()

	// Repo count - reuse the same active-repo resolver the /repos page uses
	// (globs <policyRoot>/*/gateway.toml through the activation symlinks). A
	// naive os.ReadDir + IsDir() misses the symlinks and reports 0.
	if policyRoot != "" {
		info.Repos = len(listGatewayRepos(policyRoot))
	}

	return info
}

// authModeLabel renders the --auth posture in human terms. The raw flag value
// "setup-token" reads like "you still need a setup token" once the admin is
// claimed, but it actually means single-admin password login (bootstrapped once
// via a setup token). "off" means there is no login wall.
func authModeLabel(mode string) string {
	switch mode {
	case "setup-token":
		return `single-admin login <span class="sub">(password: bootstrapped once via a setup token)</span>`
	case "off":
		return `disabled <span class="sub">(no login wall: front with a reverse proxy, or trusted LAN only)</span>`
	default:
		return `<code>` + mode + `</code>`
	}
}

// detectInstallType looks for filesystem markers that identify the runtime
// environment. Conservative: only labels with high confidence.
func detectInstallType() string {
	// nimblegate container variant ships s6-overlay v3.
	if _, err := os.Stat("/etc/s6-overlay"); err == nil {
		return "container (nimblegate image)"
	}
	// Docker-style container: /.dockerenv is created by the docker runtime.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "container (Docker)"
	}
	// Generic containerized: cgroup line will mention docker/containerd/kubepods.
	if b, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(b)
		switch {
		case strings.Contains(s, "kubepods"):
			return "container (Kubernetes pod)"
		case strings.Contains(s, "containerd"):
			return "container (containerd)"
		case strings.Contains(s, "docker"):
			return "container (Docker)"
		}
	}
	// Systemd unit running on a bare host - the dashboard service is typically
	// `nimblegate-dashboard.service`. Best-effort check.
	if _, err := os.Stat("/etc/systemd/system/nimblegate-dashboard.service"); err == nil {
		return "bare metal (systemd unit)"
	}
	if _, err := os.Stat("/lib/systemd/system/nimblegate-dashboard.service"); err == nil {
		return "bare metal (systemd unit)"
	}
	return "bare metal (or unknown)"
}

// expectedCleanInstall reports whether the install type is one normally built
// via CI/release pipeline (container image, systemd-deployed bare metal) where
// `dirty:true` indicates someone bypassed the pipeline. Returns false for
// dev/unknown installs where dirty is expected and informational.
func expectedCleanInstall(installType string) bool {
	switch installType {
	case "container (nimblegate image)",
		"container (Docker)",
		"container (Kubernetes pod)",
		"container (containerd)",
		"bare metal (systemd unit)":
		return true
	default:
		return false
	}
}

func isHex(s string) bool {
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// renderSysInfoSection writes the System info section's HTML. Pure function
// over sysInfo - keeps the template inline so adding a row is one edit.
func renderSysInfoSection(info sysInfo) string {
	var b strings.Builder
	b.WriteString(`<section class="frame gw-sysinfo">`)
	b.WriteString(`<h3 class="gw-section-head">System info</h3>`)
	b.WriteString(`<p class="sub">Read-only metadata about this gateway install. Useful when filing an issue or comparing against another deployment.</p>`)
	b.WriteString(`<table class="gw-sysinfo-table">`)
	row := func(label, value string) {
		if value == "" {
			value = "<span class=\"sub\">-</span>"
		}
		fmt.Fprintf(&b, `<tr><td class="gw-sysinfo-k">%s</td><td class="gw-sysinfo-v">%s</td></tr>`, label, value)
	}
	row("Install type", info.InstallType)
	// The dirty marker rides on the Version row because it's always rendered;
	// the Build SHA row may be suppressed when Version == BuildSHA (the
	// canonical CLAUDE.md build passes the SHA as version via ldflag), and
	// the marker has to be visible regardless of that overlap.
	versionCell := info.Version
	if info.BuildDirty {
		// Context-aware framing: container/systemd installs are normally built
		// via CI/goreleaser from a clean checkout, so dirty there is a yellow
		// flag worth investigating (hand-patched binary?); bare-metal/unknown
		// installs are often dev or one-off, where dirty is expected.
		if expectedCleanInstall(info.InstallType) {
			versionCell += ` <span class="gw-dirty gw-dirty-warn" title="Built from a working tree with uncommitted changes: vcs.modified=true. On a release-style install this is unexpected, likely a hand-patched binary deployed outside the CI/release pipeline. Worth investigating who built and deployed this.">(dirty)</span> <span class="sub">unexpected on this install type, worth investigating</span>`
		} else {
			versionCell += ` <span class="gw-dirty gw-dirty-info" title="Built from a working tree with uncommitted changes: vcs.modified=true. Normal for local dev builds (go build against a working tree); informational only. A CI/release build from a clean checkout would show clean.">(dirty)</span> <span class="sub">expected for dev builds, informational only</span>`
		}
	}
	row("Version", versionCell)
	if info.BuildSHA != "" && info.BuildSHA != info.Version {
		row("Build SHA", `<code>`+info.BuildSHA+`</code>`)
	}
	if info.BuildDate != "" {
		row("Build date", `<code>`+info.BuildDate+`</code>`)
	}
	if info.BuildVCS != "" {
		row("VCS", info.BuildVCS)
	}
	row("Go version", `<code>`+info.GoVersion+`</code>`)
	row("OS / arch", `<code>`+info.OS+`/`+info.Arch+`</code>`)
	row("Hostname", `<code>`+info.Hostname+`</code>`)
	row("Uptime", info.Uptime)
	row("Policy root", `<code>`+info.PolicyRoot+`</code>`)
	row("Repos root", `<code>`+info.ReposRoot+`</code>`)
	row("Registered repos", fmt.Sprintf("%d", info.Repos))
	row("Auth mode", authModeLabel(info.AuthMode))
	editsLabel := "read-only (start with <code>--allow-edits</code> to enable writes)"
	if info.AllowEdits {
		editsLabel = "enabled (<code>--allow-edits</code>)"
	}
	row("Edit controls", editsLabel)
	b.WriteString(`</table>`)
	b.WriteString(`<style>.gw-sysinfo-table{width:100%;border-collapse:collapse}.gw-sysinfo-table td{padding:6px 10px;border-bottom:1px solid var(--gw-border-soft);font-size:13px;vertical-align:top}.gw-sysinfo-k{width:32%;color:var(--gw-text-muted)}.gw-sysinfo-v code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;color:var(--gw-text-soft);background:var(--gw-bg-soft);padding:1px 6px;border-radius:3px}.gw-dirty{font-size:11px;padding:1px 6px;border-radius:3px;margin-left:6px;cursor:help}.gw-dirty-warn{background:#5a3a14;color:#ffc97a;border:1px solid #8a5a20}.gw-dirty-info{background:var(--gw-bg-soft);color:var(--gw-text-muted);border:1px solid var(--gw-border-soft)}</style>`)
	b.WriteString(`</section>`)
	return b.String()
}
