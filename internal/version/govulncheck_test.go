// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package version

import (
	"errors"
	"go/build"
	"os/exec"
	"path/filepath"
	"testing"
)

// The Go toolchain and the modules are not pinned to a patch, so a newly
// published vulnerability can make yesterday's green tree unsafe without a
// single commit. Running govulncheck with the suite is what surfaces that.
// It fails only on vulnerabilities the code actually reaches (exit 3), and
// skips rather than fails when the tool is missing or cannot reach the
// vulnerability database, so an offline run stays usable.
func TestGovulncheck(t *testing.T) {
	if testing.Short() {
		t.Skip("govulncheck needs the network; skipped under -short")
	}
	bin, err := exec.LookPath("govulncheck")
	if err != nil {
		bin = filepath.Join(build.Default.GOPATH, "bin", "govulncheck")
		if _, err := exec.LookPath(bin); err != nil {
			t.Skip("govulncheck not installed: go install golang.org/x/vuln/cmd/govulncheck@latest")
		}
	}
	cmd := exec.Command(bin, "./...")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit) && exit.ExitCode() == 3:
		t.Fatalf("govulncheck found reachable vulnerabilities:\n%s", out)
	default:
		t.Skipf("govulncheck could not run (%v):\n%s", err, out)
	}
}
