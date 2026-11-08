---
name: no-mixed-indent
category: encoding
subcategory: indent
platform: []
framework: []
severity: BLOCK
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
    - "**/*.mk"
    - "**/Makefile"
    - "**/makefile"
pattern: indent
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-11-08T14:37:12Z
---

# No mixed tab + space indentation

Block lines whose leading whitespace contains **both** tabs and
spaces. The two render identically in many editors but bind
differently to whatever consumes the file:

- Python raises `TabError: inconsistent use of tabs and spaces`
- `make` requires literal-tab recipe lines and silently drops
  rules whose indent contains a space prefix
- Go gofmt rewrites the indent, producing huge unrelated diffs
- ESLint / Prettier disagree about which to keep, causing CI flap

## Fix

Pick one (project-wide). Run `gofmt`, `black`, `prettier --tab-width`,
or `expand`/`unexpand` to normalize.

For Makefiles specifically: recipe lines must start with a literal
tab - but the *first character* must be a tab, not a space followed
by a tab.

## Never fires

- **Go raw string literals and block comments.** A line that starts inside
  a backtick string (embedded SQL, JSON) or a `/* */` comment holds content,
  not indentation; gofmt normalises every code line to tabs and leaves
  literals alone. Mixed indentation on Go code lines still fires.
