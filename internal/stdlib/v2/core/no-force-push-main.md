---
name: no-force-push-main
category: git
subcategory: history-integrity
platform: []
framework: []
severity: BLOCK
tier: 1
triggers:
  - git-wrap
applies-to:
  commands:
    - git push
pattern: shared-history-rewrite
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:56:32Z
---

# No force-push to main

Block `git push --force` (or `-f`) against protected branches (main, master,
trunk, release/*). This is the most common irreversible destruction of public
history.

## When it fires

When `git push --force` (or `-f`, or `--force-with-lease` without an explicit
target) is invoked from any folder, and the resolved target branch is in the
protected list.

## Override

For deliberate history rewrites (cleanup of accidentally-pushed secrets, branch
rebase before merge), use `nimblegate git --force-yes --reason="..." push --force ...`.
