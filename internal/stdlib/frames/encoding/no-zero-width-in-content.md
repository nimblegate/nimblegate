---
name: no-zero-width-in-content
category: encoding
subcategory: invisible-chars
platform: []
framework: []
severity: WARN
severity-source: frame
tier: 3
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.md"
    - "**/*.markdown"
    - "**/*.txt"
    - "**/*.rst"
    - "**/README"
    - "**/README.*"
    - "**/LICENSE"
    - "**/LICENSE.*"
    - "**/CHANGELOG"
    - "**/CHANGELOG.*"
pattern: invisible-chars
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No zero-width characters in docs / prose

Warn on zero-width Unicode runes (U+200B/U+200C/U+200D/U+FEFF) in
documentation and prose files. These render invisibly, but:

- Word-count / readability tools mis-count
- Search / grep miss the highlighted word
- Diff renders fine but actual byte diff shows the smuggled rune

This is the docs-side companion to `security/no-zero-width-in-source`.
WARN (not BLOCK) because some i18n libraries legitimately use ZWJ for
Indic / Arabic shaping in user-facing strings.

Leading U+FEFF (BOM) at file position 0 is ignored - `encoding/no-bom`
owns that case.

## Fix

Strip the offending characters:

```
LC_ALL=C sed -i 's/[\xE2\x80\x8B-\xE2\x80\x8D]//g' <file>
```

or in editors with "show invisible characters" turned on, delete by hand.
