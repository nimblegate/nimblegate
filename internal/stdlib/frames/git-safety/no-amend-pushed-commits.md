---
name: no-amend-pushed-commits
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
    - git commit
pattern: shared-history-rewrite
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 2/2
  last-run: 2026-05-20T15:18:55Z
---

# No amend on pushed commits

Block `git commit --amend` when the commit being amended has already
been pushed to a remote. Amending a pushed commit rewrites the SHA,
which means every collaborator who pulled the original now has a
divergent local branch - the next push then either fails (good) or
needs a force-push (catastrophic, see also: `git-safety/no-force-push-main`).

This is the canonical "I broke main and tried to fix it with --amend"
incident. The fix is straightforward at the wrap layer: refuse the
amend when HEAD is reachable from any `origin/*` ref.

## BLOCK condition

All four must hold:

1. The command is `git commit ... --amend ...`
2. The repo has a remote named `origin`
3. `git rev-parse HEAD` succeeds (not a detached or empty repo)
4. `git branch -r --contains HEAD` lists at least one `origin/*` branch

If any of those fail, the check returns PASS (the amend is either
unsafe to evaluate or safe to perform). False-positive risk is
intentionally biased toward "let it through" - the legitimate cases
(amending an unpushed commit) are common and harmless.

## Failure message

```
❌ git-safety/no-amend-pushed-commits (git-safety) - HEAD (abc1234)
   is already on origin/main, origin/release - amending rewrites
   history other collaborators have pulled
   fix: either don't amend, or `git commit --fixup HEAD` + interactive
        rebase locally only; for an audited bypass:
        `nimblegate git --force-yes --reason="..." commit --amend`
```

## Override

The shell wrapper accepts `--force-yes --reason="..."` on `nimblegate
git`, which records the override in the audit log. This is the
right escape hatch for the rare case where amending is correct
(e.g., a solo-author branch that no one else has pulled yet).

No in-source disable marker exists - this check runs on the command
invocation, not on file content.

## What's NOT detected

- **Amend without push history** (the common, safe case): if
  `git branch -r --contains HEAD` returns no `origin/*` entries,
  this check returns PASS.
- **Non-`origin` remotes**: detection is anchored on `origin/`. A
  fork-style workflow where the upstream is `upstream/` rather than
  `origin/` will produce false PASS results. Multi-remote support
  is a future revision.
- **Rebase + force-push to non-protected branches**: the destructive
  history rewrite is covered by `git-safety/no-force-push-main` for
  protected branches; other branches are intentionally lower-friction.
- **Detached HEAD or empty repo**: short-circuits to PASS (the amend
  command itself would error out before doing damage).
