---
name: no-placeholder-tests
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
  positives: 8/8
  negatives: 5/5
  last-run: 2026-09-27T16:51:16Z
---

# Tests must be able to fail

Flags tests in test files that cannot fail:

- **Empty bodies** written on one line: `it('x', () => {})`,
  `test("x", async () => {})`, `def test_x(): pass`, `func TestX(t *testing.T) {}`,
  RSpec `it 'x' do end`.
- **Assertions on constants:** `expect(true).toBe(true)`, `expect(1).toBe(1)`,
  `assert(true)`, `assert True`, `self.assertTrue(True)`, `assert!(true)`,
  `assertTrue(true)`, `Assert.True(true)`.

A multi-line empty body is not detected; the one-line forms are what
placeholder tests usually look like. Commented-out lines are ignored.

Why it matters with coding agents: a placeholder test turns "add tests" into a
green checkmark and a higher test count, with nothing checked.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every case present, not only new ones.

## Fix

Write the test's real assertion, or delete the placeholder. If one is
intentional (a smoke test that only checks the module loads), record why: a
whitelist entry with a reason, or
`appframes:disable-next-line app-correctness/no-placeholder-tests` in a comment
above it.
