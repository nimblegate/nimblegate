// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"strings"
	"testing"
)

func TestParse_ValidFrame(t *testing.T) {
	input := `---
name: folder-branch-lock
category: git
subcategory: branch-discipline
severity: BLOCK
triggers:
  - git-wrap
  - pre-commit
applies-to:
  commands:
    - git commit
    - git push
canonical-refs:
  - folder-branch-map.toml
---

# Folder-to-branch lock

Catches commit/push from wrong folder in multi-branch repos.
`

	f, err := Parse(strings.NewReader(input), "test://folder-branch-lock.md")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if f.Frontmatter.Name != "folder-branch-lock" {
		t.Errorf("Name = %q, want %q", f.Frontmatter.Name, "folder-branch-lock")
	}
	if f.Frontmatter.Category != CategoryGitSafety {
		t.Errorf("Category = %q, want %q", f.Frontmatter.Category, CategoryGitSafety)
	}
	if f.Frontmatter.Severity != SeverityBlock {
		t.Errorf("Severity = %q, want %q", f.Frontmatter.Severity, SeverityBlock)
	}
	if len(f.Frontmatter.Triggers) != 2 || f.Frontmatter.Triggers[0] != "git-wrap" {
		t.Errorf("Triggers = %v, want [git-wrap pre-commit]", f.Frontmatter.Triggers)
	}
	if len(f.Frontmatter.AppliesTo.Commands) != 2 {
		t.Errorf("AppliesTo.Commands = %v, want 2 entries", f.Frontmatter.AppliesTo.Commands)
	}
	if len(f.Frontmatter.CanonicalRefs) != 1 || f.Frontmatter.CanonicalRefs[0] != "folder-branch-map.toml" {
		t.Errorf("CanonicalRefs = %v, want [folder-branch-map.toml]", f.Frontmatter.CanonicalRefs)
	}
	if !strings.Contains(f.Body, "Folder-to-branch lock") {
		t.Errorf("Body missing expected content; got: %q", f.Body)
	}
	if f.SourcePath != "test://folder-branch-lock.md" {
		t.Errorf("SourcePath = %q", f.SourcePath)
	}
}

func TestParse_MissingFrontmatter(t *testing.T) {
	input := `# Just a heading, no frontmatter at all.`
	_, err := Parse(strings.NewReader(input), "test://no-fm.md")
	if err == nil {
		t.Fatal("Parse() expected error for missing frontmatter, got nil")
	}
}

func TestParse_UnclosedFrontmatter(t *testing.T) {
	input := `---
name: unclosed
category: git-safety
severity: BLOCK

body here without closing fence
`
	_, err := Parse(strings.NewReader(input), "test://unclosed.md")
	if err == nil {
		t.Fatal("Parse() expected error for unclosed frontmatter, got nil")
	}
}

func TestParse_MissingRequiredField(t *testing.T) {
	input := `---
name: missing-category
severity: BLOCK
triggers: [cli]
---
body
`
	_, err := Parse(strings.NewReader(input), "test://missing-cat.md")
	if err == nil {
		t.Fatal("Parse() expected error for missing category, got nil")
	}
}

func TestParse_InvalidCategory(t *testing.T) {
	input := `---
name: bad-cat
category: not-a-real-category
severity: BLOCK
triggers: [cli]
---
body
`
	_, err := Parse(strings.NewReader(input), "test://bad-cat.md")
	if err == nil {
		t.Fatal("Parse() expected error for invalid category, got nil")
	}
}

func TestParse_InvalidSeverity(t *testing.T) {
	input := `---
name: bad-sev
category: security
severity: MAYBE
triggers: [cli]
---
body
`
	_, err := Parse(strings.NewReader(input), "test://bad-sev.md")
	if err == nil {
		t.Fatal("Parse() expected error for invalid severity, got nil")
	}
}

// V0.5 metadata extensions: tier, tags, dedup-key, runs-after.

