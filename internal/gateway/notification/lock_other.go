// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

//go:build !unix

package notification

// LockFile is a no-op where flock is unavailable; the gateway runs on unix.
func LockFile(string) (func(), error) { return func() {}, nil }
