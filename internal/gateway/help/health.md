# Health

Notification rail telemetry: queue depth, deadletter counts, recent delivery success. Values are computed on each request; reload to refresh.

## Service status

- **Dashboard service**: PID + uptime of the web UI. Restarting the systemd unit zeros this.
- **Daemon loop**: drain worker that empties the notification queue. The "last successful drain" is the freshest `DeliveredAt` across every repo's audit log; a fresh box reads as "no successful drain yet" until the first notification lands.
- **Disk free**: bytes available on the policy-root filesystem. Green :icon-ok: = ≥10% free; orange :icon-warn: = below the 10% threshold. The gateway needs disk for queue + state + audit log, so this is worth glancing at before you find out the hard way.
- **Gateway config** (only when something is wrong): a `[gateway]` setting that parses fine but has no effect - written without a `[gateway]` header, under another section, or into a repo's own `<policy-root>/<repo>/gateway.toml`, which shares its filename with the machine-level file one directory up but holds per-repo policy instead. Also shown when the file fails to parse. The message names the key, the file it was found in, and where it belongs.
- **Scan staging** (when `--repos-root` is set): where pushed trees are extracted for scanning, and free space on **that** filesystem - which is not the policy root's once `[gateway] scan_tmpdir` points elsewhere, and is the mount that decides whether a push can be scanned at all. Shows the effective path, notes when it came from `scan_tmpdir`, and says "created on first push" before any push has made it. Orange :icon-warn: means either under 10% free, or that the configured directory could not be created and staging fell back to `$TMPDIR` - tmpfs on most distributions, so trees are being staged in RAM, which is what setting `scan_tmpdir` was meant to avoid. Check [/events](/events) for the `scan-staging-fallback` entry naming the path and the reason.
- **Memory pressure**: the share of wall-clock time tasks spent stalled waiting on memory, read from the kernel's PSI counters (the container's own figure when running under a memory limit, the host's otherwise). Green :icon-ok: below the thresholds; orange :icon-warn: at `some` >= 10% or `full` >= 5% over the last 10 seconds, which means tasks are already waiting and the OOM killer is the next event if it keeps climbing - add RAM, cap push concurrency, or reduce the frame load. Free-memory numbers cannot tell you this: a box with little free memory and no reclaim stalls is healthy, and one with headroom that reclaims constantly is not. Reads "unavailable" on kernels before 4.20, builds without `CONFIG_PSI`, and distros that keep it behind the `psi=1` boot flag - that means no data, not a clean bill of health.
- **Gate scans** (only when non-zero): pushes the gate could not evaluate in the last 24h - a full staging filesystem, an unwritable scan dir, a tree over `max_tree_bytes`, or a frame run past `scan_timeout`. Under enforcement those pushes were rejected; in observe mode they were relayed **unscanned**, and this line is the only place that says so.
- **Repo connection** (when `--repos-root` is set): one-line summary of whether every registered repo has the files the gateway needs to accept pushes and relay them upstream (bare repo, `gateway.toml`, `appframes.toml`, and a credential when the upstream is HTTP). Green :icon-ok: = all repos connected. Orange :icon-warn: = N issue(s) across M repo(s), with the blocking count broken out when non-zero. Click through to [/repos](/repos) for the per-issue Repair buttons. Suppressed when reposRoot isn't configured because the check needs both roots to detect missing bares.
- **Maintenance** (when configured): periodic self-cleanup loop. Shows the interval, last sweep time, next sweep time, and per-task summary lines (auth-session prune, /tmp orphan cleanup, deadletter retention). Per-repo `git gc` details collapse behind an expandable "per-repo gc" section. Configured in `<policy-root>/gateway.toml` `[maintenance]`; disabled if that section isn't present or `--repos-root` isn't set. See [docs/server/SECURITY-MODEL.md "Maintenance loop"](https://github.com/nimblegate/nimblegate/blob/main/docs/server/SECURITY-MODEL.md) for what each task touches.

## Notification queue per repo

One row per registered repo:

- **Queue**: records waiting to drain. Non-zero is normal during a delivery storm; persistently high means the receiver is slow or broken.
- **Last drain**: when this repo's queue last produced a successful delivery.
- **Deadletter**: permanently-failed records parked aside so they don't clog the queue. Non-zero gets an **Investigate** button.

## Recent activity (last 24h)

Two success-rate bars over notifications attempted in the window:

- **Webhook delivery success**: your receiver returned 2xx.
- **PR comment success**: upstream API (Gitea / GitHub) accepted the sticky comment.

Watching them separately catches per-side failures: webhook 100% but comments 0% usually means the upstream PAT is expired; the reverse means your webhook receiver is down.

## What deadletter means

A record is moved to `pr-comment-deadletter.jsonl` after `delivery.max-attempts` (default 20) consecutive failures, usually because the webhook URL is wrong, the auth secret was rotated, the upstream is permanently 401-ing, or the PR no longer exists. The daemon stops retrying it, so a single broken config doesn't bury the queue. **Investigate** lets you inspect the record + the last error; fix the root cause (Auto-PR · Setup) and the next pushes drain normally.

## Common gotchas

- A long "last drain" with zero queue depth just means nothing has been rejected recently; it's not an alarm.
- Deadletter doesn't shrink automatically. Once you've fixed the root cause, you can manually replay the file or delete it; see the depth link.
- Disk-free is measured at the policy root, not `/`. A separate volume for `/srv/gateway` is a sensible deploy choice.

For depth: [docs/notifications.md: Operations](https://github.com/nimblegate/nimblegate/blob/main/docs/notifications.md#operations).
