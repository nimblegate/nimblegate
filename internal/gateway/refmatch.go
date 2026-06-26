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

// isDeleteProtected reports whether refName may NOT be deleted. This is separate
// from gating: ProtectedRefs decides what gets content-checked, delete-protection
// decides what can't be removed. The default branch (main/master) is ALWAYS
// protected; DeleteProtectedRefs adds more. So feature branches gated via
// refs/heads/* stay deletable, while main can't be dropped by accident.
func isDeleteProtected(p Policy, refName string) bool {
	pats := append([]string{"refs/heads/main", "refs/heads/master"}, p.DeleteProtectedRefs...)
	for _, pat := range pats {
		if ok, _ := path.Match(pat, refName); ok {
			return true
		}
	}
	return false
}
