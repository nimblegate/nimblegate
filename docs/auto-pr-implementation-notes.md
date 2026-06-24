# Auto-PR rail: implementation notes (2026-06 wiring pass)

Trace doc for the session that took the auto-PR / webhook rail from **fully built
but never wired into the running gateway** to **functional end to end**. Written
so the next person adding features can see how the pieces connect and which bugs
were already paid down.

## TL;DR

The rail's building blocks (`notification.Build`, `Orchestrator`, `Daemon`, the
Gitea/GitHub `upstream` adapters, queue/deadletter, the `Transition` loop state
machine) all existed and were unit-tested, but **nothing in any running process
invoked them**. A rejected push wrote no queue record, and even if it had,
nothing drained the queue. This pass wired all three layers and fixed the bugs
that surfaced when delivering against a real Gitea upstream.

## Architecture, as wired

```
push ──► pre-receive hook (gatewayPreReceive)
          │  RunPreReceive: check → decision
          │  REJECT  → buildNotification → stamp audit EventID → enqueue QueueRecord
          │  ACCEPT  → buildResolutions (loops on the ref) → stamp audit → enqueue + clear PRState
          ▼
        <policyRoot>/<repo>/pr-comment-queue.jsonl
          ▲
          │  drained every 5s by ──► notification.Daemon (goroutine in the dashboard process,
          │                            started by startNotificationDaemon; registry rebuilt each tick)
          ▼
        Orchestrator.DeliverOne(record)
          │  LookupByURL → adapter (per-repo Gitea/GitHub, built with the repo's PAT)
          │  FindPRForRef → ReadPRPeople
          │  push.rejected → Transition (advance attempt/rotation) → WritePRState → render reject comment → upsert sticky
          │  push.resolved → render ✅ comment → update sticky (PRState already cleared by the hook)
          │  webhook ALWAYS fires when a URL is configured
          ▼
        Gitea/GitHub PR comment  +  webhook POST
```

Read-side: the dashboard never mutates the append-only audit log. The push
stamps the audit record with the notification **EventID** only; live status
(`delivered` / `queued` / `deadlettered`) is recovered at render time by
`gateway.CorrelateNotificationStatus`, which looks the EventID up in the
queue/deadletter files. Used by both `ReadDecisions` (feed) and
`readRecentNotificationEvents` (Activity tab).

Loop state lives at `<repo>/pr-comment-state/<pr>.json` (PRState). The dashboard
reads it for the attempt counter; a clean push to the gated ref clears it.

## Wiring done (features)

| What | Where | Commit |
|---|---|---|
| Pre-receive passes `NotificationConfig`+`PolicyRoot` → queue record written | `gatewayPreReceive` | 9fa3c6f |
| Daemon started as a goroutine alongside the dashboard; per-tick registry rebuild | `startNotificationDaemon`, `buildNotifRegistry` | 9fa3c6f |
| Loop attempt-counter / bot rotation run at delivery (`Transition` in `DeliverOne`); `QueueRecord.LoopConfig` carries the config | orchestrator, queue | 12ce05a |
| Loop closes on a clean push to the gated ref | `RunPreReceive` accept path | f4b3155 |
| PR comment flips to "✅ resolved" on the fix (`push.resolved` event + render branch) | build/render/orchestrator | 6df2809 |
| Bot-rotation banner rendered in the comment | `applyLoopState` | c0aa18d |
| Live notification status in feed + Activity (EventID stamp + read-time correlation) | `CorrelateNotificationStatus` | 21e328f |
| Resolutions shown in feed + Activity as "✅ resolved" | `buildResolutions`, Activity render | 0316c7a |

## Bugs found + fixed (root causes)

These were surfaced by live testing against a Proxmox/Alpine + Gitea install;
none were caught by the unit tests, which is itself a lesson (tests used clean
URLs / didn't exercise the running process):

1. **Rail never fired**: `gatewayPreReceive` built `PreReceiveDeps` without
   `NotificationConfig`/`PolicyRoot`, so the guard `NotificationConfig != nil`
   was always false. (9fa3c6f)
2. **Nothing drained the queue**: `Daemon`/`Orchestrator` had zero non-test
   callers. (9fa3c6f)
3. **Symlink-blind enumeration**: registered repos are activation symlinks
   (`<policyRoot>/<name> → _repos/<name>`); `os.ReadDir` + `DirEntry.IsDir()` is
   Lstat-based and skips symlinks. Hit in **three** places: `collectAutoPR` +
   `listConfiguredRepos` (e551313) and the daemon's `scanRepos` (9fa3c6f). Fix:
   glob `*/gateway.toml` or `os.Stat` (follows symlinks).
4. **`.git` in the API path**: `splitRepoURL` / `splitGitHubRepoURL` didn't
   strip the clone-URL `.git` suffix, so `…/you/gw-test.git/pulls` → 404 at the
   host for every real upstream. (c65f92b)
5. **Instant deadletter**: `DaemonConfig` built with a non-zero `PollInterval`
   skipped `resolveDefaults`, leaving `DeliveryMaxAttempts` at 0; `attempts >= 0`
   deadlettered on the first failure. Fix: start from `DefaultDaemonConfig()`.
   (c65f92b)
6. **Gitea `/requested_reviewers` 404**: many Gitea versions lack that
   GitHub-style endpoint; `ReadPRPeople` returned its error and sank delivery.
   Reviewers are optional → made best-effort. (f6670f4)
7. **`safe.directory`**: the dashboard process doesn't own the git-owned bare
   repos, so the seed/mirror git calls hit "dubious ownership". (19df03f, same
   guard `gitlog` already used)
8. **Dashboard CSRF**: the notification Setup form + Reset Loop form submitted
   natively (`method`/`action`) instead of `hx-post`, so the body's global
   `hx-headers` CSRF token never attached → 403 "csrf". (0405e89, e24d61c)
9. **Empty comment Location**: `fileLineRe` was `^`-anchored, but several frames
   prefix the message ("pipe-to-shell patterns detected: deploy.sh:1, …"), so
   the file:line sat mid-string. Unanchored. (12ce05a)

Not auto-PR but found in the same pass: registration left the gateway repo empty
for an existing upstream → added auto-mirror at registration + a "Sync from
upstream" row action (dc730cf, cca5a72, 19df03f).

## Onboarding gotcha (not a code bug)

**Gitea PAT needs the `issue` write scope**, not just `repository`. PR comments
are issue comments in Gitea, so a repo-only token posts the comment with HTTP
403. Grant `write:repository` **and** `write:issue` (or the broad scope), then
rotate the credential on `/repos`. This is the one config step that isn't
discoverable from the gateway side.

## Remaining follow-ups

- **No inline delivery from the hook**: `DeliverOne`'s inline path runs only if
  an `Orchestrator` is injected into `PreReceiveDeps`; the hook doesn't, so the
  daemon does all delivery (≤ ~8s latency: `InlineRaceGap` 3s + poll 5s). Fine
  for now; wire an inline attempt if latency matters.
- **Multi-PR-per-ref resolutions**: only the first resolution's EventID is
  stamped on the (single) audit record, so the feed/Activity show one of N. Rare.
- **GitHub adapter** ships but is only exercised against Gitea live; verify
  against a real GitHub upstream before claiming it.
