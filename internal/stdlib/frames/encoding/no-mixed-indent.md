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
  negatives: 4/4
  last-run: 2026-11-08T14:50:12Z
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

- **Tabs then spaces in Makefiles and C-family files** (`.mk`, `Makefile`,
  `.c`, `.h`, `.cpp`, `.cc`, `.cxx`, `.hpp`). A recipe line must start with a
  tab and the spaces after it belong to the shell command; C code indents
  with tabs and aligns with spaces by convention. A space *before* a tab
  still fires there. Python, JS/TS, Rust and Go flag any mix.

- **Go raw string literals and block comments.** A line that starts inside
  a backtick string (embedded SQL, JSON) or a `/* */` comment holds content,
  not indentation; gofmt normalises every code line to tabs and leaves
  literals alone. Mixed indentation on Go code lines still fires.
