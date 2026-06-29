# Changelog

All notable changes to nimblegate will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - (set date at tag time)

### Added

- **Commercial-license self-attestation.** A status pill in the dashboard top
  bar reads "Non-commercial use" by default and flips to "Licensed" once you
  record a license on Settings -> About (checkbox + optional Lemon Squeezy order
  reference). Honor-system only: it is stored locally in `license.toml`, is never
  validated, and contacts no server. A "Get a license" link points to the
  commercial-license purchase path.

### Changed

- New repos now default their protected refs to `refs/heads/*` (gate every
  branch) instead of `refs/heads/main`, so the auto-PR fix-loop works on agent
  feature branches out of the box. Content-gating only; branch deletion stays
  protected on `main`/`master`, so feature branches remain deletable. Existing
  repos keep their stored setting.

### Security

- Reject unsafe repo names before any path is constructed across the policy and
  access stores (defense-in-depth path-confinement; repo names are already
  validated at every HTTP entry).
- Validate upstream URLs and add the `--` option terminator to git invocations
  in seeding and reconciliation, preventing a hostile URL from being read as a
  git option. The upstream URL never appears in argv where it could be misread.
- HTML-escape reflected dashboard output (frame id, severity, repo name).
- Restrict the post-login redirect to same-site local paths (reject `//` and
  `/\` forms; host-checked).
- Bounds-check the audit retention integer conversion.
- Document in `SECURITY.md` that test fixtures and rule definitions intentionally
  contain detection patterns, which produce expected scanner false positives.

### Fixed

- Handle close/flush errors when writing the audit log, event log, and
  notification queue, so a failed flush can no longer silently lose a record.
- Make active fix-loop selection deterministic on ties (sort by PR number).
- Close files in the demo static-build script; remove dead code in several
  frame checks.

## [0.1.0] - 2026-06-25

Initial public release. nimblegate is a self-hosted git push gateway that checks
an AI agent's pushes against your rules **before** they reach your real repo, and
relays the clean ones through byte-for-byte unchanged.

### The gate

- Pre-receive policy engine: pushes to protected refs are checked against the
  enabled frames (rules); a finding **rejects** the push with a clear reason,
  while a clean push is **relayed to the upstream untouched** (same SHA, author,
  signature). Invisible when clean - a clean push forwards in under a second.
- Stdlib frames grouped into kits - `core` (catastrophic-prevention: hardcoded
  credentials, private keys, `rm -rf` of protected paths, force-push to main,
  migration/ schema-drift guards, …) plus web-app, cf-pages, cf-workers,
  security-strict, and encoding-strict.
- Per-repo policy: enable/disable frames + kits, per-frame severity overrides,
  per-finding whitelist exemptions, and protected-ref patterns (e.g.
  `refs/heads/*` to gate every branch).
- Observe mode: record would-blocks without rejecting, for measuring an agent
  before you enforce.
- Custom RE2 regex linters, authored from the dashboard, run alongside the
  stdlib frames.

### Three-places model + relay

- Your machine pushes to the **gateway** over SSH (key auth, port 2222); only the
  gateway holds the upstream credential and relays clean pushes to your real host
  (GitHub / Gitea / GitLab) over HTTPS + a token - works for public **and**
  private repos.
- Registering a repo whose upstream already has history mirrors it into the
  gateway automatically; a per-row **Sync from upstream** re-mirrors on demand.

### Auto-PR / webhook rail (the agent fix-loop)

- A rejected push posts a structured comment on the upstream PR **and** fires a
  webhook with the same JSON, so an agent (Claude Code, Cursor, Copilot, custom
  CI) reads the rejection and fixes itself.
- Per-PR fix-loop: attempt counter `N/M`, bot rotation, and a sticky comment that
  flips to **✅ All findings resolved** on a clean push; loop guardrails
  (max-attempts, same-finding fast-rotation, cooldown).
- Durable delivery (queue + background daemon with backoff → deadletter). The
  dashboard **surfaces delivery errors with an actionable hint** - e.g. an HTTP
  403 names the missing token scope per host (GitHub `repo` / Issues + Pull
  requests; Gitea `write:issue`; GitLab `api`) - plus a **Retry now** button that
  resets the backoff and re-queues deadlettered records.

### Dashboard

- Live decision feed (filter by repo/severity, day separators, JSONL/CSV export,
  retention-aware paging), stats + time-saved, server-rendered agent reports,
  repos (register / archive / delete-permanently / edit upstream + protected
  refs), policy (frames / custom linters / whitelist), Auto-PR
  (dashboard / repos / activity / setup), health, settings, and per-page help.
- Single-admin auth: bcrypt + server-side sessions + a first-run setup token.

### Agent analytics API

- Read-only analytics over the decision log, bearer-token authed: a REST surface
  under `/api/v1/` and an MCP endpoint (JSON-RPC over HTTP) exposing the same
  seven tools - `gate_stats`, `bounce_rate`, `top_rules`, `recurring_findings`,
  `decisions`, `time_saved`, `what_changed`.

### Install + operations

- Combined container (sshd + dashboard supervised by s6-overlay) and a bare-metal
  install path; persistent volumes for the bare repos, config, and SSH state.
- Periodic maintenance: `git gc`, decision-log + events retention, session / tmp /
  deadletter self-cleanup, and unattended security upgrades.
- One-command public deployment via `gateway tls-setup` (Caddy + automatic TLS).
- Optional scoped per-key access (deny-by-default forced-command shell) for
  multi-tenant gateways.

### Security model

- Forced-command SSH shell: command parse + verb whitelist + symlink-safe,
  root-confined repo resolution, with an optional per-key ACL. The gate runs on a
  box the agent can't reach and holds the sole upstream credential; commits relay
  byte-for-byte. A `receive.maxInputSize` cap closes the disk-fill DoS vector.
- Source is licensed under PolyForm-Noncommercial-1.0.0 (free for non-commercial
  use; see the README for commercial licensing).
