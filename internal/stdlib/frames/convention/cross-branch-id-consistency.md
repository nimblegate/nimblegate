---
name: cross-branch-id-consistency
category: documentation
subcategory: branch-consistency
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.html"
    - "**/*.md"
canonical-refs:
  - website-ids.toml
pattern: same-thing-different-name
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 2/2
  last-run: 2026-05-20T15:18:55Z
---

# Cross-branch ID consistency

When a project uses canonical ID tables (analytics website IDs, API token tags,
external service identifiers), files in different folders/branches must
reference IDs that match the canonical table - not hardcoded copies that
drift.

## When it fires

Scans files matching `applies-to.files` for ID patterns declared in
`website-ids.toml`. Flags any hardcoded ID that doesn't match the canonical
table's entry for the file's location (folder → domain → expected ID).

## Override

If a file deliberately uses a different ID (e.g., dev vs prod environments):
add `<!-- appframes:disable convention/cross-branch-id-consistency -->` at
the top.
