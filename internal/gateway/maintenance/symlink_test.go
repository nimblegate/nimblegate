// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Registered repos are activation symlinks (<policy-root>/<name> ->
// _repos/<name>); the prune passes must follow them.
func TestPrunes_FollowSymlinkedRepoDirs(t *testing.T) {
	policyRoot := t.TempDir()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	real := filepath.Join(policyRoot, "_repos", "demo")
	writeDeadletter(t, real, base, -40*24*time.Hour, -1*24*time.Hour)
	if err := os.WriteFile(filepath.Join(real, "audit.log"), []byte(`{"accept":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(policyRoot, "demo")); err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return base }

	if res := runDeadletterPrune(now, policyRoot, 30*24*time.Hour); len(res) != 1 || res[0].Pruned != 1 {
		t.Errorf("deadletter prune must reach the symlinked repo, got %+v", res)
	}
	if res := runAuditPrune(now, policyRoot, 30*24*time.Hour, 0); len(res) != 1 {
		t.Errorf("audit prune must reach the symlinked repo, got %+v", res)
	}

	reposRoot := t.TempDir()
	target := filepath.Join(t.TempDir(), "x.git")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(reposRoot, "x.git")); err != nil {
		t.Fatal(err)
	}
	if got := (&Runner{reposRoot: reposRoot}).findBareRepos(); len(got) != 1 {
		t.Errorf("gc must reach a symlinked bare repo, got %v", got)
	}
}
