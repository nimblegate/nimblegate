# Changelog

All notable changes to nimblegate will be documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and
this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.2] - 2026-09-01

### Fixed

- **"Apply recommended" could turn gating off.** The scan recommendation passed
  kit names into `[frames] enabled`, which holds frame IDs. A kit name matches
  no frame, and a non-empty allowlist replaces the empty-means-every-frame
  default - so a repo whose list held only kit names ran no checks at all,
  while the dashboard and `gateway doctor` both reported those entries as
  active frames. Kits are now expanded to their frame IDs and recorded under
  `[ui] applied_kits`, the same way the Apply-kit button already did.
- **Existing repos are repaired on startup.** A warning alone would leave
  affected repos ungated, so the dashboard rewrites kit names in
  `[frames] enabled` into frame IDs before it starts serving. Empty lists stay
  empty, clean repos are untouched, and unrecognised entries are kept rather
  than silently dropped.
- **`gateway doctor` misread the frame allowlist.** An empty list means every
  stdlib frame runs; doctor reported it as a failure. It now reports OK for an
  empty list, WARN when some entries resolve to no frame, and FAIL only when
  none resolve and nothing is actually being checked.
- **Turning a frame on from the tuning page accepted any string.** `toggleFrame`
  now rejects an id that names no stdlib frame, closing the other route into
  the allowlist.
- **Doctor printed a push port it could not know.** The SSH-gate line reported
  the port the probe reached with "push to this port from your dev box" -
  inside the container that is sshd's internal `22`, not the published `2222`
  the operator uses - while the connect URLs hardcoded `2222`, which is wrong
  on bare metal, where sshd is on `22`. On a real gateway the two lines
  contradicted each other in one report. Each deploy path now declares its
  published port through `NIMBLEGATE_PUBLIC_SSH_PORT` (`compose.yaml` from the
  same variable it publishes, the bare-metal dashboard unit as `22`), and
  doctor resolves that first, then the port the probe reached where that
  observation means anything, then the install shape's own convention - 22 on
  bare metal, 2222 for the compose publish. A container's probe is deliberately
  ignored: it reaches sshd inside the container, never the published port.
  `gateway doctor --push-port` overrides.
- **The container dashboard could not see the container's environment.** Its s6
  run script used a plain `#!/bin/sh`, which gets s6's environment rather than
  the container's; it is now `#!/command/with-contenv sh`.
- **The Relay check ignored the only signal a container has.** It read
  `relay-status.json`, written solely by the reconcile backstop in
  `gateway relay-service`, so a gateway relaying every push reported "no relay
  status yet" forever when that service was not running - and the published
  image runs no such service at all, relaying inline from post-receive. It now
  reads the per-push `relay-ok` / `relay-failed` events the dashboard's
  **Repos** page already used, and the two agree. A push that never reached the
  upstream is a failure even when a backstop record says otherwise.
- **Doctor printed remediation for the install it was not running on.** The
  bind-host fix named compose variables to bare-metal operators, and the relay
  advice named `systemctl` inside a container that has no such service. The
  facts a gateway cannot observe about itself now live in one `InstallProfile`
  per shape, declared by the image (`NIMBLEGATE_INSTALL=container`) and by the
  bare-metal unit, with `/run/s6` and `/run/systemd/system` as fallback
  detection. The report names the shape it resolved. Drift recovery is reported
  once, as a `Relay backstop` check, and only by shapes that have one.
- **The connect steps assumed you were reading them in the dashboard.** Step 3
  said "the value shown is the address you reached this dashboard on" in CLI
  output, where no dashboard was involved.

## [0.4.1] - 2026-08-28

### Fixed

- **`nimblegate uninstall` could delete part of your shell rc file.** The
  uninstall path scanned the rc file, dropped the wrapper block, and wrote the
  rest back - but discarded the scanner's error. `bufio.Scanner` stops at any
  line over 64 KB, so a single long line (a large exported variable, a minified
  completion script) made it write back only the lines above that point,
  silently deleting everything below. It now refuses and leaves the file
  untouched, so the wrapper stays installed rather than the config being lost.
