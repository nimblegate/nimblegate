// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// bareWithLinks builds a bare repo whose main branch holds real.txt plus the
// given symlinks (name -> target), and returns it with the commit SHA.
func bareWithLinks(t *testing.T, links map[string]string) (string, string) {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) string {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "real.txt"), []byte("in tree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, target := range links {
		p := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, p); err != nil {
			t.Fatal(err)
		}
	}
	run(work, "add", "-A")
	run(work, "commit", "-qm", "links")
	bare := filepath.Join(t.TempDir(), "r.git")
	run(work, "clone", "-q", "--bare", work, bare)
	return bare, run(work, "rev-parse", "HEAD")
}

func TestMaterializeTree_outsideLinksSidecar(t *testing.T) {
	bare, sha := bareWithLinks(t, map[string]string{
		"docs/leak":   "/etc/passwd",
		"up":          "../../../../x",
		"inside-link": "real.txt",
		"dangling":    "missing.txt",
	})
	parent := t.TempDir()
	dest := filepath.Join(parent, "scan")
	if err := os.Mkdir(dest, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := materializeTree(bare, sha, dest, 0); err != nil {
		t.Fatal(err)
	}
	side := OutsideLinksPath(dest)
	if filepath.Dir(side) != parent {
		t.Fatalf("sidecar %s must sit next to the tree, outside it", side)
	}
	fi, err := os.Stat(side)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("sidecar mode = %v, want 0600", fi.Mode().Perm())
	}
	links, err := LoadOutsideLinks(dest)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, l := range links {
		got[l.Path] = l.Target
	}
	if len(got) != 2 || got["docs/leak"] != "/etc/passwd" || got["up"] != "../../../../x" {
		t.Errorf("sidecar = %v", got)
	}

	removeStagedTree(dest)
	if left, _ := os.ReadDir(parent); len(left) != 0 {
		t.Errorf("left behind after removal: %v", left)
	}
}

func TestMaterializeTree_cleanTreeWritesEmptyList(t *testing.T) {
	bare, sha := bareWithLinks(t, map[string]string{"inside-link": "real.txt"})
	dest := t.TempDir()
	if err := materializeTree(bare, sha, dest, 0); err != nil {
		t.Fatal(err)
	}
	links, err := LoadOutsideLinks(dest)
	if err != nil || links == nil || len(links) != 0 {
		t.Errorf("clean staged tree: links=%v err=%v, want an empty non-nil list", links, err)
	}
	if links, err := LoadOutsideLinks(t.TempDir()); err != nil || links != nil {
		t.Errorf("unstaged dir: links=%v err=%v, want nil", links, err)
	}
}

// sidecarChecker records what the staged sidecar said when the check ran.
type sidecarChecker struct{ seen *[][]engine.OutsideLink }

func (c sidecarChecker) Check(root string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	links, err := LoadOutsideLinks(root)
	*c.seen = append(*c.seen, links)
	return nil, nil, err
}

func TestStagedTreeAndSidecarRemoved_hookAndPreview(t *testing.T) {
	bare, sha := bareWithLinks(t, map[string]string{"leak": "/etc/passwd"})

	var seen [][]engine.OutsideLink
	scanTmp := t.TempDir()
	deps := PreReceiveDeps{
		Policy:     Policy{Repo: "demo", Enabled: true, ProtectedRefs: []string{"refs/heads/main"}, PolicyDir: t.TempDir()},
		GitDir:     bare,
		Checker:    sidecarChecker{seen: &seen},
		AuditPath:  filepath.Join(t.TempDir(), "a.log"),
		ScanTmpDir: scanTmp,
	}
	var out bytes.Buffer
	if code := RunPreReceive(deps, strings.NewReader(zeroRev+" "+sha+" refs/heads/main\n"), &out); code != 0 {
		t.Fatalf("clean checker must accept: %d\n%s", code, out.String())
	}
	if left, _ := os.ReadDir(scanTmp); len(left) != 0 {
		t.Errorf("hook left staging files behind: %v", left)
	}

	previewTmp := t.TempDir()
	PreviewPolicy(PreviewInput{
		Policy:     deps.Policy,
		GitDir:     bare,
		Checker:    sidecarChecker{seen: &seen},
		ScanTmpDir: previewTmp,
	})
	if left, _ := os.ReadDir(previewTmp); len(left) != 0 {
		t.Errorf("preview left staging files behind: %v", left)
	}

	if len(seen) != 2 {
		t.Fatalf("checker ran %d times, want 2", len(seen))
	}
	for i, links := range seen {
		if len(links) != 1 || links[0].Path != "leak" || links[0].Target != "/etc/passwd" {
			t.Errorf("run %d saw sidecar %v", i, links)
		}
	}
}
