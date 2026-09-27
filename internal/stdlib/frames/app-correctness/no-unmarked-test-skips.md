---
name: no-unmarked-test-skips
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
    - "**/*.test.*"
    - "**/*.spec.*"
    - "**/*_test.go"
    - "**/test_*.py"
    - "**/*_test.py"
    - "**/*.rs"
    - "**/*Test.java"
    - "**/*_spec.rb"
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 9/9
  negatives: 5/5
  last-run: 2026-09-27T16:23:22Z
---

# Skipped and focused tests must be marked as intended

Flags tests that are switched off or that switch the rest of the suite off:

- **Skipped:** `it.skip`, `test.skip`, `describe.skip`, `xit`, `xdescribe`
  (JS/TS); `@pytest.mark.skip`, `@unittest.skip`, `pytest.skip()`,
  `self.skipTest()` (Python); `t.Skip()` (Go); `#[ignore]` (Rust);
  `@Disabled`, `@Ignore` (Java/Kotlin); `Skip =`, `[Ignore]` (C#); `xit`,
  `skip` (RSpec).
- **Focused:** `.only`, `fit`, `fdescribe`. Only the focused tests run, so
  every other failure goes unseen.

Conditional skips (`skipif`, `skipIf`, `skipUnless`) are not flagged: they
state their condition. Only test files are scanned (`*.test.*`, `*.spec.*`,
`*_test.go`, `test_*.py`, files under `test/`, `tests/`, `__tests__/`,
`spec/`, ...), plus `#[ignore]` in any Rust file. Commented-out lines are
ignored.

Why it matters with coding agents: skipping the failing test is the fastest
way to a green run, and the diff looks small.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every unmarked case present, not only new ones; after a one-time
pass to fix or mark the existing ones, each warning is a new shortcut.

## Fix

Fix the test and remove the skip. If the skip is intended, record why:
a whitelist entry with a reason (recorded in the audit log on every push), or
`// appframes:disable-next-line app-correctness/no-unmarked-test-skips` on the
line above.
