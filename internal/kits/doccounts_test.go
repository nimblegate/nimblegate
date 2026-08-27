package kits

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// docs/frames.md publishes a frame count per kit. Five of the six had drifted
// from the kit definitions before this test existed, and a wrong count is the
// kind of thing a reader checks first. Pin the table to stdlib.toml.
func TestDocsFrameCountsMatchKits(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "frames.md"))
	if err != nil {
		t.Skipf("docs/frames.md not readable: %v", err)
	}
	ks, err := LoadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile(`(?m)^\| ` + "`" + `([a-z0-9-]+)` + "`" + ` \|.*\| (\d+) \|$`)
	seen := 0
	for _, m := range row.FindAllStringSubmatch(string(doc), -1) {
		k, ok := ks.Get(m[1])
		if !ok {
			continue // a table row for something that isn't a kit
		}
		want, _ := strconv.Atoi(m[2])
		if len(k.Frames) != want {
			t.Errorf("docs/frames.md says kit %q has %d frames; stdlib.toml has %d", m[1], want, len(k.Frames))
		}
		seen++
	}
	if seen != len(ks.All()) {
		t.Errorf("matched %d kit rows in docs/frames.md, stdlib has %d kits", seen, len(ks.All()))
	}
}
