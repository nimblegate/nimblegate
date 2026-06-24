// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"strings"
	"testing"
)

func TestParsePattern_Minimal(t *testing.T) {
	src := `---
id: example-pattern
description: A short description for the test.
---
Body content.
`
	p, err := ParsePattern(strings.NewReader(src), "test://example")
	if err != nil {
		t.Fatalf("ParsePattern: unexpected error: %v", err)
	}
	if p.ID() != "example-pattern" {
		t.Errorf("ID: got %q, want %q", p.ID(), "example-pattern")
	}
	if p.Frontmatter.Description != "A short description for the test." {
		t.Errorf("Description: got %q", p.Frontmatter.Description)
	}
	if !strings.Contains(p.Body, "Body content.") {
		t.Errorf("Body: missing expected text: %q", p.Body)
	}
}

func TestParsePattern_WithSiblings(t *testing.T) {
	src := `---
id: with-siblings
description: Has anticipated siblings.
anticipated-siblings:
  - sibling-one
  - sibling-two
---
`
	p, err := ParsePattern(strings.NewReader(src), "test://with-siblings")
	if err != nil {
		t.Fatalf("ParsePattern: unexpected error: %v", err)
	}
	if len(p.Frontmatter.AnticipatedSiblings) != 2 {
		t.Fatalf("AnticipatedSiblings: got %d, want 2", len(p.Frontmatter.AnticipatedSiblings))
	}
	if p.Frontmatter.AnticipatedSiblings[0] != "sibling-one" {
		t.Errorf("first sibling: got %q", p.Frontmatter.AnticipatedSiblings[0])
	}
}

func TestParsePattern_MissingID(t *testing.T) {
	src := `---
description: No id field.
---
`
	if _, err := ParsePattern(strings.NewReader(src), "test://noid"); err == nil {
		t.Error("expected error for missing id, got nil")
	}
}

func TestParsePattern_MissingDescription(t *testing.T) {
	src := `---
id: pattern-without-description
---
`
	if _, err := ParsePattern(strings.NewReader(src), "test://nodesc"); err == nil {
		t.Error("expected error for missing description, got nil")
	}
}

func TestParsePattern_InvalidIDFormat(t *testing.T) {
	src := `---
id: not valid id with spaces
description: Has spaces in id.
---
`
	if _, err := ParsePattern(strings.NewReader(src), "test://badid"); err == nil {
		t.Error("expected error for invalid id format, got nil")
	}
}

func TestParsePattern_NoOpeningFence(t *testing.T) {
	src := `id: missing-fence
description: No leading ---.
`
	if _, err := ParsePattern(strings.NewReader(src), "test://nofence"); err == nil {
		t.Error("expected error for missing opening fence, got nil")
	}
}

func TestFrontmatter_EffectiveLifecycle(t *testing.T) {
	cases := []struct {
		in   Lifecycle
		want Lifecycle
	}{
		{"", LifecycleActive},
		{LifecycleProposed, LifecycleProposed},
		{LifecycleCandidate, LifecycleCandidate},
		{LifecycleActive, LifecycleActive},
		{LifecycleDeprecated, LifecycleDeprecated},
		{LifecycleArchived, LifecycleArchived},
	}
	for _, c := range cases {
		fm := Frontmatter{Lifecycle: c.in}
		if got := fm.EffectiveLifecycle(); got != c.want {
			t.Errorf("EffectiveLifecycle(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}
