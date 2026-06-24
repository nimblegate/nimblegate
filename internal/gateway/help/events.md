# Events

Raw audit-log stream: every event the gateway has logged, including the ones that don't show in the [Feed](/feed): config changes, credential rotations, login attempts, kit applies, frame toggles.

## Key actions

- **Filter by type**: push, login, config-change, credential-update, etc.
- **Filter by repo**: use the repo dropdown.
- **Click a row**: shows the full event payload (structured JSON for inspection).

## Events vs. Feed

- **Feed** = pushes only, decision-focused, user-friendly.
- **Events** = everything the gateway records, including admin actions. Use this when investigating "what happened at 14:37?" or "did someone change the kit?"

## Common gotchas

- Events are append-only and on-disk; nothing here can be edited or deleted from the UI.
- Credential-update events have no payload, just the fact and the timestamp.
- **Process restarts** (crashes, manual restart, container recreates) don't appear here. For the container install they go to `docker logs nimblegate`; for bare-metal they go to the systemd journal (`journalctl -u nimblegate-dashboard.service`, the unit name kept the v0.1.0 codename). The dashboard does emit a **`build-update`** event when the running binary's SHA differs from the prior start (formatted `build abc1234 → build d9fe903`, with `(dirty)` appended when the binary was built from a working tree with uncommitted changes), so a real version change always shows up here. A restart that loads the *same* binary is silent in events; the process log is the right place to see it.

For depth: [README: Operator visibility](https://github.com/nimblegate/nimblegate/blob/main/README.md#operator-visibility).