- **The Settings About tab hid license read errors.** `LoadLicense` returns a
  zero value and no error when the file is simply absent, so a non-nil error
  always means a real fault - bad permissions, malformed TOML. It was
  discarded, rendering the install as unlicensed with no signal. The tab now
  shows a warning callout with the cause.
- The demo snapshot's inline-script strip missed `<SCRIPT>`, mixed case, and
  close tags carrying attributes (`</script foo="bar">`). Build-time transform
  over first-party output, so not a trust boundary, but the loose form is free.

### Changed

- **The notification rail's two toggles default on.** *Enable notifications for
  this repo* and *Also send notifications in observe mode* now render checked
  for a repo with no `[notification]` section, so saving the rail once turns the
  auto-PR loop on. Observe mode is the recommended way to onboard a repo, and
  notifications off there means evaluating the gateway with the loop invisible.
  Form prefill only: an unsaved repo keeps the section absent and the rail
  inactive. "Reset section to defaults" resets to enabled to match.
- Releases now publish when the tag is pushed instead of waiting as a draft
  (`.goreleaser.yaml` `draft: false`).

### Documentation

- The policy help page names all six per-tier hour defaults instead of stopping
  at three.

## [0.4.0] - 2026-08-27

### Security

- **`gateway add --name ../evil` wrote outside the configured roots.** The
  dashboard has validated repo names since v0.1.0; no CLI path did. A name
  containing `..` or a separator became part of the policy and bare-repo paths,
  so registration landed outside `--policy-root`. `add`, `archive`, `delete`,
  `restore` and `rescan` now run the same predicate the dashboard uses, before
  any path is built. `delete --name ../x --yes` was the sharpest case.

### Changed

- **The gateway path flags default to the layout that ships.** Every `gateway`
  subcommand defaulted `--policy-root` to `/etc/nimblegate-gateway/repos` and
  `--repos-root` to `/srv/nimblegate-gateway/repos`, paths nothing installs to.
  Both the container and the systemd units use `/srv/gateway/cfg` and
  `/srv/gateway/repos`, which are now the defaults. Run any gateway command
  without path flags and it reads the gateway's real data. If you hand-installed
  elsewhere, keep passing the flags you already pass.
- **`gateway doctor` no longer reports an empty frame allowlist as a failure.**
  An empty `[frames] enabled` runs *every* stdlib frame - the list is an
  allowlist consulted only when non-empty. Doctor called that "no frames/rules
  active; pushes relay unchecked", and its implied remedy (apply a kit) is the
  one action that reduces coverage. Both states now say which mode a repo is in.
- **`gateway dashboard --repos-root` defaults**, so live check preview and the
  staging-health section on `/health` work without the flag.

### Added

- **`gateway add --kit`** applies a starter kit at registration, matching what
  the dashboard's Add form does. Opt-in: leave it off and the allowlist stays
  empty, which runs every stdlib frame. Unknown kit names are rejected before
  the repo is created.
- The seeded `appframes.toml` carries a comment explaining that an empty
  `enabled` list means every frame, and that adding entries narrows the set.

### Fixed

- `gateway restore` on an unknown repo reported a raw `stat ...: no such file
  or directory`; it now says no such repo is archived, and a genuine I/O error
  still surfaces as itself.
- `gateway rescan` on an unregistered repo said "no commits to scan"; it now
  says the repo is not registered. The first-push scan's "no ref to archive"
  wording collided with the `gateway archive` command and is now "no commits
  to scan".
- `gateway doctor`'s authorized-keys hint pointed at `/ssh-keys`, which reads
  as a filesystem path in CLI output; it now names the dashboard page.

### Documentation

- The command-line reference no longer instructs operators to pass
  `--policy-root` and `--repos-root` on every invocation.
- `frames.md`, `policy-authoring.md` and the server guide state that an empty
  frame allowlist runs every stdlib frame. The server guide's "minimal policy"
  example silently took a repo from 51 frames to 3.
- Every kit frame count in `frames.md` was stale (core 15 to 18, web-app 27 to
  30, cf-pages 29 to 32, cf-workers 20 to 23, security-strict 10 to 14); the
  table is now pinned to `stdlib.toml` by a test.
- Four internal working documents (`v0.5-candidates`, `test-suite-findings`,
  `auto-pr-implementation-notes`, `positioning`) are no longer published.

