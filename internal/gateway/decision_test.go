// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"strings"
	"testing"
	"unicode/utf8"

	"nimblegate/internal/engine"
)

func block(id string) engine.CheckResult {
	return engine.CheckResult{FrameID: id, Outcome: engine.OutcomeBlock, Reason: "boom"}
}
func warn(id string) engine.CheckResult {
	return engine.CheckResult{FrameID: id, Outcome: engine.OutcomeWarn, Reason: "meh"}
}

func hasFinding(fs []Finding, id, sev string) bool {
	for _, f := range fs {
		if f.ID == id && f.Severity == sev {
			return true
		}
	}
	return false
}

func TestDecide(t *testing.T) {
	p := Policy{Enabled: true, ProtectedRefs: []string{"refs/heads/main"}}
	main := RefUpdate{Name: "refs/heads/main", OldRev: "a", NewRev: "b"}

	if d := Decide(p, []RefUpdate{main}, map[string][]engine.CheckResult{
		"refs/heads/main": {warn("convention/x")},
	}); !d.Accept {
		t.Errorf("WARN-only on protected ref should accept; got %+v", d)
	} else if !hasFinding(d.Findings, "convention/x", "WARN") {
		t.Errorf("WARN result must appear in Findings as WARN; got %+v", d.Findings)
	}
	if d := Decide(p, []RefUpdate{main}, map[string][]engine.CheckResult{
		"refs/heads/main": {block("security/x")},
	}); d.Accept || len(d.Messages) == 0 {
		t.Errorf("BLOCK on protected ref should reject with messages; got %+v", d)
	} else if !hasFinding(d.Findings, "security/x", "BLOCK") {
		t.Errorf("BLOCK result must also appear in Findings as BLOCK; got %+v", d.Findings)
	}
	if d := Decide(p, []RefUpdate{main}, map[string][]engine.CheckResult{
		"refs/heads/main": {{FrameID: "engine", Outcome: engine.OutcomeError, Reason: "check failed"}},
	}); d.Accept {
		t.Error("ERROR result must fail closed (reject)")
	}
	feat := RefUpdate{Name: "refs/heads/feature/y", OldRev: "a", NewRev: "b"}
	if d := Decide(p, []RefUpdate{feat}, map[string][]engine.CheckResult{}); !d.Accept {
		t.Error("non-protected ref should accept (free flow)")
	}
}

func TestDecide_disabledRepoPassesThrough(t *testing.T) {
	p := Policy{Enabled: false, ProtectedRefs: []string{"refs/heads/main"}}
	main := RefUpdate{Name: "refs/heads/main", OldRev: "a", NewRev: "b"}
	if d := Decide(p, []RefUpdate{main}, map[string][]engine.CheckResult{
		"refs/heads/main": {block("security/x")},
	}); !d.Accept {
		t.Error("disabled repo must pass through even with a BLOCK")
	}
}

func TestDecide_deleteProtectedRejected(t *testing.T) {
	p := Policy{Enabled: true, ProtectedRefs: []string{"refs/heads/main"}}
	del := RefUpdate{Name: "refs/heads/main", OldRev: "a", NewRev: zeroRev}
	d := Decide(p, []RefUpdate{del}, map[string][]engine.CheckResult{})
	if d.Accept {
		t.Error("deleting a protected ref must be rejected (destructive)")
	}
	if !hasFinding(d.Findings, "gateway/protected-ref-delete", "BLOCK") {
		t.Errorf("delete-reject must carry a gateway/protected-ref-delete BLOCK finding; got %+v", d.Findings)
	}
}

func TestDecide_deleteFeatureBranchAllowed(t *testing.T) {
	// refs/heads/* gates ALL branches' content, but delete-protection covers only
	// the default branch, so a feature branch stays deletable.
	p := Policy{Enabled: true, ProtectedRefs: []string{"refs/heads/*"}}
	if d := Decide(p, []RefUpdate{{Name: "refs/heads/feature-x", OldRev: "a", NewRev: zeroRev}}, map[string][]engine.CheckResult{}); !d.Accept {
		t.Errorf("deleting a gated-but-not-delete-protected feature branch must be allowed; got %+v", d.Findings)
	}
	if d := Decide(p, []RefUpdate{{Name: "refs/heads/main", OldRev: "a", NewRev: zeroRev}}, map[string][]engine.CheckResult{}); d.Accept {
		t.Error("deleting main (always protected) must still be rejected")
	}
}

func TestDecide_deleteProtectedRefsAddsToDefault(t *testing.T) {
	p := Policy{Enabled: true, ProtectedRefs: []string{"refs/heads/*"}, DeleteProtectedRefs: []string{"refs/heads/release/*"}}
	if d := Decide(p, []RefUpdate{{Name: "refs/heads/release/1.0", OldRev: "a", NewRev: zeroRev}}, map[string][]engine.CheckResult{}); d.Accept {
		t.Error("a ref matching DeleteProtectedRefs must be undeletable")
	}
	if d := Decide(p, []RefUpdate{{Name: "refs/heads/main", OldRev: "a", NewRev: zeroRev}}, map[string][]engine.CheckResult{}); d.Accept {
		t.Error("main stays protected even when DeleteProtectedRefs is set")
	}
	if d := Decide(p, []RefUpdate{{Name: "refs/heads/feature-y", OldRev: "a", NewRev: zeroRev}}, map[string][]engine.CheckResult{}); !d.Accept {
		t.Error("a feature branch not in the protected set must be deletable")
	}
}

func TestFindingMessage_longReasonBounded(t *testing.T) {
	// Build a Reason that is 1000 chars long (all ASCII) - simulates a linter
	// returning the full semicolon-joined hit list; must exceed findingMessageCap
	// so truncation is still exercised.
	longReason := strings.Repeat("x", 1000)
	res := engine.CheckResult{
		FrameID: "linters/go-vet",
		Outcome: engine.OutcomeBlock,
		Reason:  longReason,
	}
	msg := findingMessage(res)
	if n := utf8.RuneCountInString(msg); n > findingMessageCap+3 {
		// allow a tiny margin for the ellipsis " …" (2 runes)
		t.Errorf("message length %d exceeds cap %d (+ellipsis); got %q", n, findingMessageCap, msg)
	}
	if !strings.HasSuffix(msg, " …") {
		t.Errorf("truncated message must end with \" …\"; got %q", msg)
	}
	if strings.ContainsRune(msg, '\n') {
		t.Errorf("message must be single-line; got %q", msg)
	}
}

func TestFindingMessage_multilineTruncatedToFirstLine(t *testing.T) {
	res := engine.CheckResult{
		FrameID: "linters/go-vet",
		Outcome: engine.OutcomeWarn,
		Reason:  "first line\nsecond line\nthird line",
	}
	msg := findingMessage(res)
	if msg != "first line" {
		t.Errorf("expected only first line; got %q", msg)
	}
}

func TestFindingMessage_shortReasonUnchanged(t *testing.T) {
	res := engine.CheckResult{
		FrameID: "security/x",
		Outcome: engine.OutcomeBlock,
		Reason:  "short reason",
	}
	msg := findingMessage(res)
	if msg != "short reason" {
		t.Errorf("short reason must pass through unchanged; got %q", msg)
	}
}
