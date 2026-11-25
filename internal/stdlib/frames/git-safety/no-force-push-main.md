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

When `git push` forces and the target branch is in the protected list. Forcing
includes `--force`, `-f` alone or bundled (`-fu`), abbreviations (`--forc`),
`--force-with-lease`, `--force-if-includes`, and a `+` refspec (`+main`,
`+HEAD:main`); the target is read from the refspec's destination
(`HEAD:main`, `HEAD:refs/heads/main`). Global options before `push`
(`git -C dir push`) and git aliases (`alias.pf = "push --force"`) do not hide it.

## Override

For deliberate history rewrites (cleanup of accidentally-pushed secrets, branch
rebase before merge), use `nimblegate git --force-yes --reason="..." push --force ...`.
