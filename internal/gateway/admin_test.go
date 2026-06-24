// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// With RelaySocket set, the generated post-receive hook must export
// NBG_RELAY_SOCKET (so the relay routes through the privilege-separated
// service); pre-receive must not carry it.
func TestAddRepo_relaySocketBakedIntoPostReceiveOnly(t *testing.T) {
	policyRoot := t.TempDir()
	reposRoot := t.TempDir()
	if err := AddRepo(AddOptions{
		Name:          "demo",
		UpstreamURL:   "file:///tmp/up.git",
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		PolicyRoot:    policyRoot,
		ReposRoot:     reposRoot,
		SelfExe:       "/usr/local/bin/nimblegate",
		RelaySocket:   "/run/nbg/relay.sock",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	hooks := filepath.Join(reposRoot, "demo.git", "hooks")
	post, _ := os.ReadFile(filepath.Join(hooks, "post-receive"))
	if !strings.Contains(string(post), `export NBG_RELAY_SOCKET="/run/nbg/relay.sock"`) {
		t.Errorf("post-receive must export the relay socket:\n%s", post)
	}
	pre, _ := os.ReadFile(filepath.Join(hooks, "pre-receive"))
	if strings.Contains(string(pre), "NBG_RELAY_SOCKET") {
		t.Errorf("pre-receive must NOT carry the relay socket:\n%s", pre)
	}
}

func TestAddRepo(t *testing.T) {
	gwRoot := t.TempDir()
	reposRoot := t.TempDir()
	opts := AddOptions{
		Name:          "demo",
		UpstreamURL:   "file:///tmp/whatever.git",
		ProtectedRefs: []string{"refs/heads/main"},
		Enabled:       true,
		PolicyRoot:    gwRoot,
		ReposRoot:     reposRoot,
		SelfExe:       "/usr/local/bin/nimblegate",
	}
	if err := AddRepo(opts); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	bare := filepath.Join(reposRoot, "demo.git")
	for _, h := range []string{"pre-receive", "post-receive"} {
		hp := filepath.Join(bare, "hooks", h)
		info, err := os.Stat(hp)
		if err != nil {
			t.Fatalf("missing hook %s: %v", h, err)
		}
		if info.Mode().Perm()&0o111 == 0 {
			t.Errorf("hook %s not executable", h)
		}
		b, _ := os.ReadFile(hp)
		if !strings.Contains(string(b), "nimblegate") || !strings.Contains(string(b), "demo") {
			t.Errorf("hook %s should invoke nimblegate for repo demo:\n%s", h, b)
		}
	}
	if p, err := (FilePolicyStore{Root: gwRoot}).Load("demo"); err != nil || p.UpstreamURL == "" {
		t.Errorf("policy not saved: %+v %v", p, err)
	}
}

func TestMigrateToSymlinkLayout_movesLegacyDirsAndCreatesSymlinks(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(filepath.Join(policyRoot, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "foo", "gateway.toml"),
		[]byte("repo=\"foo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(reposRoot, "foo.git"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := MigrateToSymlinkLayout(MigrateOptions{PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo", "gateway.toml")); err != nil {
		t.Fatalf("lib policy missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git")); err != nil {
		t.Fatalf("lib bare missing: %v", err)
	}
	if fi, err := os.Lstat(filepath.Join(policyRoot, "foo")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("policy symlink missing: fi=%v err=%v", fi, err)
	}
	if fi, err := os.Lstat(filepath.Join(reposRoot, "foo.git")); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("bare symlink missing: fi=%v err=%v", fi, err)
	}
	target, _ := os.Readlink(filepath.Join(policyRoot, "foo"))
	if target != filepath.Join("_repos", "foo") {
		t.Fatalf("symlink target: got %q want %q", target, filepath.Join("_repos", "foo"))
	}
	target, _ = os.Readlink(filepath.Join(reposRoot, "foo.git"))
	if target != filepath.Join("_repos", "foo.git") {
		t.Fatalf("bare symlink target: got %q want %q", target, filepath.Join("_repos", "foo.git"))
	}
}

func TestMigrateToSymlinkLayout_idempotent(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(filepath.Join(policyRoot, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "foo", "gateway.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(reposRoot, "foo.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := MigrateOptions{PolicyRoot: policyRoot, ReposRoot: reposRoot}
	if err := MigrateToSymlinkLayout(opts); err != nil {
		t.Fatal(err)
	}
	if err := MigrateToSymlinkLayout(opts); err != nil {
		t.Fatalf("second run: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "gateway.toml"))
	if len(matches) != 1 {
		t.Fatalf("after 2x migrate: want 1 match got %v", matches)
	}
}

func TestAddRepo_writesLibAndSymlinks(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name:          "foo",
		UpstreamURL:   "http://example.test/foo.git",
		ProtectedRefs: []string{"main"},
		Enabled:       true,
		PolicyRoot:    policyRoot,
		ReposRoot:     reposRoot,
		SelfExe:       "/bin/true",
	}); err != nil {
		t.Fatalf("AddRepo: %v", err)
	}
	// Lib dirs are real (not symlinks).
	for _, p := range []string{
		filepath.Join(policyRoot, "_repos", "foo"),
		filepath.Join(reposRoot, "_repos", "foo.git"),
	} {
		fi, err := os.Lstat(p)
		if err != nil {
			t.Fatalf("lib missing: %s: %v", p, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
			t.Fatalf("lib should be real dir: %s mode=%v", p, fi.Mode())
		}
	}
	// gateway.toml present inside the lib.
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo", "gateway.toml")); err != nil {
		t.Fatalf("gateway.toml missing: %v", err)
	}
	// Bare repo initialized (HEAD reachable via the lib path).
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git", "HEAD")); err != nil {
		t.Fatalf("HEAD missing: %v", err)
	}
	// Hooks installed.
	for _, h := range []string{"pre-receive", "post-receive"} {
		if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git", "hooks", h)); err != nil {
			t.Fatalf("hook %s missing: %v", h, err)
		}
	}
	// Activation symlinks present with the expected relative target.
	for _, c := range []struct{ link, want string }{
		{filepath.Join(policyRoot, "foo"), filepath.Join("_repos", "foo")},
		{filepath.Join(reposRoot, "foo.git"), filepath.Join("_repos", "foo.git")},
	} {
		fi, err := os.Lstat(c.link)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("not a symlink: %s err=%v", c.link, err)
		}
		got, _ := os.Readlink(c.link)
		if got != c.want {
			t.Fatalf("symlink target: got %q want %q", got, c.want)
		}
	}
	// Policy round-trips through FilePolicyStore.Load (via the symlink).
	pol, err := (FilePolicyStore{Root: policyRoot}).Load("foo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pol.Repo != "foo" || pol.UpstreamURL != "http://example.test/foo.git" || !pol.Enabled {
		t.Fatalf("policy roundtrip wrong: %+v", pol)
	}
}

