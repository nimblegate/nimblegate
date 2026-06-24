---
name: no-bidi-override
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
    - "**/*"
pattern: invisible-payload
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No Unicode bidirectional override

Reject files containing Unicode bidirectional override characters. These
reverse the visual rendering of code so the human eye sees one
control-flow path while the compiler / interpreter executes another.

Typical failure shape: `if (admin)` rendered to the reader, executed as
`if (!admin)`. CVE-2021-42574 ("Trojan Source"); affects every language
that allows Unicode in source files. AI-pasted code is a primary vector
because the override characters survive clipboard copy without showing
in most editors.

## What this catches

Presence of any of the following codepoints:

| Codepoint | Name |
|---|---|
| U+202A | LRE - left-to-right embedding |
| U+202B | RLE - right-to-left embedding |
| U+202C | PDF - pop directional formatting |
| U+202D | LRO - left-to-right override |
| U+202E | RLO - right-to-left override |
| U+2066 | LRI - left-to-right isolate |
| U+2067 | RLI - right-to-left isolate |
| U+2068 | FSI - first strong isolate |
| U+2069 | PDI - pop directional isolate |

## Fix

Remove the offending character. Most editors with "show hidden characters"
or "render bidirectional control" surface the offending position; on the
command line `LC_ALL=C grep -P '[\x{202A}-\x{202E}\x{2066}-\x{2069}]' <file>`
locates lines.

If you legitimately need bidirectional override (a real RTL-language
codebase mixing scripts), suppress at the file level:

```
# appframes:disable security/no-bidi-override
# (this file intentionally mixes LTR / RTL - Arabic translation table)
```

## Reference

- CVE-2021-42574 - https://trojansource.codes/
- Unicode Technical Standard #39 (Unicode Security Mechanisms)
