// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"regexp"
	"strings"
)

// git marks a ref the upstream refused because it holds commits the pushing
// side does not: "non-fast-forward" when the pusher has those commits but its
// branch does not contain them, "fetch first" when it has never seen them.
var rejectedDiverged = regexp.MustCompile(`\[rejected\]\s+\S+\s+->\s+(\S+)\s+\((?:non-fast-forward|fetch first)\)`)

// RelayDiverged reports whether a recorded relay error is the upstream
// refusing a push because its history has moved on, and names the branch.
// That is divergence, not a credential or network problem.
func RelayDiverged(errText string) (branch string, ok bool) {
	m := rejectedDiverged.FindStringSubmatch(errText)
	if m == nil {
		return "", false
	}
	return strings.TrimPrefix(m[1], "refs/heads/"), true
}

// WithoutGitHints drops git's "hint:" lines from a relay error. They advise a
// working copy ("use git pull"), which is wrong on a gateway.
func WithoutGitHints(errText string) string {
	var keep []string
	for _, l := range strings.Split(errText, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "hint:") {
			keep = append(keep, l)
		}
	}
	return strings.TrimRight(strings.Join(keep, "\n"), "\n")
}
