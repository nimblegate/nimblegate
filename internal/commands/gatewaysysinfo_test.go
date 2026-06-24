// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/gateway"
)

// The raw --auth value "setup-token" reads like an unfinished to-do once the
// admin is claimed; the System panel should show a human-friendly label.
func TestRenderSysInfo_authModeFriendlyLabel(t *testing.T) {
	got := renderSysInfoSection(sysInfo{Version: "x", AuthMode: "setup-token"})
	if !strings.Contains(got, "single-admin login") {
		t.Errorf("setup-token should render as 'single-admin login', got:\n%s", got)
	}
	if strings.Contains(got, "<code>setup-token</code>") {
		t.Errorf("the bare 'setup-token' code value should not be shown:\n%s", got)
	}
	off := renderSysInfoSection(sysInfo{Version: "x", AuthMode: "off"})
	if !strings.Contains(off, "disabled") {
		t.Errorf("off should render as 'disabled (no login wall)', got:\n%s", off)
	}
}

// Registered repos live as activation symlinks (<policyRoot>/<name> ->
// _repos/<name>), so the System panel's repo count must resolve them the same
// way the /repos page does. A naive os.ReadDir + IsDir() check returns false
// for symlinks and reports 0 even when repos are registered - this guards that.
func TestCollectSysInfo_countsActivationSymlinkRepos(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "cfg")
	reposRoot := filepath.Join(tmp, "repos")
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := gateway.AddRepo(gateway.AddOptions{
			Name: name, UpstreamURL: "http://x", Enabled: true,
			PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
		}); err != nil {
			t.Fatal(err)
		}
	}
	info := collectSysInfo(policyRoot, reposRoot, "off", false, time.Now(), time.Now())
	if info.Repos != 3 {
		t.Errorf("Repos = %d, want 3 (activation-symlink repos must be counted)", info.Repos)
	}
}

// When Version == BuildSHA (the canonical build that passes the SHA as the
// version via ldflag), the Build SHA row is suppressed as a duplicate - the
// dirty marker still needs to render on the Version row in that case.
func TestRenderSysInfo_dirtyFlagSurfacesEvenWhenVersionEqualsSHA(t *testing.T) {
	got := renderSysInfoSection(sysInfo{
		Version:     "d9fe903",
		BuildSHA:    "d9fe903",
		BuildDirty:  true,
		InstallType: "bare metal (or unknown)",
		GoVersion:   "go1.25",
		OS:          "linux",
		Arch:        "amd64",
		Uptime:      "1m 0s",
	})
	if !strings.Contains(got, "(dirty)") {
		t.Errorf("dirty=true should render the (dirty) suffix even when Version == BuildSHA\n%s", got)
	}
	if !strings.Contains(got, `title="Built from a working tree with uncommitted changes`) {
		t.Errorf("dirty suffix should carry the tooltip explanation\n%s", got)
	}
}

// When Version is a tag (e.g. "v0.1.0") and BuildSHA is the commit, the dirty
// marker rides on the Version row - the SHA row stays clean since it's just
// data, the dirty fact belongs to the build identity as a whole.
func TestRenderSysInfo_dirtyFlagOnVersionRowWhenSeparate(t *testing.T) {
	got := renderSysInfoSection(sysInfo{
		Version:     "v0.1.0",
		BuildSHA:    "abcdef0",
		BuildDirty:  true,
		InstallType: "bare metal (or unknown)",
		GoVersion:   "go1.25",
		OS:          "linux",
		Arch:        "amd64",
		Uptime:      "1m 0s",
	})
	if !strings.Contains(got, "v0.1.0 <span") || !strings.Contains(got, "(dirty)") {
		t.Errorf("dirty marker should sit next to Version v0.1.0\n%s", got)
	}
}

func TestRenderSysInfo_cleanBuildHasNoDirtyMarker(t *testing.T) {
	got := renderSysInfoSection(sysInfo{
		Version:     "d9fe903",
		BuildSHA:    "abcdef0",
		BuildDirty:  false,
		InstallType: "bare metal (or unknown)",
		GoVersion:   "go1.25",
		OS:          "linux",
		Arch:        "amd64",
		Uptime:      "1m 0s",
	})
	if strings.Contains(got, "(dirty)") {
		t.Errorf("clean build should not render (dirty)\n%s", got)
	}
}
