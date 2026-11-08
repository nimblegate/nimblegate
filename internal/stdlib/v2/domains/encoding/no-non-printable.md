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
  positives: 2/2
  negatives: 1/1
  last-run: 2026-11-08T14:33:57Z
---

# No non-printable control characters

Warn on C0 control characters (U+0000–U+001F, excluding tab / LF / CR)
and C1 control characters (U+0080–U+009F). These typically arrive
from raw paste of terminal output, screen capture, or copy-from-PDF;
they render invisibly but break diff / grep / search / JSON encoding.

A frequent source is the literal ESC byte (U+001B) leaked from ANSI
color codes when log output is pasted into source.

Binary files are skipped: known binary extensions (images, archives,
fonts, compressed `.br`/`.gz`, databases, NumPy/pickle data, office
documents), and any file whose first 8000 bytes hold a NUL byte (the
same rule git uses) or are largely control bytes / invalid UTF-8.
Minified bundles (`*.min.js`, `*.min.css`) are skipped too: their
embedded string tables are machine output, not the author's text.
Test-fixture and test-suite directories (`testdata/`, `fixtures/`,
`__fixtures__/`, `__snapshots__/`, `test/`, `tests/`, `__tests__/`,
`spec/`) are skipped: they hold deliberately odd input. Test source
files elsewhere (`foo_test.go`) are still scanned - write the byte as
an escape (`\x1b`) instead.

Severity WARN, not BLOCK, because rare text fixtures legitimately
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
