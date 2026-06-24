// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "path"

// isGatedRef reports whether refName matches any ProtectedRefs glob.
// Patterns use path.Match semantics over the full ref name; "release/*"
// must be written as a full ref "refs/heads/release/*".
func isGatedRef(p Policy, refName string) bool {
	if p.GateAllRefs {
		return true // gate every ref, not just protected ones
	}
	for _, pat := range p.ProtectedRefs {
		if ok, _ := path.Match(pat, refName); ok {
			return true
		}
	}
	return false
}
