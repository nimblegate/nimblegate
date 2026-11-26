// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

//go:build unix

package notification

import (
	"errors"
	"os"
	"syscall"
)

// LockFile takes an exclusive advisory lock guarding path, held on a sibling
// "<path>.lock" file so the lock survives the temp+rename rewrites that replace
// path itself. The pre-receive hook and the dashboard daemon are separate
// processes writing the same queue files; without this a rewrite can drop a
// record appended between its read and its rename. Call the returned func to
// release.
func LockFile(path string) (func(), error) {
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	return func() { _ = f.Close() }, nil
}
