---
id: identifier-confusable
description: Non-Latin characters that render identically to Latin letters in monospace fonts and can forge identifiers that look like real ones.
anticipated-siblings: []
---

# Pattern: identifier-confusable

Cyrillic `а` (U+0430) and Latin `a` (U+0061) render identically in
most fonts but are different codepoints - and bind to different
symbols. A function named `аdmin` (Cyrillic а) compiled into the same
binary as `admin` (Latin a) is two distinct entry points; the human
reviewer typically cannot tell them apart.

The structural defense is to detect non-Latin runes that visually
collide with Latin letters in source files and surface them for
review. Some projects legitimately use non-Latin identifiers (i18n,
translation tables), so the gate is WARN rather than BLOCK - the
finding documents the risk while leaving per-file judgment to the
user.

Distinct from `invisible-payload`: confusables are visible (they
render to a glyph), they just render to the wrong glyph.
