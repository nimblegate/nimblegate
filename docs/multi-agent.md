# Multi-agent concurrency

How nimblegate behaves when multiple processes (multiple AI agents, multiple terminals, multiple developers, or any combination) operate on the same project simultaneously.

## TL;DR

Safe by design. Each `nimblegate` invocation writes to its own private audit-log file under `.appframes/audit.parts/`. Readers (`status`, `audit analyze`, `watch`) see a unified view. A compaction step consolidates quiescent parts into the main `audit.log` periodically. Mutating commands (`enable` / `disable` / `incident new` / `incident promote`) are infrequent enough that race-to-collision probability is low; failures are loud.

## What works without contention

| Operation | N processes simultaneously | Notes |
|-----------|---------------------------|-------|
| `nimblegate check` | Safe | Read-only against the project; each writes its own audit part |
| `nimblegate git` / `cmd` (the wraps) | Safe | Same as above; git itself serializes commits at the kernel level |
| Pre-commit hook | Naturally serialized | Git itself only fires one commit at a time |
| Frame check execution (within one process) | Safe | Goroutine-safe via mutex |
| Whitelist + scan-ignore matchers | Safe | Read-only, rebuilt per run |

## The audit log architecture

```
.appframes/
├── audit.log                                # consolidated history (compaction target)
├── audit.log.1, audit.log.2, ...            # rotated siblings
└── audit.parts/                             # per-process append-only files
    ├── audit.<startTimeNs>.<pid>.log        # writer A
    ├── audit.<startTimeNs>.<pid>.log        # writer B (different inode)
    └── .compact.lock                        # sentinel for single-compactor coordination
```

### Writers

Each `OpenAudit` invocation creates a new part file at `audit.parts/audit.<nanoseconds>.<pid>.log`. The filename embeds both the nanosecond start time and the PID, so two processes that start in the same second still get distinct filenames. Each writer has a private inode: no shared file descriptor between processes, no inter-process lock contention, no rotation race.

### Readers

`engine.RotatedFiles(auditLogPath)` returns a chronologically-sorted list of every audit-log file the project has: the consolidated `audit.log` plus its rotated siblings plus every active part file under `audit.parts/`. Readers don't need to know about the parts/consolidated split; they just iterate the unified list.

This is what powers `nimblegate status`, `nimblegate audit analyze`, `nimblegate watch`, `nimblegate audit reset`, and the audit-log reading in other tools.

### Compaction

`engine.CompactAudit(projectRoot, quiescenceWindow)` walks `audit.parts/`, finds files whose mtime is older than the quiescence window (default 5 minutes), and appends each in chronological order to `audit.log`. The consumed parts are removed. A sentinel-file lock (`O_EXCL` on `.compact.lock`) prevents two compactors from running simultaneously: the second concurrent call is a clean no-op.

When `audit.log` grows past `APPFRAMES_AUDIT_MAX_BYTES` (default 10MB), compaction rotates it first via the same `.1`, `.2`, ... scheme used elsewhere. Up to `APPFRAMES_AUDIT_MAX_FILES` siblings (default 5) are kept.

### When compaction runs

- **Manually:** `nimblegate audit compact [--quiescence 5m]`
- **Opportunistically:**
  - `nimblegate status` runs CompactAudit before reading
  - `nimblegate audit analyze` runs CompactAudit before reading
- **Never silently in the background.** There is no daemon. Compaction is always tied to a user-initiated command.

You can let parts accumulate freely between compactions. The only cost is a `audit.parts/` directory with a few dozen files. Compaction is fast (proportional to part file count) and tidies the directory.

## Quiescence semantics

A part file is eligible for compaction only after `quiescenceWindow` has passed since its last `mtime` update. Default is 5 minutes. The window protects against compacting a file an active writer is still appending to.

