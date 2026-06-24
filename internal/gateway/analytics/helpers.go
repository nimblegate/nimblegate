// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"crypto/sha256"
	"encoding/hex"

	"nimblegate/internal/gateway"
)

var sevRank = map[string]int{"INFO": 1, "WARN": 2, "ERROR": 3, "BLOCK": 4}

// maxSeverity returns the highest-ranked severity among findings
// (BLOCK > ERROR > WARN > INFO); "" when there are none. Unknown severities
// rank 0 and are ignored.
func maxSeverity(fs []gateway.Finding) string {
	best, bestRank := "", 0
	for _, f := range fs {
		if r := sevRank[f.Severity]; r > bestRank {
			best, bestRank = f.Severity, r
		}
	}
	return best
}

// dedupHash is the idempotency key for a decision: sha256 of the raw JSONL line.
func dedupHash(line []byte) string {
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:])
}

// fingerprint identifies a finding by content: sha256 of frame ID + its message
// (which carries the file:line locations). Identical standing findings re-pushed
// produce a byte-identical message → the same fingerprint → deduped; a changed
// location set yields a new fingerprint (counts as a new issue).
func fingerprint(frameID, message string) string {
	sum := sha256.Sum256([]byte(frameID + "\x00" + message))
	return hex.EncodeToString(sum[:])
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
