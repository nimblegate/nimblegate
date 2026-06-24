---
name: no-homoglyph-identifiers
category: security
subcategory: identifier-confusable
platform: []
framework: []
severity: WARN
severity-source: frame
tier: 2
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.py"
    - "**/*.js"
    - "**/*.ts"
    - "**/*.jsx"
    - "**/*.tsx"
    - "**/*.mjs"
    - "**/*.cjs"
    - "**/*.go"
    - "**/*.rs"
    - "**/*.c"
    - "**/*.cpp"
    - "**/*.h"
    - "**/*.hpp"
    - "**/*.java"
    - "**/*.kt"
    - "**/*.swift"
    - "**/*.rb"
    - "**/*.php"
    - "**/*.sh"
    - "**/*.bash"
    - "**/*.zsh"
pattern: identifier-confusable
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No homoglyph (Latin-confusable) characters in source

Warn when source files contain Cyrillic or Greek letters that look
identical to Latin letters in most fonts. Attack shape: a function
or variable named `аdmin` (Cyrillic а, U+0430) sits beside the real
`admin` (Latin a, U+0061) - they render identically but bind to
different symbols.

This frame ships at **WARN**, not BLOCK, because legitimate i18n
projects (Cyrillic / Greek strings in comments, translation tables)
will trigger false positives. The warning surfaces the risk; the
user decides per file whether it's intentional.

## What this catches

The following Cyrillic and Greek runes that are visually
indistinguishable from common Latin letters in monospace fonts:

| Codepoint | Char | Confusable with |
|---|---|---|
| U+0430 | а | Latin a |
| U+0435 | е | Latin e |
| U+043E | о | Latin o |
| U+0440 | р | Latin p |
| U+0441 | с | Latin c |
| U+0443 | у | Latin y |
| U+0445 | х | Latin x |
| U+0410 | А | Latin A |
| U+0415 | Е | Latin E |
| U+041E | О | Latin O |
| U+0420 | Р | Latin P |
| U+0421 | С | Latin C |
| U+03B1 | α | Latin a |
| U+03BF | ο | Latin o |
| U+03C1 | ρ | Latin p |
| U+03C5 | υ | Latin u |
| U+0391 | Α | Latin A |
| U+039F | Ο | Latin O |
| U+03A1 | Ρ | Latin P |

The set is intentionally tight - the most common attack-shape
substitutions, not the full UTR #39 confusables list.

## Fix

Replace with the Latin equivalent if the homoglyph was unintentional.
On the command line:

```
LC_ALL=C grep -P '[\x{0410}-\x{042F}\x{0430}-\x{044F}\x{0391}-\x{03A9}\x{03B1}-\x{03C9}]' <file>
```

If the file legitimately contains Cyrillic / Greek (translation table,
i18n string literals), suppress at the file level:

```
# appframes:disable security/no-homoglyph-identifiers
```

## Reference

- Unicode Technical Standard #39 (Security Mechanisms)
- "IDN homograph attack" (Wikipedia)
