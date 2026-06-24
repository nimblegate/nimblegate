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
  positives: 1/1
  negatives: 1/1
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
