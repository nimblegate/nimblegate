---
name: no-zero-width-in-source
category: security
subcategory: invisible-payload
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
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
pattern: invisible-payload
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No zero-width characters in source files

Reject source files containing zero-width Unicode characters:

| Codepoint | Name |
|---|---|
| U+200B | ZWSP - zero-width space |
| U+200C | ZWNJ - zero-width non-joiner |
| U+200D | ZWJ - zero-width joiner |
| U+FEFF | BOM / zero-width no-break space (when NOT at position 0) |

These render invisibly yet count as identifier characters in some
parsers - `varname` and `var‌name` (containing a ZWNJ) are different
symbols. Attack shape: a function whose name *looks* like one already
in scope but resolves to a different definition the attacker controls.
AI-pasted snippets are a common delivery channel.

A leading U+FEFF (file position 0) is a UTF-8 BOM and is handled by
the separate `encoding/no-bom` frame - this frame ignores position-0
BOMs to avoid double-reporting.

## Fix

Delete the offending character. On the command line:

```
LC_ALL=C grep -P '[\x{200B}-\x{200D}\x{FEFF}]' <file>
```

If your codebase legitimately needs zero-width joiners (rare -
Indic / Arabic identifier support), suppress at the file level:

```
# appframes:disable security/no-zero-width-in-source
```
