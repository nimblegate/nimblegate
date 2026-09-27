// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package stdlib

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// Reference docs state the exact number of built-in frames; everywhere else
// (README intro, landing, posts) says "50+" so it survives new frames. This
// pins the exact statements to the catalog, so a new frame fails here
// instead of leaving a stale count behind. Per-kit counts are pinned in
// internal/kits.
func TestDocsTotalFrameCount(t *testing.T) {
	all, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		file string
		re   string
	}{
		{"docs/frames.md", `from all (\d+) frames`},
		{"docs/policy-authoring.md", `ships \*\*(\d+) rules`},
	} {
		doc, err := os.ReadFile(filepath.Join("..", "..", c.file))
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		m := regexp.MustCompile(c.re).FindSubmatch(doc)
		if m == nil {
			t.Errorf("%s: no match for %q - the sentence was reworded; update this test", c.file, c.re)
			continue
		}
		if got, _ := strconv.Atoi(string(m[1])); got != len(all) {
			t.Errorf("%s says %d frames; the stdlib has %d", c.file, got, len(all))
		}
	}
}