func TestAddRepo_rejectsReservedName(t *testing.T) {
	tmp := t.TempDir()
	err := AddRepo(AddOptions{
		Name:        "_repos",
		UpstreamURL: "http://x",
		PolicyRoot:  filepath.Join(tmp, "p"),
		ReposRoot:   filepath.Join(tmp, "r"),
		SelfExe:     "/bin/true",
	})
	if err == nil {
		t.Fatal("want error for reserved name _repos")
	}
}

func TestAddRepo_rejectsExistingSymlink(t *testing.T) {
	tmp := t.TempDir()
	opts := AddOptions{
		Name:        "foo",
		UpstreamURL: "http://x",
		Enabled:     true,
		PolicyRoot:  filepath.Join(tmp, "p"),
		ReposRoot:   filepath.Join(tmp, "r"),
		SelfExe:     "/bin/true",
	}
	if err := AddRepo(opts); err != nil {
		t.Fatalf("first AddRepo: %v", err)
	}
	if err := AddRepo(opts); err == nil {
		t.Fatal("second AddRepo: want collision error")
	}
}

func TestAddRepo_rejectsArchivePrefix(t *testing.T) {
	tmp := t.TempDir()
	err := AddRepo(AddOptions{
		Name:        "_archive_x",
		UpstreamURL: "http://x",
		PolicyRoot:  filepath.Join(tmp, "p"),
		ReposRoot:   filepath.Join(tmp, "r"),
		SelfExe:     "/bin/true",
	})
	if err == nil {
		t.Fatal("want error for _archive prefix")
	}
}

