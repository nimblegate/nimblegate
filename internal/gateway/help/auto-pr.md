# Auto-PR

When the gateway rejects a push, it posts a structured comment on the upstream PR **and** fires a webhook with the same JSON payload. Agents listening on either rail (Claude Code, Cursor, Copilot, custom CI) see the rejection in machine-parseable form and can fix it themselves: the headline value moment.

This page has four tabs. The header tab strip stays put; the content below it changes.

## Dashboard

PRs currently mid-fix-loop, sorted by attempt count (most concerning first). Each row shows:

- **Repo · PR #**: the affected pull request.
- **attempt N/M**: current attempt out of `max-attempts` (default 5).
- **Current bot**: the agent the gateway is talking to, e.g. `@cursor-bot`.
- **View PR**: deep link to the sticky comment on the upstream.
- **Reset Loop**: clears the per-PR state file. Use when the loop is wedged on a bad assumption and you want the next push to start fresh.

The search field above the list filters live by repo name or PR number, useful when you have lots of loops in flight.

## Repos

One row per registered repo with notification-rail status:

- **Status**: `enabled` or `off` (mirrors `[notification] enabled = …` in `gateway.toml`).
- **Webhook**: host-only display of the configured URL (the secret is never rendered).
- **Queue / Deadletter / Active loops**: current depths. Non-zero values get colored pills.
- **24h delivered**: successfully-delivered notifications over attempts in the last 24 hours.
- **Delivery error row**: if deliveries are failing, a warning row appears under the repo showing the upstream error + an actionable hint (e.g. an HTTP 403 names the missing token scope). No need to read `docker logs`.
- **Edit config**: jumps to the Setup tab for that repo.
- **Retry now** (shown when Queue or Deadletter is non-zero): resets the retry backoff and re-queues any deadlettered records so pending comments deliver on the next ~5s poll. Use it after fixing a wrong/expired upstream token - it saves waiting out the multi-hour backoff, and means no editing queue files on the server.

## Activity

Most recent notification-rail events across all repos in the last 24 hours, both rejections and the resolutions that close their loops:

- :icon-notif: **delivered**: a rejection's webhook + sticky comment landed.
- :icon-ok: **resolved**: a clean push closed the loop; the comment flipped to ✅ and the dashboard loop cleared.
- :icon-pending: **queued**: accepted into the queue, drain pending.
- :icon-warn: **deadlettered**: permanent failure; needs operator attention (see Health).

## Setup

**Prerequisite - gate the PR branches.** The loop only fires on a *rejected* push, and only gated refs are checked. Agents work on feature branches (often git worktrees) and open one PR per branch, so set the repo's **protected refs to `refs/heads/*`** (Repos → the repo → Edit policy → **Edit repo settings**). The default `refs/heads/main` gates only `main`, so feature-branch pushes never trigger the loop.

Per-repo edit form for `[notification.*]` in `gateway.toml`. The **Editing:** dropdown at the top picks the repo; the form below maps 1:1 to the TOML keys.

Top-level toggles:

- **Enable notifications for this repo**: the master switch.
- **Also send notifications in observe mode**: by default observe-mode pushes don't fire the rail.

Webhook section:

- **Webhook URL**: your receiver endpoint.
- **Auth mode**: HMAC-SHA256 (recommended) / Bearer / None.
- **Secret**: signing key or Bearer token. Click **Generate random** for a fresh 32-byte hex value. HMAC mode requires a non-empty secret or save will fail.
- **Auth header**: optional override; defaults to `X-Hub-Signature-256` (HMAC) or `Authorization` (Bearer).

Mention section:

- **Default mention**: which bot to @-tag in the sticky comment, e.g. `@nimblegate-bot`.
- **Auto-tag PR assignees + reviewers**: pulls names from the upstream PR.

**Multi-bot rotation (opt-in)**: when one bot can't fix the issue, fall through to the next:

- **Bots (ordered)**: one per line.
- **Attempts per bot**: how many tries before rotating.
- **Rotate immediately on same finding**: skip remaining attempts if the same fingerprint reappears.
- **Fallback human**: who to @-tag once all bots are exhausted.

**Loop guardrails** (usually leave these alone):

- **Max attempts**: global ceiling per PR (default 5).
- **Cooldown threshold count / window**: N attempts inside the window triggers cooldown.
- **Cooldown duration**: how long the rail stays quiet after cooldown trips.

**Delivery**: webhook/comment retry shape:

- **Max attempts**: total retries before deadletter (default 20).
- **Backoff schedule**: comma-separated durations, e.g. `1m, 5m, 30m, 2h`.

## Common gotchas

- HMAC mode without a secret will refuse to save. Either Generate one or pick a different auth mode.
- Changing `webhook_url` doesn't replay queued records. They retry against the new URL automatically on the next drain.
- Reset Loop deletes the per-PR state file; the next push starts attempt 1/N with the default bot. It does **not** clear notifications already sent to the upstream.

For depth: [docs/notifications.md](https://github.com/nimblegate/nimblegate/blob/main/docs/notifications.md) (operator guide) · [docs/adapters.md](https://github.com/nimblegate/nimblegate/blob/main/docs/adapters.md) (adapter-author guide).
