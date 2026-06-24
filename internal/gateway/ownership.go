// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
)

// MatchParentOwnership recursively chowns every entry under tree to match
// parent's owner uid/gid. The intended use is right after creating files
// under <reposRoot> or <policyRoot>: pre-existing repos in those roots
// were created by the git user (the intended runtime identity), so new
// repos must inherit the same ownership or git-shell's safe.directory
// check will reject the next push as "dubious ownership."
//
// This addresses the common gateway-admin footgun: running `gateway add`
// via `ssh nbg-admin` (root) creates root-owned files, then when the
// git user runs git-receive-pack against them, git refuses to operate
// on a repo it doesn't own. The fix is to chown to the git user
// automatically right after creation.
//
// Behavior:
//
//   - Stat parent. If parent doesn't exist, return nil (nothing to match against;
//     first repo on a fresh box - the operator's running user wins by default).
//   - Stat tree. If tree doesn't exist, return an error (caller's bug).
//   - If parent's owner already matches the running process, return nil
//     (no work to do - files are already correctly owned).
//   - Otherwise chown every entry under tree (including tree itself) to
//     parent's uid/gid.
//   - On non-Unix platforms (Windows), this is a no-op: chown semantics
//     differ enough that we don't try to emulate.
func MatchParentOwnership(tree, parent string) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	parentInfo, err := os.Stat(parent)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat parent %s: %w", parent, err)
	}
	uid, gid, ok := unixOwnerOf(parentInfo)
	if !ok {
		return nil
	}

	if uid == os.Getuid() && gid == os.Getgid() {
		return nil
	}

	return filepath.Walk(tree, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// Skip symlinks - we don't want to follow them, and lchown would
		// touch the link itself which is rarely what we want.
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if err := os.Chown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
		return nil
	})
}

// unixOwnerOf extracts (uid, gid, ok) from an os.FileInfo. ok is false on
// platforms where Sys() doesn't return a *syscall.Stat_t (Windows; never
// hit because the caller short-circuits on runtime.GOOS == "windows").
func unixOwnerOf(info os.FileInfo) (uid, gid int, ok bool) {
	st, k := info.Sys().(*syscall.Stat_t)
	if !k {
		return 0, 0, false
	}
	return int(st.Uid), int(st.Gid), true
}
