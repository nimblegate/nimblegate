// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"path/filepath"
	"strings"
)

// allowedGitVerbs are the only commands the forced-command shell will run - the
// same git transfer verbs git-shell itself permits. Everything else is refused.
var allowedGitVerbs = map[string]bool{
	"git-upload-pack":    true, // fetch / clone (read)
	"git-receive-pack":   true, // push (write)
	"git-upload-archive": true, // archive (read)
}

// gitVerbIsWrite reports whether a parsed verb mutates the repo (push).
func gitVerbIsWrite(verb string) bool { return verb == "git-receive-pack" }

// parseGitShellCommand parses the $SSH_ORIGINAL_COMMAND git sends over SSH
// (e.g. `git-upload-pack 'myrepo.git'`, or the space form `git upload-pack …`)
// into the git verb and the LOGICAL repo name. It refuses any non-git verb and
// any malformed/empty path.
//
// The returned repo is a single path component with `.git` stripped: directory
// and traversal components are dropped by filepath.Base, so the name can never
// contain a slash or `..` and cannot escape the repos root. resolveRepoBare
// still validates downstream that it's an active repo under the root.
func parseGitShellCommand(orig string) (verb, repo string, err error) {
	orig = strings.TrimSpace(orig)
	if orig == "" {
		return "", "", fmt.Errorf("no command: interactive login is not allowed")
	}
	verb, arg, ok := splitVerbArg(orig)
	if !ok {
		return "", "", fmt.Errorf("unrecognized command %q", orig)
	}
	if !allowedGitVerbs[verb] {
		return "", "", fmt.Errorf("command not permitted: %q", verb)
	}
	path := unquoteRepoArg(arg)
	if path == "" {
		return "", "", fmt.Errorf("missing repository path")
	}
	name := strings.TrimSuffix(filepath.Base(path), ".git")
	if name == "" || name == "." || name == ".." {
		return "", "", fmt.Errorf("invalid repository path %q", path)
	}
	return verb, name, nil
}

// AuthorizeShellRequest is the security core of the forced-command shell. Given
// the raw $SSH_ORIGINAL_COMMAND, the connecting key's fingerprint, and the
// roots, it decides whether the key may run the requested git command - and if
// so returns the git sub-verb ("upload-pack"/"receive-pack"/"upload-archive")
// and the resolved bare-repo dir to exec against. It composes the guards:
// (1) parse + verb whitelist, (2) symlink-safe root-confined repo resolution,
// and - only when enforceACL is set (scoped mode) - (3) the per-key ACL (write
// ops require a write grant). Any failure → error, and the caller must NOT exec
// git.
//
// enforceACL is the single-tenant/multi-tenant switch. The forced command is
// now written for EVERY key (so the clean `ssh://host/repo.git` URL routes
// through here - bare git-shell can't resolve its absolute path). In the
// single-tenant default (enforceACL=false) any authorized key reaches any repo,
// exactly as before scoped access existed; scoped mode (enforceACL=true) keeps
// the deny-by-default per-key ACL unchanged.
func AuthorizeShellRequest(origCommand, fingerprint, reposRoot, policyRoot string, enforceACL bool) (subverb, bareDir string, err error) {
	verb, repo, err := parseGitShellCommand(origCommand)
	if err != nil {
		return "", "", err
	}
	bareDir, err = resolveRepoBare(reposRoot, repo)
	if err != nil {
		return "", "", fmt.Errorf("repo %q: %w", repo, err)
	}
	if enforceACL {
		ok, err := (AccessStore{PolicyRoot: policyRoot}).Allows(repo, fingerprint, gitVerbIsWrite(verb))
		if err != nil {
			return "", "", err
		}
		if !ok {
			return "", "", fmt.Errorf("access denied: key %s is not authorized to %s %q", fingerprint, verb, repo)
		}
	}
	return strings.TrimPrefix(verb, "git-"), bareDir, nil
}

// splitVerbArg splits the original command into a hyphenated git verb and its
// argument, accepting both `git-upload-pack '<path>'` and `git upload-pack
// '<path>'`.
func splitVerbArg(orig string) (verb, arg string, ok bool) {
	head, rest, found := strings.Cut(orig, " ")
	if !found {
		return "", "", false // a bare verb with no path is not a valid git request
	}
	if head == "git" {
		sub, subArg, found := strings.Cut(rest, " ")
		if !found {
			return "", "", false
		}
		return "git-" + sub, subArg, true
	}
	return head, rest, true
}

// unquoteRepoArg strips a single surrounding pair of single or double quotes
// (git single-quotes the repo path) and surrounding whitespace.
func unquoteRepoArg(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	return strings.TrimSpace(s)
}
