---
name: no-test-special-casing
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
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 4/4
  negatives: 5/5
  last-run: 2026-09-27T16:51:16Z
---

# Production code must not detect test runs

Flags code outside test files that checks whether it is running under
test:

- **JS/TS:** `process.env.NODE_ENV === 'test'` (or `!==`),
  `process.env.JEST_WORKER_ID`, `process.env.VITEST`, `import.meta.env.VITEST`,
  `typeof jest !== 'undefined'`
- **Python:** `'pytest' in sys.modules`, `PYTEST_CURRENT_TEST`
- **Go:** `testing.Testing()`, `flag.Lookup("test.v")`, a `.test` suffix check
  on `os.Args[0]`

Test files, `conftest.py`, `setupTests*` and `*.config.*` files are not
scanned. Commented-out lines are ignored.

Why it matters with coding agents: making the code take a different path under
test is a way to turn a failing test green without fixing the code it tests.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every case present, not only new ones.

## Fix

Make the code correct in both cases and inject the difference from the
test (a parameter, a fake, a config value). If a branch is intended (quieter
logging under test), record why: a whitelist entry with a reason, or
`appframes:disable-next-line app-correctness/no-test-special-casing` in a
comment above it.
