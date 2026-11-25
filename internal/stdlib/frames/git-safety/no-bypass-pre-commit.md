---
name: no-bypass-pre-commit
category: git
subcategory: gate-integrity
platform: []
framework: []
severity: BLOCK
tier: 1
tags: [pre-commit, bypass-prevention, command-parse]
triggers: [git-wrap, cli]
applies-to:
  commands:
    - "git commit"
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:56:32Z
---

# Commits must not skip hooks with --no-verify

Reject `git commit --no-verify` and every other spelling git accepts for it: the short form `-n`, alone or bundled (`-nm "msg"`), abbreviations such as `--no-veri`, global options before the verb (`git -C dir commit -n`), and `git -c core.hooksPath=... commit`, which points git at an empty hooks directory. The command is read the way git reads it, so a message or pathspec that happens to contain `-n` is not flagged. `--no-verify` is git's built-in mechanism to skip the pre-commit hook - which is exactly how nimblegate runs its pre-commit-trigger frames. Allowing silent bypass defeats the load-bearing guarantee.

## Why this exists

In any agent-driven or human-driven workflow, the pre-commit hook is the *one* gate that fires regardless of how the commit happens - IDE, terminal, agent Bash tool, automation script. `--no-verify` is the universal escape hatch that skips it. Without this frame, anyone (or anything) who knows about the flag can route around every nimblegate gate by just typing it.

The shim at `~/.appframes/shims/git` routes `git commit` through nimblegate when any argument could skip the hook (`-n`, a bundle containing `n`, `--no-v...`, or a global `-c`); this frame then parses the command and BLOCKs the ones that do. After upgrading, run `nimblegate shell install --strict` again so the shim has the current rules. Together they close the silent-bypass route:

| Path | Without this frame | With this frame |
|------|--------------------|-----------------|
| `git commit --no-verify` from interactive shell | Skips hook silently | Shim catches → frame BLOCKs |
| `git commit --no-verify` from agent Bash tool | Skips hook silently | Shim catches → frame BLOCKs |
| `git commit --no-verify` from IDE | Skips hook silently | Shim catches → frame BLOCKs |
| `git commit -nm "msg"`, `--no-veri`, `git -C dir commit -n` | Skips hook silently | Shim catches → frame BLOCKs |
| `git -c core.hooksPath=/dev/null commit` | Skips hook silently | Shim catches → frame BLOCKs |
| `/usr/bin/git commit --no-verify` (absolute path) | Skips hook | Skips hook AND shim (operator-only action; left as bypass-with-evidence) |

## What still works around it (deliberately)

The frame does not (and cannot) prevent:

- `/usr/bin/git commit --no-verify` - absolute path skips the shim. This is operator-only action; the resulting commit exists in git history without an audit-log entry, so the gap is detectable post-facto.
- Removing the shim from `~/.appframes/shims/` or `nimblegate shell uninstall --strict` - also operator action; leaves visible state.
- Compromising the nimblegate binary itself - outside the threat model.

The point is **eliminating silent bypass for normal command shapes**. Deliberate, recorded bypass is permitted via `--force-yes`.

## Fix

Remove `--no-verify` from your commit command. The pre-commit hook will fire, nimblegate will run its checks, and the commit will proceed if checks pass:

```bash
git commit -m "your message"
```

If the bypass is genuinely needed for this one commit (the hook is broken in CI, an emergency rollback, a frame is wrong and you need to commit while fixing it), record it explicitly:

```bash
nimblegate git --force-yes --reason="emergency rollback - frame X is wrong, fixing in next PR" commit --no-verify -m "..."
```

This:
- Records the bypass in `.appframes/audit.log` with the reason
- Shows up in `nimblegate audit analyze` reports
- Will be clustered by `audit analyze` if the same reason text appears repeatedly (vague reasons like "test" or "fix" get flagged as suspect)

## Suppressing intentional cases

Frame-level disable in `appframes.toml` (records the decision in the project's diff):

```toml
[frames.git-safety.no-bypass-pre-commit]
enabled = false
```

This is a deliberate policy choice (and committed to the repo's history) - the right place to record "this project accepts --no-verify."

## Composes with

- `git-safety/folder-branch-lock` - prevents commits from the wrong working folder
- `git-safety/no-amend-pushed-commits` - prevents history rewrites of pushed commits
- `git-safety/no-force-push-main` - prevents catastrophic force-pushes
- All four together cover the most common git-side escape hatches.
