// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "strings"

// ForeignTOMLBlocks returns the tables of doc that a whole-file writer does
// not own, verbatim and in order, so the writer can append them and leave
// other writers' sections intact. owned gets the table name without
// brackets: `frames."security/x"` for [frames."security/x"], `ui.user_kits`
// for [[ui.user_kits]]. Lines before the first table belong to the writer.
func ForeignTOMLBlocks(doc string, owned func(table string) bool) string {
	var out strings.Builder
	keep := false
	for _, line := range strings.SplitAfter(doc, "\n") {
		if strings.HasPrefix(line, "[") {
			name := strings.TrimSpace(line)
			if i := strings.Index(name, "#"); i > 0 && !strings.Contains(name[:i], `"`) {
				name = strings.TrimSpace(name[:i])
			}
			name = strings.TrimSuffix(strings.TrimSuffix(name, "]"), "]")
			name = strings.TrimPrefix(strings.TrimPrefix(name, "["), "[")
			keep = !owned(strings.TrimSpace(name))
		}
		if keep {
			out.WriteString(line)
		}
	}
	s := out.String()
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}
