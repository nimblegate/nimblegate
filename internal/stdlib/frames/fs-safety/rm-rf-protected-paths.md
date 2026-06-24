---
name: rm-rf-protected-paths
category: filesystem
subcategory: destructive-paths
platform: []
framework: []
severity: BLOCK
tier: 1
triggers:
  - git-wrap
applies-to:
  commands:
    - rm
canonical-refs:
  - protected-paths.toml
pattern: destructive-on-protected-resource
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:56:32Z
---

# rm -rf - protect catastrophic paths

Block recursive `rm` invocations that target paths from which recovery
is hard or impossible: the filesystem root, the user's home directory,
core OS trees (`/etc`, `/usr`, `/var`, `/bin`, `/sbin`, `/lib`,
`/lib64`, `/boot`, `/sys`, `/proc`), and the project root itself when
that's something the developer doesn't want destroyed.

Two real-world incident classes this prevents:

1. **Explicit catastrophic command.** `rm -rf /` because someone typo'd
   or pasted the wrong thing. Modern `rm` has `--preserve-root` by
   default for `/` specifically, but not for `/etc`, `/var`, `/usr`,
   etc. - they go quietly.

2. **Unexpanded variable expansion.** The famous Steam-runtime bug:
   ```
   rm -rf "$STEAMROOT/"*
   ```
   If `$STEAMROOT` is undefined, this becomes `rm -rf /*`. We detect
   shell-variable expansions with a missing or empty value sentinel.

## Trigger + detection

`git-wrap` - fires when `nimblegate cmd rm …` is invoked through the
shell wrapper. The wrapper routes `rm` through nimblegate ONLY when a
recursive flag (`-r`, `-R`, `-rf`, `-fr`, `--recursive`) is present;
non-recursive `rm` doesn't reach this check (zero overhead on the
common case).

The check parses `ctx.Command`, looks at each argument that isn't a
flag, and BLOCKs if any of:

- The argument equals a protected path (`/`, `/etc`, `/usr`, …)
- The argument starts with a protected-path prefix followed by `/`
  (so `/etc/foo` is blocked, not just bare `/etc`)
- The argument is `$HOME`, `~`, or `~/` (your home directory)
- The argument contains an unexpanded shell variable that resolves
  to empty (`""/`, `${VAR}/` where the engine sees no value, etc.) -
  this is the "Steam bug" pattern

The default protected-path catalog is built in:

```
/  /bin  /boot  /dev  /etc  /home  /lib  /lib32  /lib64  /opt
/proc  /root  /run  /sbin  /srv  /sys  /usr  /var
```

Plus `$HOME`, `~`, `~/`.

## Customizing the catalog

A project that wants additional protected paths (e.g., the project
root itself) can list them in
`.appframes/_canonical/protected-paths.toml`:

```toml
# Project-specific paths the rm-rf protector must never touch.
[paths]
"/srv/projects/critical-data" = "production data - never delete"
"/home/me/myproject"            = "main project root"
```

Keys are absolute paths; values are human-readable reasons. The
project entries augment (not replace) the built-in catalog.

## Failure message

```
❌ fs-safety/rm-rf-protected-paths (fs-safety)
   rm -rf would target protected path "/etc/nginx" (reason: OS config tree)
   fix: if you really mean it, use `nimblegate cmd --force-yes
        --reason="<why>" rm -rf /etc/nginx` to record an audited bypass
```

## Override

`nimblegate cmd --force-yes --reason="<why>" rm -rf <path>` - same
override path as every other git-wrap frame. The reason is recorded
in the audit log along with the full command.

There's deliberately NO per-file disable for this frame because the
detection target is the COMMAND, not a file in the repo.

## What's NOT detected

- **Non-recursive rm.** A bare `rm /etc/something` deletes one file
  and would error if it tried to descend, so the destruction risk is
  bounded. We focus on the recursive-flag cases.
- **rmdir.** Empty-directory removal is non-destructive almost by
  definition.
- **find … -delete.** A `find / -name '*.tmp' -delete` could be just
  as catastrophic but requires a separate frame (`find-delete-protected-paths`,
  V0.6+ candidate). The shell wrapper doesn't currently intercept `find`.
- **Indirect destructive commands.** `dd of=/dev/sda`, `mkfs.ext4 /dev/sda`,
  `shred /dev/sda`. Different frames for different tools.
- **Container/VM contexts** where `/` is intended (e.g. cleaning a
  Docker build stage). Use `--force-yes` with a reason; the audit log
  records the intent.
