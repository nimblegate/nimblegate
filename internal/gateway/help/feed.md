# Feed

Live decision stream: one row per push the gateway has handled. The table has four columns: **time**, **location** (repo + ref + SHA), **status** (decision + notification + findings + loop), and a narrow **reset** column for active-loop controls.

## Key actions

- **Filter by repo**: use the repo dropdown at the top right; all-repos shows everything.
- **Click a row**: expands to the full finding list with file:line + rule ID + reason for each fired frame.
- **Watch live**: the feed refreshes automatically (interval set in [Settings → Display](/settings?tab=display)); new pushes appear at the top.

## Status-column elements

The status cell stacks information vertically. Each push always shows the decision; the rest are conditional.

- **:icon-accept: accept / :icon-reject: REJECT**: top-line outcome.
- **Notification chip**: appears when the notification rail was engaged. :icon-notif: delivered / :icon-pending: queued / :icon-warn: deadlettered. Hover for the message.
- **Findings list**: one pill per fired frame: `BLOCK security/no-private-keys-in-repo`. Click any pill to expand its reason. The pill color matches severity.
- **:icon-loop: N/M @bot pill**: appears when this push opened or continued a fix-loop on the upstream PR. The pill mirrors the BLOCK/WARN geometry so the row stays compact; `N` is the current attempt, `M` is the configured max.
- **no PR comment: notifications off [Enable]**: appears on a rejected push for a repo that has an upstream but the notification rail switched off, so no PR comment was posted. The default is off, so this is the reminder that the auto-PR loop won't fire until you enable it; the link opens the per-repo config. Operator-side only - the agent that pushed never sees it.

## Reset column

When a row has an active loop, the rightmost column shows a small **Reset** button. Clicking it confirms then deletes the per-PR state file. The next push to that PR starts fresh at attempt 1/N with the default bot. It does **not** clear notifications already sent to the upstream.

## Common gotchas

- A `?` next to "last seen" means the SSH key is unfamiliar: first-push TOFU is in effect.
- Findings show against the policy as-of when the push started; later policy changes don't backfill the row.
- The Reset button is only useful when a loop is wedged; most loops drain on their own as the agent fixes and re-pushes.
- Notification chip absence ≠ the rail is off. Pushes that landed before the rail was enabled won't have it; only newly-rejected pushes get the chip.

For depth: [README](https://github.com/nimblegate/nimblegate/blob/main/README.md#how-it-works) · [docs/notifications.md](https://github.com/nimblegate/nimblegate/blob/main/docs/notifications.md).

## History: paging, retention, and export

The feed shows the newest decisions first and auto-refreshes every few seconds.
To look further back, use **Load older** at the bottom - it pages back through
retained history.

History does not grow forever. The maintenance loop trims old records on its
interval (see Settings → maintenance):

- **Accept** records are kept for `accept_retention` (default 30 days).
- **Reject / observed** records are kept for `reject_retention` (default: forever)
  - the gateway never auto-deletes evidence of a blocked push.
- The gateway-wide event log is trimmed at `events_retention` (default 30 days).

Unreadable log lines are never dropped by trimming.

To keep a permanent copy, use **Export JSONL** (faithful to the on-disk format)
or **Export CSV** (spreadsheet-friendly) in the filter bar. Export before lowering
`reject_retention` if you want an archive of older rejects.
