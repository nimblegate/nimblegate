// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"strings"
	"testing"
)

// rewriteEnabledList is the load-bearing piece of `nimblegate enable/disable`.
// These tests pin its contract: preserve everything outside the enabled
// array, sort the inside, idempotent on no-op.

func TestRewriteEnabledList_AddsNewEntrySortedAlphabetically(t *testing.T) {
	doc := `# top comment
[project]
name = "x"

[frames]
# this comment must survive
enabled = [
    "git-safety/*",
    "security/*",
]

[triggers]
cli = true
`
	out, changed, err := rewriteEnabledList(doc, "command-safety/*", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	// Result must contain the new entry, sorted.
	if !strings.Contains(out, `"command-safety/*"`) {
		t.Errorf("missing new entry; got:\n%s", out)
	}
	// Sort order: command-safety < git-safety < security.
	pos := []int{
		strings.Index(out, `"command-safety/*"`),
		strings.Index(out, `"git-safety/*"`),
		strings.Index(out, `"security/*"`),
	}
	if !(pos[0] < pos[1] && pos[1] < pos[2]) {
		t.Errorf("entries not sorted; positions: %v", pos)
	}
	// Surrounding comments + sections must survive.
	if !strings.Contains(out, "# top comment") {
		t.Error("top comment lost")
	}
	if !strings.Contains(out, "# this comment must survive") {
		t.Error("inside-section comment lost")
	}
	if !strings.Contains(out, "[triggers]") {
		t.Error("post-section content lost")
	}
}

func TestRewriteEnabledList_RemovesEntry(t *testing.T) {
	doc := `[frames]
enabled = [
    "git-safety/*",
    "@tier-1",
    "security/*",
]
`
	out, changed, err := rewriteEnabledList(doc, "@tier-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	if strings.Contains(out, "@tier-1") {
		t.Errorf("@tier-1 should be removed; got:\n%s", out)
	}
	if !strings.Contains(out, `"git-safety/*"`) || !strings.Contains(out, `"security/*"`) {
		t.Errorf("other entries should survive; got:\n%s", out)
	}
}

func TestRewriteEnabledList_NoOpWhenAlreadyPresent(t *testing.T) {
	doc := `[frames]
enabled = ["@tier-1", "security/*"]
`
	_, changed, err := rewriteEnabledList(doc, "@tier-1", true)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected changed=false (already present)")
	}
}

func TestRewriteEnabledList_NoOpWhenRemovingAbsent(t *testing.T) {
	doc := `[frames]
enabled = ["@tier-1"]
`
	_, changed, err := rewriteEnabledList(doc, "security/*", false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("expected changed=false (already absent)")
	}
}

func TestRewriteEnabledList_HandlesInlineArray(t *testing.T) {
	// Some users write a single-line array. Rewriter must cope.
	doc := `[frames]
enabled = ["@tier-1", "security/*"]
`
	out, changed, err := rewriteEnabledList(doc, "command-safety/*", true)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected changed=true")
	}
	for _, want := range []string{`"@tier-1"`, `"command-safety/*"`, `"security/*"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

func TestRewriteEnabledList_StripsComments(t *testing.T) {
	doc := `[frames]
enabled = [
    "@tier-1",      # security baseline
    "convention/*", # docs
]
`
	out, _, err := rewriteEnabledList(doc, "security/*", true)
	if err != nil {
		t.Fatal(err)
	}
	// Original inline comments are dropped by the rewriter (single-line
	// canonicalization). The entries themselves survive.
	for _, want := range []string{`"@tier-1"`, `"convention/*"`, `"security/*"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s; got:\n%s", want, out)
		}
	}
}

func TestRewriteEnabledList_ErrorsOnUnparseableShape(t *testing.T) {
	// No [frames] enabled = [...] block present.
	doc := `[project]
name = "x"

[triggers]
cli = true
`
	_, _, err := rewriteEnabledList(doc, "anything", true)
	if err == nil {
		t.Fatal("expected error when enabled array can't be located")
	}
}

func TestParseEnabledBody_StripsCommentsAndTrailingCommas(t *testing.T) {
	body := `
    "a/x",  # inline
    "b/y",
    # standalone
    "c/z",
`
	got := parseEnabledBody(body)
	want := []string{"a/x", "b/y", "c/z"}
	if len(got) != len(want) {
		t.Fatalf("parseEnabledBody got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] %q != %q", i, got[i], want[i])
		}
	}
}

func TestParseEnabledBody_DedupesEntries(t *testing.T) {
	body := `"a/x", "a/x", "b/y"`
	got := parseEnabledBody(body)
	if len(got) != 2 || got[0] != "a/x" || got[1] != "b/y" {
		t.Errorf("dedup failed; got %v", got)
	}
}