## [0.3.3] - 2026-08-27

### Security

- **A pushed symlink could make the gate read files outside the repo.** Frames
  read whatever the staged tree contains, and a tree is attacker-controlled: a
  committed `cred -> /etc/nimblegate-gateway/repos/<name>/credential` had the
  gate open that file and report findings about it - an arbitrary-read oracle
  over anything the gateway user can read, including other repos and the
  gateway's own upstream credential. Staging now replaces every symlink whose
  target resolves outside the staged tree with an empty regular file, before
  any frame runs. Links that stay inside the tree are ordinary content and are
  still scanned. Writing outside the tree was never possible: a git tree holds
  no `..` entry and cannot carry both a symlink `x` and an entry under `x/`.
- **Scan failures no longer leaked gateway internals to the pusher.** A push the
  gate could not evaluate printed its cause verbatim, so a client able to fill
  the disk received `ERROR [gateway] materialize: untar: exit status 2` -
  revealing the gateway and its extraction mechanism. The reject is now the
  neutral per-ref line any host prints; the cause goes to the audit log.
- **Connection timeouts on both dashboards.** They listened via the zero-value
  `http.Server`, so a client dribbling headers held a handler goroutine
  indefinitely. Header, read, write, and idle deadlines are now set.

### Added

- **`[gateway]` section in `<policy-root>/gateway.toml`**: `scan_tmpdir` moves
  where pushed trees are staged (default `<repos-root>/_scan-tmp`, deliberately
  not `/tmp`, which is tmpfs on most distributions - staging a full copy of
  every in-flight push there is how a large repo becomes an out-of-memory kill);
  `max_tree_bytes` caps what one push may expand to (default 2 GiB, 0 =
  unlimited) - `max-input-size` bounds the compressed pack and says nothing
  about the tree, since a blob of zeros extracts in full; `scan_timeout` bounds
  one push's frame run (default 5m, 0 = no deadline).
- **`/health` reports memory pressure** from the kernel's PSI counters (the
  container's own figure under a memory limit, the host's otherwise), warning
  before the OOM killer intervenes rather than after. Also reports the effective
  scan-staging directory with free space on *its* filesystem, and counts pushes
  that could not be scanned in the last 24h.
- **Misplaced `[gateway]` keys are reported** on `/health` and by
  `gateway doctor`. A knob written without a `[gateway]` header, under another
  section, or into `<policy-root>/<repo>/gateway.toml` - the per-repo policy
  file that shares its filename - parses cleanly and does nothing.

### Fixed

- **A gate that cannot evaluate a push now says so distinctly.** Scan failures
  carry `gateway/scan-failed` rather than collapsing into a frame finding: still
  a reject under enforcement, but the notification rail and a `scan-failed`
  event fire even under observe, where the push is relayed unscanned and nothing
  else would tell the operator that scanning had stopped.
- **Extraction failures name their cause.** `tar`'s stderr was discarded, so a
  full filesystem reported only `exit status 2`; the audit record now carries
  `No space left on device`.
- Upstream API responses and agent-API request bodies are read under a cap
  rather than buffered whole.

## [0.3.2] - 2026-08-22

### Fixed

- **Nested branch names were never gated.** Protected-ref patterns matched with
  `path.Match`, whose `*` does not cross `/`, so the default `refs/heads/*`
  checked `main` and `hotfix-1` but silently skipped `agent/task-1`,
  `feature/login`, `fix/bug` and `dependabot/...` - the naming coding agents
  actually use. Those pushes relayed to the upstream unchecked, wrote no audit
  row and no finding, and returned success, so there was nothing to notice; the
  auto-PR loop could never fire on them because it triggers on a rejected push.
  A trailing `/*` is now recursive and matches at any depth, so existing
  configs become correct without being edited. Ref-pattern validation uses the
  same matcher, so a pattern that loads cannot then fail to gate.

  **On upgrade, branches that previously passed will start being checked.** They
  were not passing because they were clean - they were never inspected. Expect
  findings on feature branches that pushed cleanly before.

- `gateway doctor` reports FAIL when a repo's protected-ref patterns cannot
  match nested branch names, naming the gap and the fix. An ungated push leaves
  no trace, so this is the only place the gap is discoverable.

