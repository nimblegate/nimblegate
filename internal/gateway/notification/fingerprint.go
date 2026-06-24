// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Fingerprint returns a stable identifier for a (frame, file, line) triple.
// Powers the same-finding-twice rotation trigger: when the gateway sees the
// same fingerprint in consecutive rejected pushes on a PR, the agent has not
// actually fixed the issue → rotate immediately.
//
// Format: "sha256:" + first 16 hex chars of SHA-256. Truncated for readability
// in the JSON payload; collision risk at this length is negligible for
// per-PR-per-attempt comparison.
func Fingerprint(frameID, file string, line int) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", frameID, file, line)))
	return "sha256:" + hex.EncodeToString(h[:])[:16]
}
