# Troubleshooting

Operator-facing gotchas and their fixes. Most are about the gap between what the
gateway **records** and what you see when you poke at it by hand: the gate can
be working perfectly while a manual check looks broken.

---

## `git push` succeeds but never reaches the gateway (no output, remote unchanged)

**Symptom.** `git push` prints nothing (or aborts quietly), and neither the
gateway's audit log nor the remote ref moves, yet the push appears to "work".

**Cause.** A stale **client-side shim**. If the dev machine ever ran
`nimblegate shell install` (the git-wrap adapter, see [adapters](adapters.md)),
it placed a wrapper at `~/.appframes/shims/git` and put that directory first on
`PATH`. The wrapper reroutes `push`/`reset`/`branch`/`rebase`/`stash` through
the local CLI, which runs a *local* check before delegating to real git. An old
or broken shim can block or swallow the push **before it ever reaches the
gateway**, so the gate never sees it and the operator sees nothing.

**Diagnose.**

```bash
command -v git                                   # ~/.appframes/shims/git ⇒ shim is active
echo "$PATH" | tr ':' '\n' | grep appframes/shims
```

**Fix.** Bypass it once with real git:

```bash
/usr/bin/git push origin main
```

Remove it for good: drop the `export PATH="$HOME/.appframes/shims:$PATH"` line
from your shell rc, then:

```bash
rm -rf ~/.appframes/shims        # or: nimblegate shell uninstall
```

---

## A repo gates fine but is missing from `what_changed`

**Symptom.** `gate_stats` lists the repo (decisions, findings, the lot), but
`what_changed` reports *"repo X not found, searched all repos instead"*.

**Cause.** The two tools read different sources. `gate_stats` reads the **audit
log**; `what_changed` lists the bare `<repo>.git` **directories** under the
repos root. If a repo's entry there is a **symlink** (e.g. a top-level
`<repo>.git` pointing at a bare repo kept under a subdirectory), older builds
skipped it: `os.ReadDir` reports the entry as a symlink, not a directory.

**Fix.** Current builds resolve symlinks, so this clears on the next gateway
update. To also normalise the on-disk layout so the repo matches the others
(and works on any build), replace the symlink with the real directory, run as
root on the gateway, when no push is in flight:

```bash
rm <repos-root>/<repo>.git                       # removes the symlink only;
                                                 # plain rm refuses a real dir, so it's safe
mv <symlink-target>/<repo>.git <repos-root>/<repo>.git
```

`mv` preserves ownership; confirm it matches a sibling repo with
`ls -ld <repos-root>/<repo>.git`.

---

## Inspecting a gateway bare repo by hand

Two things trip people up when running `git` directly against a gateway repo.
Neither indicates damage.

**`fatal: detected dubious ownership`.** The repos are owned by the gateway's
service user (commonly `git`); running git as **root** triggers this guard. Run
as the owner instead; do **not** add a root `safe.directory` exception:

```bash
runuser -u git -- git -C <repos-root>/<repo>.git log --oneline -1 main
```

(The gateway passes `-c safe.directory` internally, so its own reads never hit
this.)

**`your current branch 'master' does not have any commits yet`.** Bare repos
initialise with `HEAD → master`, but pushes land on **main**. A plain `git log`
reads the unborn `master` and reports no commits, while the history sits on
`main`. Name the branch, use `--branches`, or repoint `HEAD` once:

```bash
runuser -u git -- git -C <repos-root>/<repo>.git symbolic-ref HEAD refs/heads/main
```

`what_changed` already reads with `--branches`, so it is unaffected either way.

---

## A repo records findings but never blocks pushes

Not a bug: the repo is in **observe mode** (`observe = true`). It records
findings, including BLOCK-severity ones, but relays every push and stays silent
to the client by design. See
[What it catches and how it acts](../README.md#what-it-catches-and-how-it-acts). Any per-repo
report leads with an `⚠ OBSERVE MODE` banner so the state is visible to the
operator; flip `observe = false` to enforce.
