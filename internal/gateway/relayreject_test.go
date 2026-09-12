// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRelayDiverged(t *testing.T) {
	for _, c := range []struct {
		name, text, branch string
		ok                 bool
	}{
		{"non-fast-forward", "relay to git@h:x/y.git failed: exit status 1\nTo h:x/y.git\n ! [rejected]        beaa6344cdb8b5f0e3617b846e1ef2ff41d40484 -> main (non-fast-forward)\nerror: failed to push some refs", "main", true},
		{"fetch first", " ! [rejected]        main -> main (fetch first)", "main", true},
		{"full ref name", " ! [rejected] 1111111 -> refs/heads/feature/x (non-fast-forward)", "feature/x", true},
		{"auth", "fatal: Authentication failed for 'https://h/x.git/'", "", false},
		{"hook declined", " ! [remote rejected] main -> main (pre-receive hook declined)", "", false},
		{"empty", "", "", false},
	} {
		branch, ok := RelayDiverged(c.text)
		if ok != c.ok || branch != c.branch {
			t.Errorf("%s: RelayDiverged = (%q, %v), want (%q, %v)", c.name, branch, ok, c.branch, c.ok)
		}
	}
}

func TestWithoutGitHints(t *testing.T) {
	in := "relay failed\n ! [rejected] a -> main (non-fast-forward)\nhint: Updates were rejected\nhint: use 'git pull'\n"
	if got, want := WithoutGitHints(in), "relay failed\n ! [rejected] a -> main (non-fast-forward)"; got != want {
		t.Errorf("WithoutGitHints = %q, want %q", got, want)
	}
}

// The markers come from git itself, so check them against a real rejection:
// "fetch first" while the gateway has never seen the upstream's commit, then
// "non-fast-forward" once it has the commit but its main does not contain it.
func TestRelay_divergedUpstreamIsRecognised(t *testing.T) {
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream.git")
	gw := filepath.Join(root, "gateway.git")
	work := filepath.Join(root, "work")
	mustGit(t, root, "init", "-q", "--bare", upstream)
	mustGit(t, root, "init", "-q", "--bare", gw)
	mustGit(t, root, "init", "-q", "-b", "main", work)
	commit := func(msg string) string {
		if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, work, "add", ".")
		mustGit(t, work, "commit", "-qm", msg)
		return strings.TrimSpace(mustGit(t, work, "rev-parse", "HEAD"))
	}
	base := commit("base")
	mustGit(t, work, "push", "-q", upstream, "main")
	mustGit(t, work, "push", "-q", gw, "main")
	theirs := commit("pushed straight to the upstream")
	mustGit(t, work, "push", "-q", upstream, "main")
	mustGit(t, work, "reset", "-q", "--hard", base)
	ours := commit("accepted by the gateway")
	mustGit(t, work, "push", "-q", gw, "main")
	refs := []RefUpdate{{Name: "refs/heads/main", OldRev: base, NewRev: ours}}

	for _, step := range []string{"fetch first", "non-fast-forward"} {
		if step == "non-fast-forward" {
			mustGit(t, work, "push", "-q", gw, theirs+":refs/heads/theirs")
		}
		err := Relay(upstream, "", gw, refs)
		if err == nil {
			t.Fatalf("%s: relay over a diverged upstream succeeded", step)
		}
		if !strings.Contains(err.Error(), "("+step+")") {
			t.Errorf("%s: git did not report %q:\n%v", step, step, err)
		}
		if branch, ok := RelayDiverged(err.Error()); !ok || branch != "main" {
			t.Errorf("%s: rejection not recognised (%q, %v):\n%v", step, branch, ok, err)
		}
	}
}
