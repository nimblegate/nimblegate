---
name: no-innerHTML-user-input
category: security
subcategory: content-safety
platform: []
framework: []
severity: WARN
tier: 2
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.js"
    - "**/*.mjs"
    - "**/*.ts"
    - "**/*.html"
pattern: unsanitized-data-flow
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-05-20T14:37:33Z
---

# No innerHTML for user input

Detect `element.innerHTML = <expression>` patterns in JavaScript / TypeScript /
HTML files. innerHTML assignment of untrusted input is the most common XSS
vector. Use `textContent` for text, or a sanitized DOM API for HTML fragments.

## Detection

Regex: `\.innerHTML\s*=\s*[^"'` + "`" + `]` (i.e., innerHTML assignment of anything that
isn't a string literal). Conservative: false positives on `el.innerHTML = ''`
are acceptable.

## Override

Per-file: `<!-- appframes:disable security/no-innerHTML-user-input -->` at the
top of the file (after the doctype if HTML). Per-line: `// appframes:disable-next-line`.
