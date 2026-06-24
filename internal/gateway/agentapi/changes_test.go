// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway/gitlog"
)

// reposRootWith creates <root>/repos/<name>.git dirs (empty - the fake gitlog
// runner doesn't touch them) and returns the repos root.
func reposRootWith(t *testing.T, root string, names ...string) string {
	t.Helper()
	rr := filepath.Join(root, "repos")
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(rr, n+".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return rr
}

// A repo symlinked into ReposRoot (real bare repo under a subdir, exposed at
// top level via a <repo>.git symlink) must still be listed - ReadDir reports
// the link as a symlink, not a dir, so bareRepos resolves it with os.Stat.
func TestBareReposFollowsSymlinks(t *testing.T) {
	rr := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rr, "real.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(rr, "_repos", "linked.git")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(hidden, filepath.Join(rr, "linked.git")); err != nil {
		t.Fatal(err)
	}
	svc := &Service{ReposRoot: rr}
	got := svc.bareRepos()
	has := func(n string) bool {
		for _, g := range got {
			if g == n {
				return true
			}
		}
		return false
	}
	if !has("real") || !has("linked") {
		t.Fatalf("bareRepos() = %v, want both \"real\" and \"linked\" (symlinked repo must be listed)", got)
	}
}

// fakeLog swaps the package logFn (the gitlog.Log seam) for canned commits.
func fakeLog(t *testing.T, byRepo map[string][]gitlog.Commit) {
	t.Helper()
	old := logFn
	logFn = func(_ context.Context, gitDir string, _ gitlog.Options) ([]gitlog.Commit, error) {
		base := filepath.Base(gitDir)
		name := strings.TrimSuffix(base, ".git")
		return byRepo[name], nil
	}
	t.Cleanup(func() { logFn = old })
}

func writeAudit(t *testing.T, root, repo, line string) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWhatChangedTagsTipVerdict(t *testing.T) {
	// Own audit with a known tip SHA so we control the verdict join.
	root := t.TempDir()
	writeAudit(t, root, "demo", `{"time":"2026-05-26T00:00:00Z","repo":"demo","refs":["refs/heads/main"],"ref_updates":[{"Name":"refs/heads/main","OldRev":"old","NewRev":"tip111"}],"accept":true,"findings":[{"id":"app-correctness/shellcheck","severity":"WARN","message":"x"}]}`)
	rr := reposRootWith(t, root, "demo")
	fakeLog(t, map[string][]gitlog.Commit{
		"demo": {
			{SHA: "tip111", Date: "2026-06-11", Author: "alice", Subject: "feat: x", Files: []string{"a.sh"}},
			{SHA: "mid222", Date: "2026-06-10", Author: "alice", Subject: "wip", Files: []string{"b.sh"}},
		},
	})
	svc := &Service{PolicyRoot: root, ReposRoot: rr, Verify: func(string) (bool, error) { return true, nil }}
	out, err := svc.WhatChanged(Params{Repo: "demo", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "tip111") || !strings.Contains(out.Text, "feat: x") {
		t.Errorf("commit line missing: %s", out.Text)
	}
	// fields are separated for readability: sha · author · subject.
	if !strings.Contains(out.Text, "tip111 · alice · feat: x") {
		t.Errorf("sha/author/subject separators missing: %s", out.Text)
	}
	if !strings.Contains(out.Text, "accepted") || !strings.Contains(out.Text, "app-correctness/shellcheck (WARN)") {
		t.Errorf("tip verdict tag missing: %s", out.Text)
	}
	// mid-push commit: present, but no verdict tag.
	midLine := ""
	for _, ln := range strings.Split(out.Text, "\n") {
		if strings.Contains(ln, "wip") {
			midLine = ln
		}
	}
	if midLine == "" || strings.Contains(midLine, "accepted") {
		t.Errorf("mid-push commit should be untagged: %q", midLine)
	}
}

func TestWhatChangedNoReposRoot(t *testing.T) {
	svc := &Service{PolicyRoot: t.TempDir(), ReposRoot: "", Verify: func(string) (bool, error) { return true, nil }}
	out, err := svc.WhatChanged(Params{Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "repo browsing unavailable") {
		t.Errorf("expected no-repos-root note: %s", out.Text)
	}
}

func TestWhatChangedUnknownRepoRecovers(t *testing.T) {
	root := t.TempDir()
	rr := reposRootWith(t, root, "demo", "web")
	fakeLog(t, map[string][]gitlog.Commit{})
	svc := &Service{PolicyRoot: root, ReposRoot: rr, Verify: func(string) (bool, error) { return true, nil }}
	out, err := svc.WhatChanged(Params{Repo: "nope", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, `repo "nope" not found`) || !strings.Contains(out.Text, "demo") || !strings.Contains(out.Text, "web") {
		t.Errorf("recovery note should list bare-clone repos: %s", out.Text)
	}
}

func TestWhatChangedQueryAsRepoRecovery(t *testing.T) {
	root := t.TempDir()
	writeAudit(t, root, "appframes", `{"time":"2026-05-26T00:00:00Z","repo":"appframes","refs":["refs/heads/main"],"ref_updates":[{"Name":"refs/heads/main","OldRev":"old","NewRev":"af111"}],"accept":true}`)
	rr := reposRootWith(t, root, "appframes", "web")
	fakeLog(t, map[string][]gitlog.Commit{
		"appframes": {{SHA: "af111", Date: "2026-06-11", Author: "alice", Subject: "feat: y", Files: []string{"x.go"}}},
		"web":       {{SHA: "web999", Date: "2026-06-11", Author: "jill", Subject: "other", Files: []string{"y.go"}}},
	})
	svc := &Service{PolicyRoot: root, ReposRoot: rr, Verify: func(string) (bool, error) { return true, nil }}
	// Model misfire: repo name landed in query.
	out, err := svc.WhatChanged(Params{Query: "appframes", Days: 30})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "interpreted query \"appframes\" as the repository name") {
		t.Errorf("recovery note missing: %s", out.Text)
	}
	if !strings.Contains(out.Text, "feat: y") || strings.Contains(out.Text, "other") {
		t.Errorf("should scope to appframes only, not all repos: %s", out.Text)
	}
}

func TestWhatChangedTruncationNote(t *testing.T) {
	root := t.TempDir()
	writeAudit(t, root, "demo", `{"time":"2026-05-26T00:00:00Z","repo":"demo","refs":["refs/heads/main"],"ref_updates":[{"Name":"refs/heads/main","OldRev":"old","NewRev":"a"}],"accept":true}`)
	rr := reposRootWith(t, root, "demo")
	fakeLog(t, map[string][]gitlog.Commit{
		"demo": {
			{SHA: "a", Date: "2026-06-11", Author: "x", Subject: "c1"},
			{SHA: "b", Date: "2026-06-10", Author: "x", Subject: "c2"},
		},
	})
	svc := &Service{PolicyRoot: root, ReposRoot: rr, Verify: func(string) (bool, error) { return true, nil }}
	// Limit 2 with 2 commits → the repo hit the cap → per-repo truncation note.
	out, err := svc.WhatChanged(Params{Repo: "demo", Days: 30, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "commit cap") || !strings.Contains(out.Text, "demo") {
		t.Errorf("expected per-repo cap note when commits hit the limit: %s", out.Text)
	}
	// Limit 50 → both commits shown, nothing hidden → no note.
	out2, err := svc.WhatChanged(Params{Repo: "demo", Days: 30, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out2.Text, "commit cap") {
		t.Errorf("no cap note expected below the limit: %s", out2.Text)
	}
}