func TestMigrateToSymlinkLayout_skipsInternalEntries(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "policy")
	reposRoot := filepath.Join(tmp, "repos")
	// Pre-existing _repos/ and _archived.md / _events.jsonl should be left alone
	// (not migrated as if they were repo entries).
	if err := os.MkdirAll(filepath.Join(policyRoot, "_repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "_archived.md"), []byte("# log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(policyRoot, "_events.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(reposRoot, "_repos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := MigrateToSymlinkLayout(MigrateOptions{PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	// _archived.md still a regular file, not moved.
	if fi, err := os.Lstat(filepath.Join(policyRoot, "_archived.md")); err != nil || fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("_archived.md disturbed: fi=%v err=%v", fi, err)
	}
	// _events.jsonl still a regular file.
	if fi, err := os.Lstat(filepath.Join(policyRoot, "_events.jsonl")); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("_events.jsonl disturbed: fi=%v err=%v", fi, err)
	}
}

func TestArchiveRepo_removesSymlinksKeepsLib(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveRepo(ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(policyRoot, "foo")); err == nil {
		t.Fatal("policy symlink should be gone")
	}
	if _, err := os.Lstat(filepath.Join(reposRoot, "foo.git")); err == nil {
		t.Fatal("bare symlink should be gone")
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo", "gateway.toml")); err != nil {
		t.Fatalf("lib should remain: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git", "HEAD")); err != nil {
		t.Fatalf("lib bare should remain: %v", err)
	}
}

