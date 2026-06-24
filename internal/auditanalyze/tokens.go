// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package auditanalyze

import (
	"sort"
	"strings"
	"unicode"
)

// stopwords are extremely common English fillers that should never be
// surfaced as a "hotspot." Lowercased. Short list intentionally - domain
// terms like "ci", "wip" should pass through, but they're short enough
// to be filtered by MinTokenLen anyway.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "that": true,
	"this": true, "from": true, "into": true, "have": true, "been": true,
	"will": true, "would": true, "could": true, "should": true, "just": true,
	"only": true, "also": true, "very": true, "more": true, "less": true,
	"some": true, "such": true, "than": true, "then": true, "they": true,
	"them": true, "what": true, "when": true, "where": true, "which": true,
	"reason": true, "needs": true, "need": true, "test": false, // not a stopword in this domain
}

// tokenize splits s into lowercase tokens >= minLen, filtering stopwords.
// Splits on any non-alphanumeric, non-hyphen character so kebab-case
// identifiers stay intact (e.g. "force-yes" → "force-yes", not two tokens).
//
// Strips a leading "--force-yes:" prefix before tokenizing, since the
// override-audit format consistently prepends it to every bypass reason -
// counting it would surface the prefix as the top cluster on every report.
func tokenize(s string, minLen int) []string {
	s = strings.ToLower(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, "--force-yes:"))
	var out []string
	var cur strings.Builder
	flush := func() {
		t := cur.String()
		cur.Reset()
		if len(t) < minLen {
			return
		}
		if stopwords[t] {
			return
		}
		out = append(out, t)
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

// topTokens returns the most frequent tokens across reasons, filtering by
// minLen and minHits, capped at maxTokens. Each reason contributes at most
// one count per distinct token (i.e. a reason that says "vendor vendor" still
// counts +1 toward "vendor"). This avoids letting one verbose reason dominate.
func topTokens(reasons []string, minLen, minHits, maxTokens int) []HotspotToken {
	counts := map[string]int{}
	for _, r := range reasons {
		seen := map[string]bool{}
		for _, t := range tokenize(r, minLen) {
			if seen[t] {
				continue
			}
			seen[t] = true
			counts[t]++
		}
	}
	var out []HotspotToken
	for t, c := range counts {
		if c >= minHits {
			out = append(out, HotspotToken{Token: t, Count: c})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Token < out[j].Token
	})
	if len(out) > maxTokens {
		out = out[:maxTokens]
	}
	return out
}