func TestParse_OptionalFieldsAbsent(t *testing.T) {
	// Frame without any of the new V0.5 fields must still parse - existing
	// project frames and pre-V0.5 stdlib frames depend on this. Defaults:
	// Tier=0 (EffectiveTier=3), Tags=nil, DedupKey="", RunsAfter=nil.
	input := `---
name: no-extras
category: documentation
subcategory: todo-discipline
severity: WARN
triggers: [cli]
---
body
`
	f, err := Parse(strings.NewReader(input), "test://no-extras.md")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if f.Frontmatter.Tier != 0 {
		t.Errorf("Tier = %d, want 0 (absent)", f.Frontmatter.Tier)
	}
	if f.Frontmatter.EffectiveTier() != 3 {
		t.Errorf("EffectiveTier() = %d, want 3 (default)", f.Frontmatter.EffectiveTier())
	}
	if f.Frontmatter.Tags != nil {
		t.Errorf("Tags = %v, want nil", f.Frontmatter.Tags)
	}
	if f.Frontmatter.DedupKey != "" {
		t.Errorf("DedupKey = %q, want \"\"", f.Frontmatter.DedupKey)
	}
	if f.Frontmatter.RunsAfter != nil {
		t.Errorf("RunsAfter = %v, want nil", f.Frontmatter.RunsAfter)
	}
}

func TestParse_OptionalFieldsPresent(t *testing.T) {
	input := `---
name: full-meta
category: security
subcategory: credentials
severity: BLOCK
triggers: [cli]
tier: 1
tags: [secrets, supply-chain]
dedup-key: file:line
runs-after:
  - security/no-private-keys-in-repo
  - git/folder-branch-lock
---
body
`
	f, err := Parse(strings.NewReader(input), "test://full.md")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if f.Frontmatter.Tier != 1 {
		t.Errorf("Tier = %d, want 1", f.Frontmatter.Tier)
	}
	if f.Frontmatter.EffectiveTier() != 1 {
		t.Errorf("EffectiveTier() = %d, want 1", f.Frontmatter.EffectiveTier())
	}
	if len(f.Frontmatter.Tags) != 2 || f.Frontmatter.Tags[0] != "secrets" {
		t.Errorf("Tags = %v, want [secrets supply-chain]", f.Frontmatter.Tags)
	}
	if f.Frontmatter.DedupKey != "file:line" {
		t.Errorf("DedupKey = %q, want \"file:line\"", f.Frontmatter.DedupKey)
	}
	if len(f.Frontmatter.RunsAfter) != 2 {
		t.Errorf("RunsAfter = %v, want 2 entries", f.Frontmatter.RunsAfter)
	}
}

func TestParse_InvalidTier(t *testing.T) {
	for _, bad := range []int{-1, 7, 99} {
		input := `---
name: bad-tier
category: security
severity: WARN
triggers: [cli]
tier: ` + intStr(bad) + `
---
body
`
		_, err := Parse(strings.NewReader(input), "test://bad-tier.md")
		if err == nil {
			t.Errorf("Parse() with tier=%d expected error, got nil", bad)
		}
	}
}

func TestParse_TierZeroAllowed(t *testing.T) {
	// tier: 0 in YAML means "field omitted" (zero value). Must parse.
	input := `---
name: tier-zero
category: documentation
subcategory: todo-discipline
severity: INFO
triggers: [cli]
tier: 0
---
body
`
	if _, err := Parse(strings.NewReader(input), "test://t0.md"); err != nil {
		t.Fatalf("Parse() with tier:0 expected success, got %v", err)
	}
}

func TestParse_InvalidDedupKey(t *testing.T) {
	input := `---
name: bad-dedup
category: security
severity: WARN
triggers: [cli]
dedup-key: every-other-tuesday
---
body
`
	_, err := Parse(strings.NewReader(input), "test://bad-dedup.md")
	if err == nil {
		t.Fatal("Parse() expected error for invalid dedup-key, got nil")
	}
}

func TestParse_DedupKeyValidValues(t *testing.T) {
	for _, v := range []string{"file", "file:line"} {
		input := `---
name: ok-dedup
category: security
subcategory: credentials
severity: WARN
triggers: [cli]
dedup-key: ` + v + `
---
body
`
		if _, err := Parse(strings.NewReader(input), "test://"+v+".md"); err != nil {
			t.Errorf("Parse() with dedup-key=%q expected success, got %v", v, err)
		}
	}
}

// intStr is a tiny strconv.Itoa replacement so this test file doesn't pull
// in strconv just for one call site.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
