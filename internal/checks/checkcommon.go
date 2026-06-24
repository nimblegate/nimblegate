// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package checks holds the Go implementations of every stdlib frame's
// check function. The package's helpers below are the canonical patterns
// every check should reuse - bypassing them is a regression that
// TestNoUnboundedReadInChecks catches at build time.
package checks

import "os"

// DefaultMaxFileBytes is the size cap every content-scanning frame should
// use unless it has a domain-specific reason to allow larger. 1 MiB is
// generous for source files (a 1 MiB JS bundle is already huge) and small
// enough that 100 concurrent oversized pushes still fit in modest gateway
// RAM - preventing the OOM-DoS vector where a malicious push includes one
// multi-GB file specifically to crash the pre-receive evaluator.
const DefaultMaxFileBytes = 1 << 20

// ReadFileBounded is the canonical pattern for frames that scan file
// content. It reads path into memory only if the file is at most maxBytes;
// larger files (and missing/unreadable files) return (nil, false) and the
// caller should skip them.
//
// Why this exists: Go's regexp package is RE2 (linear-time, no
// catastrophic backtracking), so ReDoS-by-pattern is impossible. But
// RE2's O(n) is in INPUT BYTES - n is unbounded if the frame slurps the
// whole file via raw os.ReadFile. A malicious agent pushing a 3 GB
// markdown file would otherwise exhaust the gateway's RAM. Using this
// helper makes that vector structurally impossible.
//
// If you find yourself wanting maxBytes > DefaultMaxFileBytes, state the
// reason in the calling frame's comment and reviewer will check whether
// the larger cap is justified. Don't reach for raw os.ReadFile - that's
// exactly what TestNoUnboundedReadInChecks blocks.
func ReadFileBounded(path string, maxBytes int64) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxBytes {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}
