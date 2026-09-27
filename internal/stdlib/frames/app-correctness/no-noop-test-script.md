---
name: no-noop-test-script
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
    - "**/package.json"
    - "**/Makefile"
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 5/5
  last-run: 2026-09-27T16:51:16Z
---

# The test command must run tests

Flags a test command that runs nothing:

- a `package.json` `test` script of `exit 0`, `true`, or only `echo`
  (optionally followed by `&& exit 0`);
- a Makefile `test:` target with no prerequisites whose recipe only echoes,
  runs `true`, or exits 0.

npm's default `echo "Error: no test specified" && exit 1` is not flagged: it
fails, which is honest. A `test:` target with prerequisites
(`test: unit integration`) is not flagged either.

Why it matters with coding agents: when the tests fail, replacing the test
command with one that succeeds makes every CI run green.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every case present, not only new ones.

## Fix

Point the test command at the real test runner. A project with no tests
yet should let `test` fail rather than pass. If it is intended, record why in a
whitelist entry.
