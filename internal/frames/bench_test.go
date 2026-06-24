// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"strings"
	"testing"
)

const benchFrameContent = `---
name: bench
category: security
severity: WARN
triggers:
  - cli
  - pre-commit
applies-to:
  files:
    - "**/*.js"
    - "**/*.ts"
canonical-refs:
  - website-ids.toml
---

# Bench frame body

Some text describing the rule.
`

// BenchmarkParse_TypicalFrame
func BenchmarkParse_TypicalFrame(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchFrameContent)))
	for i := 0; i < b.N; i++ {
		_, err := Parse(strings.NewReader(benchFrameContent), "bench")
		if err != nil {
			b.Fatal(err)
		}
	}
}