**Edge case the design accepts:** an active `nimblegate` process that writes once, sits idle for >quiescence, then writes again. If compaction runs during that idle window and consumes its part file, the next write goes to the now-deleted inode (still held by the writer's open FD) and is lost.

For an observability log (not a ledger), this is the right tradeoff:
- Default quiescence (5min) is longer than typical agent runtimes
- Long-running agents that span >5min between audit writes are rare
- The lost data is observability information: the gate itself still fired correctly

If you need stricter guarantees (e.g. CI environments with long-running batched jobs), raise quiescence via `--quiescence 30m`.

## Concurrency semantics for mutating commands

These commands modify project state and have small race windows. The probability of collision in normal multi-agent use is low; failure modes are loud:

| Command | Race window | Failure mode |
|---------|-------------|--------------|
| `nimblegate enable` / `disable` | Read-modify-write on `appframes.toml` | Last-write-wins; silently overwrites concurrent change. Easy to detect (the enabled list "doesn't have what I just added") |
| `nimblegate incident new` | Stat-then-write on `_incidents/<slug>.md` | "File already exists" error; loud, easy to recover by retrying with `--title` variant |
| `nimblegate incident promote` | Stat-then-write on `<category>/<name>.md` | Same as above: second writer gets clear error |

If your team workflow involves multiple agents running these commands simultaneously, coordinate via your existing channels (don't have two agents call `nimblegate enable @cloudflare` at the same second).

## Operational guidance

### Inspecting per-agent activity

Each part filename embeds the originating PID. To see what agent wrote what:

```bash
$ ls -la .appframes/audit.parts/
audit.1779117401232049978.146737.log    # PID 146737 started at 17:23:21
audit.1779117425883204512.146821.log    # PID 146821 started at 17:23:45
```

For richer per-agent stats, query the audit log with `jq` after compaction:

```bash
$ nimblegate audit compact && jq -r '.frame' .appframes/audit.log | sort | uniq -c | sort -nr
  43 security/no-hardcoded-credentials
  18 command-safety/curl-pipe-shell
  ...
```

### Cleanup if a part file is orphaned

A killed `nimblegate` process leaves its part file behind. The file is harmless; it just won't grow further. Next compaction (after quiescence) consumes it into `audit.log`. If you want immediate cleanup:

```bash
nimblegate audit compact --quiescence 1s
```

This forces every part to be considered eligible regardless of recent activity. **Use only when you know no other agents are writing**: the 1s window can race with an active writer.

### Disk usage

Per-agent part files are small. A typical `nimblegate check` writes ~5KB of audit log. A long agent session might accumulate 50-500KB. Compaction consolidates these without size penalty (audit.log absorbs the same bytes that were in parts).

`nimblegate audit reset --yes` clears everything: parts and consolidated. `--backup` preserves the family with a `.reset-<timestamp>` suffix.

## What this design does NOT solve

- **Distributed concurrency.** If your project is checked out on multiple machines (NFS, shared SMB), the part-file design assumes a single filesystem with consistent inode semantics. Distributed filesystems with eventual consistency are out of scope.
- **CI parallelism with shared state.** If your CI runs `nimblegate check` on N parallel runners against the same repo (rare; usually each runner gets a fresh clone), the parts will be local to each runner. Aggregation across runners is a separate concern (artifact upload + post-job merge).
- **Hung writer recovery.** If a writer hangs (doesn't crash) and holds its part file open indefinitely, compaction will eventually consume the file after quiescence and the writer's subsequent writes are lost. There's no liveness check (intentional; adds complexity for a rare case).

## Verified by

- 7 dedicated tests in `internal/engine/compact_test.go` covering: quiescent-merge, recent-skip, missing-parts-dir, chronological-order-preservation, concurrent-lock contention, the no-contention guarantee (two concurrent writers don't lose entries), empty-part handling, and that `RotatedFiles` includes part files in its unified view.
- `TestE2E_ConcurrentChecks` in `internal/commands/integration_test.go` runs 10 concurrent `nimblegate check` invocations and verifies no JSON lines are torn.
- `TestStress_AuditConcurrentWriters` in `internal/engine/stress_test.go` runs 50 goroutines × 200 writes each against a single writer's part file (within-process concurrency, mutex-protected).

Full suite: **521 test functions across 14 packages, 0 failures.**
