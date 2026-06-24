---
name: no-non-printable
category: encoding
subcategory: control-chars
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
    - "**/*"
pattern: control-chars
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No non-printable control characters

Warn on C0 control characters (U+0000–U+001F, excluding tab / LF / CR)
and C1 control characters (U+0080–U+009F). These typically arrive
from raw paste of terminal output, screen capture, or copy-from-PDF;
they render invisibly but break diff / grep / search / JSON encoding.

A frequent source is the literal ESC byte (U+001B) leaked from ANSI
color codes when log output is pasted into source.

Severity WARN, not BLOCK, because rare binary fixtures legitimately
contain control bytes.

## Fix

Remove the offending bytes:

```
LC_ALL=C sed -i 's/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]//g' <file>
```

Or for the specific ESC byte from ANSI:

```
sed -i 's/\x1b\[[0-9;]*[A-Za-z]//g' <file>
```
