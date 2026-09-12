// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSafeDirectoryAllows(t *testing.T) {
	for _, c := range []struct {
		name   string
		values []string
		want   bool
	}{
		{"nothing set", nil, false},
		{"exact", []string{"/srv/m.git"}, true},
		{"exact with trailing slash", []string{"/srv/m.git/"}, true},
		{"another repository", []string{"/srv/other.git"}, false},
		{"everything", []string{"*"}, true},
		{"directory prefix", []string{"/srv/*"}, true},
		{"not a directory prefix", []string{"/sr/*"}, false},
		{"cleared", []string{"*", ""}, false},
		{"cleared, then allowed", []string{"", "/srv/m.git"}, true},
	} {
		if got := safeDirectoryAllows(c.values, "/srv/m.git"); got != c.want {
			t.Errorf("%s: safeDirectoryAllows(%q) = %v, want %v", c.name, c.values, got, c.want)
		}
	}
}

// The global config is read from the account's home, which is how the relay
// service's account sees an entry added for it alone.
func TestSafeDirectoryValuesReadsGlobalConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[safe]\n\tdirectory = /srv/from-global.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if vals := safeDirectoryValues(home); !slices.Contains(vals, "/srv/from-global.git") {
		t.Errorf("safe.directory from the home config missing: %q", vals)
	}
}

func TestDoctorCheckLocalUpstreamAccess(t *testing.T) {
	reposRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(reposRoot, "demo.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DoctorConfig{ReposRoot: reposRoot}
	check := func(upstream string, values []string) DoctorCheck {
		t.Helper()
		orig := safeDirectoryValuesFor
		safeDirectoryValuesFor = func(string) []string { return values }
		defer func() { safeDirectoryValuesFor = orig }()
		var got []DoctorCheck
		doctorCheckLocalUpstreamAccess(func(c DoctorCheck) { got = append(got, c) }, cfg, "demo", upstream)
		if len(got) != 1 {
			t.Fatalf("want one check, got %+v", got)
		}
		return got[0]
	}

	// The gateway repo's owner owns the upstream too: nothing for git to refuse.
	if c := check(t.TempDir(), nil); c.Status != DoctorOK {
		t.Errorf("same owner: want OK, got %+v", c)
	}

	if os.Geteuid() == 0 {
		t.Skip("root owns the test repo, so no other-owner case to build")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	// /usr stands in for an upstream another user (root) owns.
	c := check("/usr", nil)
	if c.Status != DoctorWarn || !strings.Contains(c.Reason, "owned by root") || !strings.Contains(c.Reason, "relays run as "+me.Username) {
		t.Errorf("root-owned upstream: want WARN naming both accounts, got %+v", c)
	}
	if c.Fix != "git config --system --add safe.directory /usr" {
		t.Errorf("fix = %q", c.Fix)
	}
	if c := check("/usr", []string{"/usr"}); c.Status != DoctorOK {
		t.Errorf("allowed by safe.directory: want OK, got %+v", c)
	}
}
