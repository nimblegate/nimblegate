// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"path/filepath"
	"strings"
	"testing"
)

// AuthorizeShellRequest composes parse + symlink-safe resolve + per-key ACL.
func TestAuthorizeShellRequest(t *testing.T) {
	reposRoot := t.TempDir()
	policyRoot := t.TempDir()
	activateRepo(t, reposRoot, "demo")
	acl := AccessStore{PolicyRoot: policyRoot}
	const fp = "SHA256:keyA"
	if err := acl.Grant("demo", fp, "write", "alice"); err != nil {
		t.Fatal(err)
	}

	// granted write key → push allowed, returns sub-verb + resolved bare dir
	sub, bare, err := AuthorizeShellRequest("git-receive-pack 'demo.git'", fp, reposRoot, policyRoot, true)
	if err != nil {
		t.Fatalf("push by granted key: %v", err)
	}
	if sub != "receive-pack" {
		t.Errorf("subverb = %q, want receive-pack", sub)
	}
	wantBare, _ := filepath.EvalSymlinks(filepath.Join(reposRoot, "_repos", "demo.git"))
	if bare != wantBare {
		t.Errorf("bare = %q, want %q", bare, wantBare)
	}

	// granted key may also fetch
	if _, _, err := AuthorizeShellRequest("git-upload-pack 'demo.git'", fp, reposRoot, policyRoot, true); err != nil {
		t.Errorf("fetch by granted key: %v", err)
	}
	// ungranted key denied
	if _, _, err := AuthorizeShellRequest("git-upload-pack 'demo.git'", "SHA256:stranger", reposRoot, policyRoot, true); err == nil {
		t.Error("ungranted key must be denied")
	}
	// read-only key cannot push
	if err := acl.Grant("demo", "SHA256:ro", "read", "bob"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AuthorizeShellRequest("git-receive-pack 'demo.git'", "SHA256:ro", reposRoot, policyRoot, true); err == nil {
		t.Error("read-only key must not push")
	}
	// repo that isn't active → denied (resolve fails)
	if _, _, err := AuthorizeShellRequest("git-upload-pack 'ghost.git'", fp, reposRoot, policyRoot, true); err == nil {
		t.Error("non-active repo must be denied")
	}
	// non-git verb → denied
	if _, _, err := AuthorizeShellRequest("rm -rf / 'x'", fp, reposRoot, policyRoot, true); err == nil {
		t.Error("non-git verb must be denied")
	}
	// granted on demo but requests another repo → denied
	activateRepo(t, reposRoot, "other")
	if _, _, err := AuthorizeShellRequest("git-receive-pack 'other.git'", fp, reposRoot, policyRoot, true); err == nil {
		t.Error("key granted on demo must not reach 'other'")
	}
}

// Unscoped (enforceACL=false): the forced command still routes (parse + resolve)
// so the clean ssh:// URL works, but ANY authorized key reaches the repo with no
// grant - the single-tenant default. Only parse/resolve guards apply.
func TestAuthorizeShellRequest_unscopedAllowsAnyKey(t *testing.T) {
	reposRoot := t.TempDir()
	policyRoot := t.TempDir()
	activateRepo(t, reposRoot, "demo")

	// no grants exist at all; an arbitrary key may still push (route + allow)
	sub, _, err := AuthorizeShellRequest("git-receive-pack 'demo.git'", "SHA256:anyone", reposRoot, policyRoot, false)
	if err != nil {
		t.Fatalf("unscoped: any authorized key should be allowed: %v", err)
	}
	if sub != "receive-pack" {
		t.Errorf("subverb = %q, want receive-pack", sub)
	}
	// parse/resolve guards still apply even unscoped
	if _, _, err := AuthorizeShellRequest("git-upload-pack 'ghost.git'", "SHA256:anyone", reposRoot, policyRoot, false); err == nil {
		t.Error("unscoped must still reject a non-active repo")
	}
	if _, _, err := AuthorizeShellRequest("rm -rf / 'x'", "SHA256:anyone", reposRoot, policyRoot, false); err == nil {
		t.Error("unscoped must still reject a non-git verb")
	}
}

func TestParseGitShellCommand(t *testing.T) {
	cases := []struct {
		in       string
		wantVerb string
		wantRepo string
		wantErr  bool
	}{
		{"git-upload-pack 'myrepo.git'", "git-upload-pack", "myrepo", false},
		{"git-receive-pack 'myrepo.git'", "git-receive-pack", "myrepo", false},
		{"git-upload-archive 'myrepo.git'", "git-upload-archive", "myrepo", false},
		{"git upload-pack 'myrepo.git'", "git-upload-pack", "myrepo", false},                    // space form
		{"git-upload-pack '/srv/gateway/repos/myrepo.git'", "git-upload-pack", "myrepo", false}, // full path
		{"git-upload-pack 'myrepo'", "git-upload-pack", "myrepo", false},                        // no .git suffix
		{"git-receive-pack '../../../etc/passwd'", "git-receive-pack", "passwd", false},         // traversal → basename (denied later by resolve/ACL)
		{"", "", "", true},                     // interactive login - not allowed
		{"bash", "", "", true},                 // not a git command
		{"rm -rf / 'x'", "", "", true},         // not a git verb
		{"scp -t /tmp", "", "", true},          // not git
		{"git-upload-pack", "", "", true},      // missing repo path
		{"git-upload-pack '..'", "", "", true}, // resolves to ".."
		{"git-upload-pack ''", "", "", true},   // empty path
	}
	for _, c := range cases {
		v, r, err := parseGitShellCommand(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("%q: expected error, got verb=%q repo=%q", c.in, v, r)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.in, err)
			continue
		}
		if v != c.wantVerb || r != c.wantRepo {
			t.Errorf("%q → (%q,%q), want (%q,%q)", c.in, v, r, c.wantVerb, c.wantRepo)
		}
	}
}

// The returned repo name is always a single clean component - traversal or
// directory components in the requested path can never produce slashes or ".."
// in the name (so it can't escape the repos root downstream).
func TestParseGitShellCommand_nameNeverEscapes(t *testing.T) {
	for _, in := range []string{
		"git-upload-pack '../../etc/x.git'",
		"git-upload-pack '/a/b/c/deep.git'",
		"git-receive-pack 'sub/dir/r.git'",
	} {
		_, repo, err := parseGitShellCommand(in)
		if err != nil {
			continue
		}
		if strings.ContainsAny(repo, `/\`) || repo == ".." || repo == "." {
			t.Errorf("%q produced unsafe repo name %q", in, repo)
		}
	}
}
