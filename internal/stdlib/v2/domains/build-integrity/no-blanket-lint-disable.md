---
name: no-blanket-lint-disable
category: app-correctness
subcategory: build-integrity
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.js"
    - "**/*.ts"
    - "**/*.tsx"
    - "**/*.py"
    - "**/*.go"
    - "**/*.rs"
    - "**/*.java"
    - "**/*.kt"
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 7/7
  negatives: 5/5
  last-run: 2026-09-27T16:23:22Z
---

# Linters must not be switched off wholesale

Flags linters and type checkers turned off for a whole file or crate:

- `/* eslint-disable */` with no rule named
- `// @ts-nocheck`
- `# ruff: noqa` / `# flake8: noqa` with no code named
- `# pylint: skip-file`, `# pylint: disable=all`, `# mypy: ignore-errors`
- `//nolint` without naming a linter (Go)
- `#![allow(warnings)]`, `#![allow(clippy::all)]` (Rust)
- `@SuppressWarnings("all")` (Java/Kotlin)

Disabling one named rule on one line (`// eslint-disable-next-line
no-console`, `# noqa: E501`, `//nolint:errcheck`) is fine and not flagged.

Why it matters with coding agents: silencing the linter for the whole file
is the quickest way past a lint failure, and it hides every future finding in
that file too.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every unmarked case present, not only new ones; after a one-time
pass to fix or mark the existing ones, each warning is a new shortcut.

## Fix

Fix the findings, or disable only the named rule on the line that needs
it. If a whole-file disable is intended (generated code, a vendored file),
record why: a whitelist entry with a reason, or
`appframes:disable-next-line app-correctness/no-blanket-lint-disable` in a
comment above it.
