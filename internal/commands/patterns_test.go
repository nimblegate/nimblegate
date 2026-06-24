// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"testing"

	"nimblegate/internal/stdlib"
)

// TestPatternsLoadable verifies every stdlib pattern parses cleanly.
// Equivalent to the frame loading invariant - load failure here means
// a malformed pattern file shipped with the binary.
func TestPatternsLoadable(t *testing.T) {
	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	if len(patterns) == 0 {
		t.Fatal("LoadPatterns returned 0 patterns; expected at least 24")
	}
	seen := map[string]bool{}
	for _, p := range patterns {
		if p.ID() == "" {
			t.Errorf("pattern at %s has empty ID", p.SourcePath)
		}
		if seen[p.ID()] {
			t.Errorf("duplicate pattern ID: %s", p.ID())
		}
		seen[p.ID()] = true
		if p.Frontmatter.Description == "" {
			t.Errorf("pattern %s has empty description", p.ID())
		}
	}
}

// TestFramesReferenceExistingPatterns verifies every stdlib frame's
// pattern field, when set, references a pattern that actually exists.
// Validates the linkage invariant of the Phase 1 architecture.
func TestFramesReferenceExistingPatterns(t *testing.T) {
	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		t.Fatalf("LoadPatterns: %v", err)
	}
	frames, err := stdlib.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	known := map[string]bool{}
	for _, p := range patterns {
		known[p.ID()] = true
	}
	for _, f := range frames {
		p := f.Frontmatter.Pattern
		if p == "" {
			t.Errorf("frame %s has no pattern field: Phase 1 expects all stdlib frames bound", f.ID())
			continue
		}
		if !known[p] {
			t.Errorf("frame %s references unknown pattern %q", f.ID(), p)
		}
	}
}
