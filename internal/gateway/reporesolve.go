// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveRepoBare maps a logical repo name to the canonical path of its bare
// repo, via the activation symlink (<reposRoot>/<name>.git -> _repos/<name>.git
// that `gateway add` creates). It succeeds only when the repo is ACTIVE (the
// activation symlink exists) and its resolved real path stays INSIDE reposRoot.
//
// Two properties matter for the relay using it:
//   - Symlink-aware: uses os.Stat / EvalSymlinks, never DirEntry.IsDir - the
//     latter is Lstat-based and reports false for the activation symlink, the
//     recurring trap that has already bitten the repo-scanning code.
//   - Root-confined: a swapped or hostile symlink that resolves outside
//     reposRoot is refused, so it cannot redirect the relay to push some other
//     location. Logical names that could traverse (slashes, "..", leading "." or
//     "_") are rejected before any filesystem access.
func resolveRepoBare(reposRoot, name string) (string, error) {
	if name == "" || name == "." || name == ".." ||
		strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("invalid repo name %q", name)
	}
	link := filepath.Join(reposRoot, name+".git")
	fi, err := os.Stat(link) // follows the activation symlink to the real dir
	if err != nil {
		return "", fmt.Errorf("repo %q not active: %w", name, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("repo %q is not a directory", name)
	}
	canon, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", fmt.Errorf("repo %q resolve: %w", name, err)
	}
	canonRoot, err := filepath.EvalSymlinks(reposRoot)
	if err != nil {
		return "", fmt.Errorf("repos root resolve: %w", err)
	}
	rel, err := filepath.Rel(canonRoot, canon)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("repo %q resolves outside repos root", name)
	}
	return canon, nil
}
