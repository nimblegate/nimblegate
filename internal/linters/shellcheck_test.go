// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/config"
)

// A script sourcing a sibling lib must produce zero findings: -x +
// --source-path=SCRIPTDIR make shellcheck follow the sourced file instead
// of emitting SC1091 "not following" per consumer. Pins the flag set -
// dropping either flag regresses to one false positive per consumer script.
func TestShellCheck_FollowsSourcedSiblingLib(t *testing.T) {
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck not on PATH")
	}
	root := t.TempDir()
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	lib := "#!/usr/bin/env bash\nlog() { echo \"$1\"; }\n"
	consumer := "#!/usr/bin/env bash\nsource \"$(dirname \"${BASH_SOURCE[0]}\")/lib.sh\"\nlog hello\n"
	if err := os.WriteFile(filepath.Join(scripts, "lib.sh"), []byte(lib), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "deploy.sh"), []byte(consumer), 0o755); err != nil {
		t.Fatal(err)
	}

	res := ShellCheck{}.Run(root, config.LinterConfig{Severity: "WARN"}, nil)
	for _, h := range res.Hits {
		if strings.Contains(h.Label, "SC1091") {
			t.Errorf("SC1091 false positive on sourced sibling lib: %+v", h)
		}
	}
	if len(res.Hits) != 0 {
		t.Errorf("clean scripts should produce zero hits, got %+v", res.Hits)
	}
}
