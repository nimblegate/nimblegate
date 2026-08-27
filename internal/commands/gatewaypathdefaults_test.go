package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The gateway's --policy-root / --repos-root defaults must name the layout that
// actually ships: /srv/gateway/cfg and /srv/gateway/repos, per
// deploy/container/s6-rc.d/dashboard/run and deploy/gateway/*.service. The
// pre-0.4 defaults pointed at /etc/nimblegate-gateway and /srv/nimblegate-gateway,
// which nothing installs to, so every subcommand run without flags reported on an
// empty tree - `gateway doctor` said "no repos registered yet" on a working
// gateway and skipped the per-repo upstream probes with it.
func TestGatewayPathDefaultsMatchShippedLayout(t *testing.T) {
	stale := regexp.MustCompile(`nimblegate-gateway/repos`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		if stale.Match(b) {
			t.Errorf("%s names a nimblegate-gateway path; the shipped roots are /srv/gateway/cfg and /srv/gateway/repos", name)
		}
	}
}
