---
id: invisible-chars
description: Zero-width Unicode runes in prose / documentation that break word-count, search, and diff readability.
anticipated-siblings: []
---

# Pattern: invisible-chars

The docs-side companion to `invisible-payload`. Zero-width spaces
(U+200B), zero-width non-joiners (U+200C), zero-width joiners
(U+200D), and zero-width no-break spaces (U+FEFF, when not at file
position 0) render as nothing in prose but:

- Word-count tools mis-count.
- `grep` / browser search miss the highlighted word.
- The visible diff looks clean while the actual byte diff includes
  the smuggled rune.

Distinct from `invisible-payload` (which is the attack surface in
SOURCE code, where an identifier can be forged): this pattern
addresses the same characters in PROSE files (README, LICENSE,
CHANGELOG, .md, .txt) where the issue is readability and i18n
shaping. Severity is WARN rather than BLOCK because some i18n
libraries legitimately use ZWJ for Indic / Arabic shaping in
user-facing strings.
