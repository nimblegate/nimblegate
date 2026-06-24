---
name: folder-branch-lock
category: git
subcategory: branch-discipline
platform: []
framework: []
severity: WARN
tier: 1
triggers:
  - git-wrap
  - pre-commit
applies-to:
  commands:
    - git commit
    - git push
canonical-refs:
  - folder-branch-map.toml
pattern: wrong-context-execution
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 2/2
  last-run: 2026-05-20T15:18:55Z
---

# Folder-to-branch lock

Multi-folder repos with one branch per folder (orphan-branch pattern) are
trivially easy to commit/push from the wrong folder. This frame verifies the
current working directory matches the expected branch declared in
`.appframes/_canonical/folder-branch-map.toml`.

## When it fires

On `git commit` and `git push`, if the current working directory (relative to
the canonical table's root) does not have an expected-branch entry that
matches the current `git branch --show-current`.

## Failure message

```
❌ folder-branch-lock (git-safety)
   You're in `infra/` but the current branch is `landing`.
   Expected branch for `infra/`: `infra`
   Quick fix: cd to the correct folder for this branch before committing/pushing.
```

## Remediation

`cd` into the folder that matches the branch you intend to push, then retry.
Or check `.appframes/_canonical/folder-branch-map.toml` if the mapping is
out of date.

## Override

For one-off intentional cross-folder commits:
```
nimblegate git --force-yes --reason="<why>" commit -m "..."
```
The override is recorded to the audit log with the reason.
