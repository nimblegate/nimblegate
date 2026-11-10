// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"nimblegate/internal/frames"
)

// PolicyEntriesID is the synthetic result for [frames] enabled entries that
// name no frame. WARN when other entries still resolve; BLOCK when the
// non-empty list resolves to nothing, because that policy runs no checks.
const PolicyEntriesID = "policy/unknown-entries"

const (
	policyEntryRuneCap = 64
	policyEntryListCap = 10
)

// UnknownEnabledEntries returns the entries of a non-empty enabled list that
// resolve to no known ID, and whether the list resolves to nothing at all. An
// entry resolves when it is a known ID, or a "category/*" pattern matching at
// least one. "@group" entries (removed in v0.1.0) never resolve. An empty list
// means every frame and has no unknown entries.
func UnknownEnabledEntries(enabled []string, known map[string]bool) (unknown []string, none bool) {
	if len(enabled) == 0 {
		return nil, false
	}
	resolved := 0
	for _, e := range enabled {
		if entryResolves(e, known) {
			resolved++
		} else {
			unknown = append(unknown, e)
		}
	}
	return unknown, resolved == 0
}

func entryResolves(e string, known map[string]bool) bool {
	if strings.HasPrefix(e, "@") {
		return false
	}
	if known[e] {
		return true
	}
	if !strings.HasSuffix(e, "/*") || len(e) <= 2 {
		return false
	}
	for id := range known {
		if isFrameEnabled(id, []string{e}) {
			return true
		}
	}
	return false
}

// PolicyEntriesResults turns the resolver's answer into the synthetic check
// result the gate, the preview and `nimblegate check` all report; nil when
// every entry resolves.
func PolicyEntriesResults(enabled []string, known map[string]bool) []CheckResult {
	unknown, none := UnknownEnabledEntries(enabled, known)
	if len(unknown) == 0 {
		return nil
	}
	msg := PolicyEntriesMessage(unknown)
	outcome := OutcomeWarn
	if none {
		outcome = OutcomeBlock
		msg = "policy enables no checks; " + msg
	}
	return []CheckResult{{
		FrameID:  PolicyEntriesID,
		Category: frames.Category("policy"),
		Outcome:  outcome,
		Reason:   msg,
	}}
}

// PolicyEntriesMessage names the unknown entries (sanitised, capped) and,
// when any uses the removed @group syntax, points at the migration.
func PolicyEntriesMessage(unknown []string) string {
	labels := PolicyEntryLabels(unknown)
	msg := "[frames] enabled entries match no frame: " + strings.Join(labels, ", ")
	if HasGroupEntry(unknown) {
		msg += " (@group syntax was removed; `nimblegate lint` shows the kit to apply)"
	}
	return msg
}

// HasGroupEntry reports whether any entry uses the removed @group syntax.
func HasGroupEntry(entries []string) bool {
	for _, e := range entries {
		if strings.HasPrefix(e, "@") {
			return true
		}
	}
	return false
}

// PolicyEntryLabels returns display-safe labels: control and format (bidi,
// zero-width) characters stripped, each capped in length, the list capped
// with an "and N more" tail.
// Entries come from operator config but end up in audit rows, webhooks and
// terminals, so they are treated as untrusted text.
func PolicyEntryLabels(entries []string) []string {
	n := len(entries)
	if n > policyEntryListCap {
		entries = entries[:policyEntryListCap]
	}
	out := make([]string, 0, len(entries)+1)
	for _, e := range entries {
		out = append(out, sanitizeEntry(e))
	}
	if n > policyEntryListCap {
		out = append(out, fmt.Sprintf("and %d more", n-policyEntryListCap))
	}
	return out
}

func sanitizeEntry(e string) string {
	e = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, e)
	if utf8.RuneCountInString(e) > policyEntryRuneCap {
		e = string([]rune(e)[:policyEntryRuneCap]) + "..."
	}
	if e == "" {
		return `""`
	}
	return e
}
