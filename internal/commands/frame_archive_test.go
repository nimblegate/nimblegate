// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"strings"
	"testing"
)

func TestRewriteForArchive_ReplaceExistingLifecycle(t *testing.T) {
	src := `---
name: example
category: convention
severity: WARN
tier: 6
triggers: [cli]
lifecycle: active
selection-grade: passing
---
body
`
	out, err := rewriteForArchive(src, "platform retired", "2026-05-20T10:00:00Z")
	if err != nil {
		t.Fatalf("rewriteForArchive: %v", err)
	}
	if !strings.Contains(out, "lifecycle: archived") {
		t.Errorf("missing lifecycle: archived in output:\n%s", out)
	}
	if !strings.Contains(out, "archived-at: 2026-05-20T10:00:00Z") {
		t.Errorf("missing archived-at in output:\n%s", out)
	}
	if !strings.Contains(out, "archive-reason: platform retired") {
		t.Errorf("missing archive-reason in output:\n%s", out)
	}
	if strings.Contains(out, "lifecycle: active") {
		t.Errorf("old lifecycle still present:\n%s", out)
	}
	// Existing unrelated fields preserved.
	if !strings.Contains(out, "selection-grade: passing") {
		t.Errorf("unrelated field selection-grade lost:\n%s", out)
	}
	// Body preserved.
	if !strings.Contains(out, "body") {
		t.Errorf("body lost:\n%s", out)
	}
}

func TestRewriteForArchive_InsertWhenAbsent(t *testing.T) {
	src := `---
name: example
category: convention
severity: WARN
tier: 6
triggers: [cli]
---
`
	out, err := rewriteForArchive(src, "", "2026-05-20T10:00:00Z")
	if err != nil {
		t.Fatalf("rewriteForArchive: %v", err)
	}
	if !strings.Contains(out, "lifecycle: archived") {
		t.Errorf("missing lifecycle: archived (insert path):\n%s", out)
	}
	if !strings.Contains(out, "archived-at: 2026-05-20T10:00:00Z") {
		t.Errorf("missing archived-at (insert path):\n%s", out)
	}
	// Empty reason should be omitted, not written as `archive-reason:` blank.
	if strings.Contains(out, "archive-reason:") {
		t.Errorf("empty reason should be omitted, got it in output:\n%s", out)
	}
}

func TestRewriteForRevive_ClearsArchivedFields(t *testing.T) {
	src := `---
name: example
category: convention
severity: WARN
tier: 6
triggers: [cli]
lifecycle: archived
archived-at: 2026-05-20T10:00:00Z
archive-reason: platform retired
---
body
`
	out, err := rewriteForRevive(src)
	if err != nil {
		t.Fatalf("rewriteForRevive: %v", err)
	}
	if !strings.Contains(out, "lifecycle: active") {
		t.Errorf("missing lifecycle: active:\n%s", out)
	}
	if strings.Contains(out, "archived-at") {
		t.Errorf("archived-at should be cleared:\n%s", out)
	}
	if strings.Contains(out, "archive-reason") {
		t.Errorf("archive-reason should be cleared:\n%s", out)
	}
	// Existing unrelated fields preserved.
	if !strings.Contains(out, "name: example") {
		t.Errorf("unrelated field 'name' lost:\n%s", out)
	}
}

func TestRewriteFrontmatterFields_PreservesNonFrontmatterContent(t *testing.T) {
	src := `---
name: example
category: convention
severity: WARN
triggers: [cli]
---
# Heading

Some markdown body with --- inside that should NOT be treated as the closing fence.

More body.
`
	out, err := rewriteForArchive(src, "test", "2026-05-20T10:00:00Z")
	if err != nil {
		t.Fatalf("rewriteForArchive: %v", err)
	}
	if !strings.Contains(out, "# Heading") {
		t.Errorf("body heading lost:\n%s", out)
	}
	if !strings.Contains(out, "More body.") {
		t.Errorf("body trailing content lost:\n%s", out)
	}
}
