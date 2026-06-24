// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/frames"
)

func TestFilterGated_SplitsByLifecycle(t *testing.T) {
	input := []frames.Frame{
		{Frontmatter: frames.Frontmatter{Name: "a", Lifecycle: frames.LifecycleActive}},
		{Frontmatter: frames.Frontmatter{Name: "b", Lifecycle: frames.LifecycleCandidate}},
		{Frontmatter: frames.Frontmatter{Name: "c", Lifecycle: ""}}, // defaults to active
		{Frontmatter: frames.Frontmatter{Name: "d", Lifecycle: frames.LifecycleProposed}},
		{Frontmatter: frames.Frontmatter{Name: "e", Lifecycle: frames.LifecycleDeprecated}},
		{Frontmatter: frames.Frontmatter{Name: "f", Lifecycle: frames.LifecycleArchived}},
	}

	gated, skipped := filterGated(input)
	if len(gated) != 3 {
		t.Errorf("gated count: got %d, want 3 (active, candidate, empty-default)", len(gated))
	}
	if len(skipped) != 3 {
		t.Errorf("skipped count: got %d, want 3 (proposed, deprecated, archived)", len(skipped))
	}
	gatedNames := map[string]bool{}
	for _, f := range gated {
		gatedNames[f.Frontmatter.Name] = true
	}
	for _, name := range []string{"a", "b", "c"} {
		if !gatedNames[name] {
			t.Errorf("expected %q in gated, missing", name)
		}
	}
	skippedNames := map[string]bool{}
	for _, f := range skipped {
		skippedNames[f.Frontmatter.Name] = true
	}
	for _, name := range []string{"d", "e", "f"} {
		if !skippedNames[name] {
			t.Errorf("expected %q in skipped, missing", name)
		}
	}
}

func TestFilterGated_EmptySliceReturnsEmpty(t *testing.T) {
	gated, skipped := filterGated(nil)
	if len(gated) != 0 || len(skipped) != 0 {
		t.Errorf("expected empty slices, got gated=%d skipped=%d", len(gated), len(skipped))
	}
}