## [0.3.1] - 2026-08-22

### Fixed

- Dashboard on narrow screens: the feed's decision timestamp no longer paints
  over the repo name, wide tables (repos, health, reports) scroll inside their
  own section instead of dragging the whole page sideways, and long upstream
  URLs wrap rather than forcing the repos table wider than the viewport. The
  narrow-screen feed layout now engages at 760px, the same width at which the
  nav rail collapses, instead of 600px.

## [0.3.0] - 2026-08-21

### Added

- **Six new built-in frames, 45 -> 51.** A files-that-never-belong-in-history
  pack, plus `commands/approved-registries-only` (flags Maven/Gradle, npm and
  pip dependency sources that bypass your approved registry mirror) and a PII
  frame that catches checksum-validated personal data in source and fixtures.
- **One-file VPS deploy.** `deploy/cloud-init.yaml` is pasted into the user-data
  field at server creation and boots the box with Docker, the gateway running, a
  firewall allowing only 22 and 2222 inbound, key-only SSH, and the setup token
  on the login banner. Documents the embedded-key options
  (`ssh_authorized_keys` / `ssh_import_id`) and warns that a server created
  without a key at creation time cannot be logged into.
- **`docs/connecting.md`**, a cross-platform guide for pointing a machine and
  its agents at the gateway (Windows/macOS/Linux, the agent workflow, running
  several agents, and a troubleshooting table).
- **Monthly commercial plan** at $10/month alongside the existing $99/year, and
  a licensing FAQ in `COMMERCIAL.md` covering the common free-vs-commercial
  cases.
- **Operations guidance** for credential lifecycle (rotation cadence table) and
  a compromise-recovery runbook, plus an optional outbound-allowlist hardening
  section.

### Changed

- The frames catalog hides browse axes with zero frames, so empty Platform and
  Framework columns no longer appear.
- The dashboard whitelist accepts custom linter IDs and surfaces load errors
  instead of rendering an empty panel.
- `security/no-hardcoded-credentials` no longer flags the documentation
  sentinels published in AWS examples.
- Registry frame docs and examples no longer name specific vendors.

### Fixed

- `gateway token` reports an actionable error when the auth database cannot be
  opened, instead of failing opaquely on first run.
- Install docs: the dashboard tunnel now uses the IPv4 `127.0.0.1` form (the
  `localhost` form silently connects to nothing over IPv6), the protected-refs
  default is stated correctly as `refs/heads/*`, the air-gapped build tag is
  matched to the tag `compose.yaml` pins, and the bare-metal push URL uses the
  absolute repos path rather than the `~/` shorthand.
- README no longer implies the gateway opens pull requests; it comments on
  existing ones.

### Security

- Bumped `golang.org/x/net` 0.54.0 -> 0.55.0, fixing an HTML-parser
  denial-of-service reachable through the HTML frames.

## [0.2.0] - 2026-06-30

### Added

- **Relay health surfacing.** The reconcile backstop now records each repo's
  relay outcome (last attempt/success, ok, redacted error, refs re-pushed) and
  the relay-service logs failures on state transition and reconciles once at
  startup, so a relay that silently stopped delivering (accepted pushes the
  upstream never received) is now visible: a Relay column on the Health page, a
  "relay failing" badge on the repo row, and a Relay check in `gateway doctor`.
  All read persisted status with no extra network calls.
- **`gateway doctor` preflight diagnostics.** A read-only command (and a new
  Diagnostics sub-tab on the Health page) that reports first-run and connect
  problems as OK/WARN/FAIL with fix hints: version/stale-binary, dashboard bind
  host + remote-tunnel hint, SSH gate reachability (probes both the container
  2222 and bare-metal 22 ports), authorized keys (listed by fingerprint + label,
  read from the dashboard's configured path), and per repo the bare-repo path +
  exact push URL, HTTPS upstream, credential, gated refs, active frames,
  notification state, and (live) upstream auth. Detects the bare-metal keys-path
  split (keys at sshd's `/home/git/.ssh/authorized_keys` while the dashboard
  manages a different file) and emits the bridge fix. Includes a copy-paste block
  to connect a dev box or agent. The CLI exits non-zero on any FAIL; the
  dashboard tab runs live connectivity checks only on request.
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
