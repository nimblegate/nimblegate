// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"path"
	"strings"
)

// matchRefPattern reports whether ref matches pat.
//
// A pattern ending in "/*" is RECURSIVE: it matches every ref beneath that
// prefix at any depth, so "refs/heads/*" gates "refs/heads/agent/task-1" as
// well as "refs/heads/main". Plain path.Match would not - its "*" never
// crosses "/" - which silently left the branch naming agents actually use
// (agent/x, feature/x, dependabot/x/y) ungated and relaying unchecked.
// Anywhere else "*" keeps single-segment path.Match semantics.
func matchRefPattern(pat, ref string) bool {
	if prefix, recursive := strings.CutSuffix(pat, "/*"); recursive {
		// Split ref at each "/" and test the leading portion against the
		// prefix, so a glob in the prefix ("refs/heads/feat-*/*") still works.
		// Requiring a separator means at least one segment follows the prefix.
		for i := len(ref) - 1; i > 0; i-- {
			if ref[i] != '/' {
				continue
			}
			if ok, _ := path.Match(prefix, ref[:i]); ok {
				return true
			}
		}
		return false
	}
	ok, _ := path.Match(pat, ref)
	return ok
}

// refPatternErr returns the syntax error in pat, if any. It validates the same
// shape matchRefPattern interprets, so a pattern that loads cannot then fail to
// match for a reason validation missed.
func refPatternErr(pat string) error {
	if prefix, recursive := strings.CutSuffix(pat, "/*"); recursive {
		_, err := path.Match(prefix, "")
		return err
	}
	_, err := path.Match(pat, "")
	return err
}

// isGatedRef reports whether refName matches any ProtectedRefs glob.
func isGatedRef(p Policy, refName string) bool {
	if p.GateAllRefs {
		return true // gate every ref, not just protected ones
	}
	for _, pat := range p.ProtectedRefs {
		if matchRefPattern(pat, refName) {
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
//
// Deliberately still plain path.Match: widening this would make branches
// undeletable that are merely content-gated, re-coupling the two policies the
// 2026-06 decision split apart.
func isDeleteProtected(p Policy, refName string) bool {
	pats := append([]string{"refs/heads/main", "refs/heads/master"}, p.DeleteProtectedRefs...)
	for _, pat := range pats {
		if ok, _ := path.Match(pat, refName); ok {
			return true
		}
	}
	return false
}
