// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// Real `go vet -json ./...` output captured 2026-05-24 from a two-package
// module with three printf findings plus one clean package. The stream
// interleaves `#`-prefixed progress lines (stderr) with concatenated
// pretty-printed JSON objects, one per package.
const goVetTwoPackages = `# vetfix
{
	"vetfix": {
		"printf": [
			{
				"posn": "/tmp/vetfix/bad.go:8:9",
				"message": "fmt.Sprintf format %s reads arg #2, but call has 1 arg"
			}
		]
	}
}
# vetfix/sub
{
	"vetfix/sub": {
		"printf": [
			{
				"posn": "/tmp/vetfix/sub/more.go:7:2",
				"message": "fmt.Printf format %d reads arg #2, but call has 1 arg"
			},
			{
				"posn": "/tmp/vetfix/sub/more.go:8:6",
				"message": "fmt.Sprintf format %w has arg \"x\" of wrong type string"
			}
		]
	}
}
# vetfix/clean
{}
`

func TestParseGoVetOutput_findingsBecomeSortedHits(t *testing.T) {
	got := parseGoVetOutput(goVetTwoPackages, "/tmp/vetfix", engine.OutcomeBlock)

	if got.FrameID != goVetFrameID {
		t.Errorf("FrameID = %q, want %q", got.FrameID, goVetFrameID)
	}
	if got.Category != frames.CategoryAppCorrectness {
		t.Errorf("Category = %q, want app-correctness", got.Category)
	}
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("Outcome = %q, want BLOCK", got.Outcome)
	}

	// Sorted by (file, line) so output + Reason are deterministic despite
	// go vet's per-package map iteration.
	want := []engine.Hit{
		{File: "bad.go", Line: 8, Label: `printf: fmt.Sprintf format %s reads arg #2, but call has 1 arg`},
		{File: "sub/more.go", Line: 7, Label: `printf: fmt.Printf format %d reads arg #2, but call has 1 arg`},
		{File: "sub/more.go", Line: 8, Label: `printf: fmt.Sprintf format %w has arg "x" of wrong type string`},
	}
	if len(got.Hits) != len(want) {
		t.Fatalf("got %d hits, want %d: %+v", len(got.Hits), len(want), got.Hits)
	}
	for i, w := range want {
		if got.Hits[i] != w {
			t.Errorf("hit[%d] = %+v, want %+v", i, got.Hits[i], w)
		}
	}

	// Reason must follow the "<header>: <hit>; <hit>" convention so the
	// whitelist suppression pass can rebuild it from surviving hits.
	wantReason := "go vet: bad.go:8 - printf: fmt.Sprintf format %s reads arg #2, but call has 1 arg" +
		"; sub/more.go:7 - printf: fmt.Printf format %d reads arg #2, but call has 1 arg" +
		"; sub/more.go:8 - printf: fmt.Sprintf format %w has arg \"x\" of wrong type string"
	if got.Reason != wantReason {
		t.Errorf("Reason mismatch:\n got: %q\nwant: %q", got.Reason, wantReason)
	}
}

func TestParseGoVetOutput_severityControlsOutcome(t *testing.T) {
	got := parseGoVetOutput(goVetTwoPackages, "/tmp/vetfix", engine.OutcomeWarn)
	if got.Outcome != engine.OutcomeWarn {
		t.Errorf("Outcome = %q, want WARN", got.Outcome)
	}
}

func TestParseGoVetOutput_noFindingsIsPass(t *testing.T) {
	clean := "# vetfix\n{}\n# vetfix/sub\n{}\n"
	got := parseGoVetOutput(clean, "/tmp/vetfix", engine.OutcomeBlock)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("Outcome = %q, want PASS", got.Outcome)
	}
	if len(got.Hits) != 0 {
		t.Errorf("clean run should have no hits, got %+v", got.Hits)
	}
	if got.Reason != "go vet: no findings" {
		t.Errorf("Reason = %q, want \"go vet: no findings\"", got.Reason)
	}
}

func TestParseGoVetOutput_absolutePathOutsideRootKept(t *testing.T) {
	// A posn that isn't under projectRoot stays as-is (can't be made
	// relative) rather than producing a bogus ../.. path.
	out := `{
	"x": {
		"printf": [
			{"posn": "/elsewhere/y.go:3:1", "message": "boom"}
		]
	}
}
`
	got := parseGoVetOutput(out, "/tmp/vetfix", engine.OutcomeBlock)
	if len(got.Hits) != 1 || got.Hits[0].File != "/elsewhere/y.go" {
		t.Fatalf("expected absolute path kept, got %+v", got.Hits)
	}
}
