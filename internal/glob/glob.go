// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package glob compiles doublestar-style path globs (* / ** / ?) into
// anchored regular expressions. Shared between internal/whitelist and
// internal/scanignore so both surfaces share identical match semantics.
package glob

import (
	"regexp"
	"strings"
)

// Compile converts a path glob into a regex anchored at both ends.
//
// Semantics:
//
//	**     matches any number of characters including `/` (deep recursion)
//	*      matches any number of characters except `/`  (one path segment)
//	?      matches one character except `/`
//	other  literal (regex-special chars are escaped)
//
// A `**/` prefix or `/`-bordered `**` also matches zero path segments,
// so `**/foo` matches both `foo` and `a/b/foo`.
func Compile(g string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	i := 0
	for i < len(g) {
		c := g[i]
		switch {
		case c == '*' && i+1 < len(g) && g[i+1] == '*':
			i += 2
			if i < len(g) && g[i] == '/' {
				b.WriteString(`(?:.*/)?`)
				i++
			} else {
				b.WriteString(`.*`)
			}
		case c == '*':
			b.WriteString(`[^/]*`)
			i++
		case c == '?':
			b.WriteString(`[^/]`)
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}