func TestArchiveRepo_rejectsRealDir(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	// Pre-migration shape: real dirs, not symlinks.
	if err := os.MkdirAll(filepath.Join(policyRoot, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(reposRoot, "foo.git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveRepo(ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err == nil {
		t.Fatal("want error on un-migrated layout")
	}
	// Real dirs still there.
	if _, err := os.Stat(filepath.Join(policyRoot, "foo")); err != nil {
		t.Fatalf("policy dir destroyed: %v", err)
	}
}

func TestArchiveRepo_rejectsMissing(t *testing.T) {
	tmp := t.TempDir()
	err := ArchiveRepo(ArchiveOptions{
		Name: "nope", PolicyRoot: filepath.Join(tmp, "p"), ReposRoot: filepath.Join(tmp, "r"),
	})
	if err == nil {
		t.Fatal("want error for missing repo")
	}
}

func TestRestoreRepo_recreatesSymlinks(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveRepo(ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreRepo(RestoreOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	for _, link := range []string{
		filepath.Join(policyRoot, "foo"),
		filepath.Join(reposRoot, "foo.git"),
	} {
		fi, err := os.Lstat(link)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("not a symlink: %s err=%v", link, err)
		}
	}
}

func TestRestoreRepo_rejectsActiveSymlink(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := RestoreRepo(RestoreOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err == nil {
		t.Fatal("want error: already active")
	}
}

func TestRestoreRepo_rejectsMissingLib(t *testing.T) {
	tmp := t.TempDir()
	err := RestoreRepo(RestoreOptions{
		Name: "ghost", PolicyRoot: filepath.Join(tmp, "p"), ReposRoot: filepath.Join(tmp, "r"),
	})
	if err == nil {
		t.Fatal("want error: no lib entry")
	}
}

func TestListArchivedRepos_returnsLibEntriesWithoutSymlinks(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "active", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := AddRepo(AddOptions{
		Name: "archived", UpstreamURL: "http://y", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveRepo(ArchiveOptions{Name: "archived", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	got := ListArchivedRepos(policyRoot)
	if len(got) != 1 || got[0] != "archived" {
		t.Fatalf("got %v want [archived]", got)
	}
}

func TestRegenerateArchivedMarkdown_filtersArchiveAndRestore(t *testing.T) {
	root := t.TempDir()
	_ = AppendEvent(root, Event{Event: "add", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "archive", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "scan-apply", Repo: "a", OK: true})
	_ = AppendEvent(root, Event{Event: "restore", Repo: "a", OK: true})
	if err := RegenerateArchivedMarkdown(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "_archived.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "archive") || !strings.Contains(s, "restore") {
		t.Fatalf("md missing archive/restore rows: %s", s)
	}
	// Non-archive events excluded.
	if strings.Contains(s, "scan-apply") {
		t.Fatalf("md should not include scan-apply: %s", s)
	}
}

func TestRegenerateArchivedMarkdown_noEventsCreatesHeaderOnly(t *testing.T) {
	root := t.TempDir()
	if err := RegenerateArchivedMarkdown(root); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "_archived.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "Gateway repo lifecycle log") {
		t.Fatalf("md missing header: %s", s)
	}
}

func TestDeleteRepo_removesSymlinksAndLib(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRepo(DeleteOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	// Both activation symlinks AND both real lib dirs must be gone.
	for _, p := range []string{
		filepath.Join(policyRoot, "foo"),
		filepath.Join(reposRoot, "foo.git"),
		filepath.Join(policyRoot, "_repos", "foo"),
		filepath.Join(reposRoot, "_repos", "foo.git"),
	} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("should be fully removed, still present: %s (err=%v)", p, err)
		}
	}
}

func TestDeleteRepo_worksOnArchivedRepo(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	// Archive first (removes symlinks, keeps lib) - delete must still clear the lib.
	if err := ArchiveRepo(ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteRepo(DeleteOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", "foo")); !os.IsNotExist(err) {
		t.Error("archived lib policy dir should be gone after delete")
	}
	if _, err := os.Stat(filepath.Join(reposRoot, "_repos", "foo.git")); !os.IsNotExist(err) {
		t.Error("archived lib bare repo should be gone after delete")
	}
}

func TestIsArchivedRepo(t *testing.T) {
	tmp := t.TempDir()
	policyRoot := filepath.Join(tmp, "p")
	reposRoot := filepath.Join(tmp, "r")
	if err := AddRepo(AddOptions{
		Name: "foo", UpstreamURL: "http://x", Enabled: true,
		PolicyRoot: policyRoot, ReposRoot: reposRoot, SelfExe: "/bin/true",
	}); err != nil {
		t.Fatal(err)
	}
	if IsArchivedRepo(policyRoot, "foo") {
		t.Error("active repo should not report archived")
	}
	if IsArchivedRepo(policyRoot, "nope") {
		t.Error("absent repo should not report archived")
	}
	if err := ArchiveRepo(ArchiveOptions{Name: "foo", PolicyRoot: policyRoot, ReposRoot: reposRoot}); err != nil {
		t.Fatal(err)
	}
	if !IsArchivedRepo(policyRoot, "foo") {
		t.Error("archived repo should report archived")
	}
}

func TestDeleteRepo_refusesReservedName(t *testing.T) {
	if err := DeleteRepo(DeleteOptions{Name: "_repos", PolicyRoot: t.TempDir(), ReposRoot: t.TempDir()}); err == nil {
		t.Error("must refuse the reserved name _repos")
	}
}

func TestDeleteRepo_errorsOnMissingRepo(t *testing.T) {
	if err := DeleteRepo(DeleteOptions{Name: "nope", PolicyRoot: t.TempDir(), ReposRoot: t.TempDir()}); err == nil {
		t.Error("must error when the repo doesn't exist (guards typos)")
	}
}
