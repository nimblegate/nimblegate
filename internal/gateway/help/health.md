# Health

Notification rail telemetry: queue depth, deadletter counts, recent delivery success. Values are computed on each request; reload to refresh.

## Service status

- **Dashboard service**: PID + uptime of the web UI. Restarting the systemd unit zeros this.
- **Daemon loop**: drain worker that empties the notification queue. The "last successful drain" is the freshest `DeliveredAt` across every repo's audit log; a fresh box reads as "no successful drain yet" until the first notification lands.
- **Disk free**: bytes available on the policy-root filesystem. Green :icon-ok: = ≥10% free; orange :icon-warn: = below the 10% threshold. The gateway needs disk for queue + state + audit log, so this is worth glancing at before you find out the hard way.
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
