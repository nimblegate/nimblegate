// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"nimblegate/internal/engine"
)

// Decide is the pure gate verdict for a whole push. All-or-nothing: if any
// gated ref carries a blocking finding (BLOCK or ERROR), or deletes a
// protected ref, the entire push is rejected. Disabled repo → pass-through.
//
// resultsByRef holds the check results for each GATED ref (keyed by full ref
// name). The orchestrator runs the check and, on a check failure, inserts a
// synthetic OutcomeError result so this stays the single decision point.
func Decide(p Policy, refs []RefUpdate, resultsByRef map[string][]engine.CheckResult) Decision {
	if !p.Enabled {
		return Decision{Accept: true}
	}
	var msgs []string
	var findings []Finding
	var scanFailures []string
	scanFailed := false
	scanRefSeen := map[string]bool{}
	reject := false
	for _, r := range refs {
		// Delete-protection is independent of content-gating: a delete-protected
		// ref can't be removed whether or not its content is gated. A delete has no
		// content to check, so this is the only thing a delete is evaluated against.
		if r.IsDelete() {
			if isDeleteProtected(p, r.Name) {
				reject = true
				msgs = append(msgs, fmt.Sprintf("%s: deleting a protected branch is not allowed", r.Name))
				findings = append(findings, Finding{
					ID:       "gateway/protected-ref-delete",
					Severity: "BLOCK",
					Message:  r.Name + ": deleting a protected branch is not allowed",
				})
			}
			continue
		}
		if !isGatedRef(p, r.Name) {
			continue
		}
		for _, res := range resultsByRef[r.Name] {
			sev := severityWord(res.Outcome)
			// The gate could not evaluate this ref. Reject exactly as a BLOCK
			// would, but the detail names gateway internals, so the pusher gets
			// only the neutral per-ref line any host prints and the cause goes
			// operator-side.
			if res.FrameID == ScanFailedID {
				reject = true
				scanFailed = true
				if !scanRefSeen[r.Name] {
					scanRefSeen[r.Name] = true
					msgs = append(msgs, fmt.Sprintf("%s: rejected", r.Name))
				}
				scanFailures = append(scanFailures, fmt.Sprintf("%s: %s [%s] %s", r.Name, res.Outcome, res.FrameID, res.Reason))
				findings = append(findings, Finding{ID: res.FrameID, Severity: sev, Message: findingMessage(res), Origin: res.Origin})
				continue
			}
			switch res.Outcome {
			case engine.OutcomeBlock, engine.OutcomeError:
				reject = true
				msgs = append(msgs, fmt.Sprintf("%s: %s [%s] %s", r.Name, res.Outcome, res.FrameID, res.Reason))
				findings = append(findings, Finding{ID: res.FrameID, Severity: sev, Message: findingMessage(res), Origin: res.Origin})
			case engine.OutcomeWarn, engine.OutcomeInfo:
				findings = append(findings, Finding{ID: res.FrameID, Severity: sev, Message: findingMessage(res), Origin: res.Origin})
			}
		}
	}
	return Decision{Accept: !reject, Messages: msgs, Findings: findings, ScanFailures: scanFailures, ScanFailed: scanFailed}
}

// severityWord maps a CheckOutcome to the uppercase severity token used in
// findings (BLOCK | ERROR | WARN | INFO). CheckOutcome has no String() method;
// its underlying string values are already these tokens, but the explicit map
// pins the contract the dashboard CSS keys on.
func severityWord(o engine.CheckOutcome) string {
	switch o {
	case engine.OutcomeBlock:
		return "BLOCK"
	case engine.OutcomeError:
		return "ERROR"
	case engine.OutcomeWarn:
		return "WARN"
	case engine.OutcomeInfo:
		return "INFO"
	default:
		return strings.ToUpper(string(o))
	}
}

// findingMessage returns a short, human-readable label for a non-pass result.
// Linter and frame results carry a populated Reason; if Reason is empty (a
// hand-built result), fall back to a short summary of the first hit plus a
// count. The returned message is always single-line and capped at 800 runes to
// prevent audit-log bloat when Reason contains the full semicolon-joined hit
// list from a linter with many matches.
const findingMessageCap = 800

func findingMessage(res engine.CheckResult) string {
	if msg := strings.TrimSpace(res.Reason); msg != "" {
		return boundMessage(msg)
	}
	if len(res.Hits) > 0 {
		msg := res.Hits[0].Format()
		if more := len(res.Hits) - 1; more > 0 {
			msg += fmt.Sprintf(" (+%d more)", more)
		}
		return boundMessage(msg)
	}
	return ""
}

// boundMessage truncates s to the first line and caps it at findingMessageCap
// runes, appending " …" when truncated.
func boundMessage(s string) string {
	// keep only the first line
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= findingMessageCap {
		return s
	}
	// truncate to cap runes
	i := 0
	for n := range s {
		if i == findingMessageCap {
			return s[:n] + " …"
		}
		i++
	}
	return s
}
