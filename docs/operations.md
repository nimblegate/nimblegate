# Operations: running the gateway day to day

Keeping a nimblegate gateway healthy: watching it, updating it, backing it up,
and recovering it. None of this is needed to *use* the gate. It's the
sysadmin-side reference.

## Contents

- [Operator visibility](#operator-visibility)
- [Updating the server](#updating-the-server)
- [Backup and recovery](#backup-and-recovery)
- [Forgot the admin password](#forgot-the-admin-password)
- [Self-maintaining storage](#self-maintaining-storage)
- [Hardening: outbound allowlist (optional)](#hardening-outbound-allowlist-optional)
- [Credential lifecycle: what lives where, when to rotate](#credential-lifecycle-what-lives-where-when-to-rotate)
- [If you suspect the gateway is compromised](#if-you-suspect-the-gateway-is-compromised)

---

## Operator visibility

The container runs sshd and the dashboard under
[s6-overlay](https://github.com/just-containers/s6-overlay); both auto-restart
on failure with distinctive log lines. Useful greps:

```bash
docker logs nimblegate | grep nbg-supervise    # service exit + restart events (crash-loop visibility)
docker logs nimblegate | grep nbg-setup        # current first-run setup token (only when no admin exists)
docker compose logs nimblegate | grep "notification daemon"   # Auto-PR delivery errors
```

A handful of clustered `nbg-supervise` lines = healthy auto-recovery; a flood in
a short window = crash loop, worth investigating.

In the dashboard:

- **`/feed`**: every push + decision, live, with per-row notification status.
- **`/health`**: notification queue depth, last drain, deadletter count, daemon
  status, delivery success rate.
- **Settings → System**: install info + the running build SHA (confirm what's
  actually running here after an update).

More gotchas, a push that silently never reaches the gateway (a stale client
shim), a repo that gates fine but is missing from `what_changed`, the
`master`-vs-`main` / bare-repo ownership pitfalls, are in
[`docs/troubleshooting.md`](troubleshooting.md).

---

## Updating the server

A new release is just a fresher image: pull and re-create the container. The
three persistent volumes (repos, cfg, ssh) carry over, so config, credentials,
audit logs, and authorized keys are unchanged.

```bash
docker compose pull            # fetch the new image
docker compose up -d           # re-create against the new image
docker logs nimblegate | tail  # confirm the new version started
```

**`docker restart nimblegate` is not enough**: it restarts the process but keeps
the old image layers, so the on-disk version doesn't change. Always use
`docker compose up -d` after `pull`. Confirm the running build from
**Settings → System** (the build SHA there is the live binary).

**Air-gapped update:** build + `docker save | gzip` on a connected machine,
transfer, `docker load`, then `docker compose up -d --force-recreate` (same flow
as the offline install in [getting-started](getting-started.md#step-1-install-the-gateway)).

**Bare-metal install** (apt + systemd): the release archive is
`nimblegate_<version>_<os>_<arch>.tar.gz` containing a `nimblegate` binary; the
systemd unit is `nimblegate-dashboard.service`:

```bash
tar -xzf nimblegate_*_linux_amd64.tar.gz nimblegate
sudo install -m 0755 nimblegate /usr/local/bin/nimblegate
sudo systemctl restart nimblegate-dashboard
```

There is no auto-update and no phone-home; watch the
[releases page](https://github.com/nimblegate/nimblegate/releases).

---

## Backup and recovery

All gateway state lives in three Docker volumes: back them up together and you
can fully reconstruct the gateway on a fresh host:

- **`nimblegate-repos`** → `/srv/gateway/repos/`: the bare repos the gateway
  forwards upstream. Recoverable from upstream if lost, but a backup saves a
  re-clone of every project.
- **`nimblegate-cfg`** → `/srv/gateway/cfg/`: **the irreplaceable one.** Per-repo
  `gateway.toml` (upstream credential + notification settings), `appframes.toml`
  (frame selection), the whitelist, `audit.log` (decision history), the
  Auto-PR queue/deadletter/state, and `_auth.db` (admin login).
- **`nimblegate-ssh`** → `/srv/gateway/ssh/`: sshd host keys (so dev machines
  don't re-warn after restore) + `authorized_keys`.

Simplest backup is a stop-tar-start cycle:

```bash
docker compose stop
sudo tar czf nimblegate-backup-$(date -u +%Y%m%d).tar.gz \
  /var/lib/docker/volumes/nimblegate-repos \
  /var/lib/docker/volumes/nimblegate-cfg \
  /var/lib/docker/volumes/nimblegate-ssh
docker compose start
```

Recovery on a fresh host: install Docker, restore the tarball to the same volume
paths, drop your `compose.yaml` in place, and `docker compose up -d`. Everything
comes back: login, repos, keys, queued notifications. Upstream credentials live
inside `gateway.toml`, so **treat the backup as sensitive**: encrypt at rest and
store off-host.

---

## Forgot the admin password

Single-admin auth has no email reset. Since you control the host, the recovery
path is to wipe the auth database and re-claim via setup token. This preserves
repos, SSH host keys, authorized keys, and audit logs. Only the admin
username + password are reset.

```bash
docker exec nimblegate sh -c 'rm -f /srv/gateway/cfg/_auth.db /srv/gateway/cfg/_auth.db-shm /srv/gateway/cfg/_auth.db-wal'
docker restart nimblegate
docker logs nimblegate | grep nbg-setup
# [nbg-setup] first-run setup token: XXXX-XXXX-XXXX-XXXX - visit /setup to claim
```

Visit `/setup` with the new token to pick a fresh username + password.
Email-based reset is on the commercial roadmap for the multi-user tier.

---

## Self-maintaining storage

The gateway is a relay, **not** a backup; your real upstream is. A built-in
weekly maintenance loop runs `git gc --auto` per bare repo so pack files don't
accumulate forever, and prunes old deadletter records. Tunable in
`<policy-root>/gateway.toml` `[maintenance]`; defaults are sane and most
operators never touch it.

### Where the gate stages its scan

To check a push, the gate exports the pushed tree into a temporary directory,
scans it, and deletes it. Scan-on-first-push and the dashboard's regex preview
stage the same way. That directory defaults to `_scan-tmp/` under your
`--repos-root`, so it is disk-backed and sized with the repos themselves. It is
deliberately **not** `/tmp`, which is a RAM-backed tmpfs on most distributions -
staging a full copy of every in-flight push there is how a large repo turns a
busy moment into an out-of-memory kill.

Point it elsewhere (a dedicated fast disk, for instance), and tune the two
resource limits that go with it, in `<policy-root>/gateway.toml`:

```toml
[gateway]
scan_tmpdir    = "/var/lib/nimblegate/scan-tmp"
max_tree_bytes = 2147483648   # 2 GiB, 0 = unlimited
scan_timeout   = "5m"         # 0 = no deadline
```

/health shows the **effective** staging directory and the free space on its
filesystem, so you can confirm a `scan_tmpdir` actually took. If the configured
path cannot be created the gateway falls back to `$TMPDIR` and keeps gating -
that fallback is recorded as a `scan-staging-fallback` event and warned about on
/health, because it silently restores the RAM staging the setting exists to
prevent.

`max_tree_bytes` caps what one push may expand to on disk. It is separate from
`max-input-size`, which caps the compressed pack a repo will accept: the two are
unrelated, because a file of 10 GB of zeros is a tiny object that still extracts
in full. `scan_timeout` bounds how long one push's frame run may take - every
enabled frame walks the staged tree itself, so cost grows with repo size and
frame count. Exceeding either is treated as a scan failure: rejected under
enforcement, relayed-and-signalled under observe, exactly like a staging failure.

If the gate cannot stage the tree - the filesystem is full, the directory is
unwritable - the push is **rejected**, because a tree that failed to extract
scans clean and would otherwise let anything through. The pushing client sees
an ordinary policy rejection; the real cause goes to the audit log, the
`scan-failed` event, and a warning line on `/health`. In observe mode the push
is relayed unscanned (observe never rejects), which is exactly when that
`/health` line is the only thing telling you scanning has stopped.

### Sizing, memory pressure, and swap

/health shows **Memory pressure** from the kernel's PSI counters: the share of
wall-clock time tasks spent stalled waiting on memory over the last 10 seconds.
Watch that number rather than free memory - a box with little free memory and no
reclaim stalls is healthy, and one with headroom that reclaims constantly is
not. It warns at `some` >= 10% or `full` >= 5%, which is well before the kernel
starts killing processes.

If it climbs under an agent swarm, the box is undersized for the push rate. Swap
helps only as a shock absorber: it turns an OOM kill into thrashing, so pushes
that took seconds take minutes and the queue behind them grows. Keep a small
swap or zram with `vm.swappiness=10` so the OOM killer does not reap sshd or the
daemon at random, but treat a rising PSI number as "add RAM or reduce
concurrency", not as something swap fixes. On systemd hosts a `MemoryHigh=` on
the nimblegate units is the better lever: it makes the kernel reclaim and
throttle that unit before the system-wide OOM killer picks a victim.

Staging is the other half of sizing. Each in-flight push holds one full copy of
its tree under `scan_tmpdir` until its hook returns, so peak disk is roughly the
largest repo times the number of simultaneous pushes.

---

## Hardening: outbound allowlist (optional)

The gateway's legitimate outbound traffic is small and known: HTTPS to your
upstream git host(s), DNS, NTP, and OS/package updates. Everything else it
might ever send is, by definition, not something you configured. Restricting
egress is defense-in-depth: even a hypothetically compromised gateway then
has nowhere to exfiltrate to. Inbound is already covered (22 + 2222 only,
see the cloud-init deploy); this section is the outbound half.

This is optional. The gateway is safe without it - the point is limiting
blast radius further on boxes where that matters (regulated environments,
strict network policies).

**Bare-metal install** (gateway binary runs as a host process) - ufw:

```bash
ufw default deny outgoing
ufw allow out on lo
ufw allow out 53          # DNS (scope to your resolver IP if it is static)
ufw allow out 123/udp     # NTP
ufw allow out 443/tcp     # HTTPS: upstream relay + PR comments + OS updates
```

Port-level (443 to anywhere) rather than per-host is the honest, robust
version: git hosts publish rotating IP ranges (GitHub's change; see their
`/meta` API), and pinning them turns hardening into a maintenance chore that
eventually breaks relaying. Port-level already removes every non-HTTPS
exfiltration path. If your upstream is a fixed internal host (self-hosted
Gitea/GitLab), tighten to its address:

```bash
ufw allow out to <upstream-ip> port 443 proto tcp
```

**Docker install:** host `ufw` outgoing rules do NOT govern container
traffic - Docker routes it through the FORWARD chain, which ufw's outgoing
policy never sees. Container egress is filtered in the `DOCKER-USER` chain
instead. Two details matter: match only container-ORIGINATED packets
(`-i <bridge>`), or the final DROP also kills inbound pushes to 2222; and
INSERT the rules (`-I`), because Docker pre-installs a `RETURN` at the end
of the chain that appended rules would sit behind, never evaluated:

```bash
# BR = docker0, or your compose network's bridge (docker network ls → br-<id>)
BR=docker0
iptables -I DOCKER-USER -i $BR -j DROP
iptables -I DOCKER-USER -i $BR -p tcp --dport 443 -j RETURN
iptables -I DOCKER-USER -i $BR -p udp --dport 53 -j RETURN
iptables -I DOCKER-USER -i $BR -m state --state ESTABLISHED,RELATED -j RETURN
```

(Inserted in reverse so the final order reads: established / DNS / HTTPS /
drop-the-rest. Inbound pushes arrive on the host interface, not `$BR`, so
they pass untouched. Persist with your distro's iptables-persistence
mechanism; `DOCKER-USER` rules survive Docker restarts by design.)

**What to expect after enabling:** pushes relay, PR comments post, `docker
pull` and OS updates work (all HTTPS). Anything that breaks is a protocol
you forgot you depended on - add it deliberately, one rule at a time.

---

## Credential lifecycle: what lives where, when to rotate

Everything secret on the gateway, in one view:

| Secret | Lives at rest in | Rotate when |
|---|---|---|
| Upstream PAT (per repo) | `<policy-root>/<repo>/gateway.toml` (cfg volume) | On your git host's expiry cadence; immediately on any compromise suspicion |
| Admin login | `_auth.db` (cfg volume, hashed) | On personnel change; recovery via setup-token reset (above) |
| Agent API tokens | `_auth.db` (cfg volume, SHA-256 hashes only) | Mint one per consumer; revoke by ID when a consumer is retired |
| Webhook secret (HMAC/bearer) | `gateway.toml [notification]` | Together with the receiver; it is a shared secret |
| SSH host keys | ssh volume | Practically never (rotating re-warns every dev machine) |
| Pusher public keys | ssh volume / dashboard Keys page | These are PUBLIC keys - nothing to leak. Remove entries when a person or agent is offboarded |

Three practical consequences:

- **The cfg volume is the sensitive one.** Backups contain it - encrypt them
  at rest and store off-host (see Backup and recovery).
- **Private keys never live on the gateway.** Developers' and agents' private
  SSH keys stay on their machines; the gateway holds only the public halves.
  A gateway compromise does not compromise any dev machine.
- **One credential per consumer beats one shared credential.** Per-repo PATs,
  per-agent pusher keys, per-consumer API tokens - each makes revocation
  surgical instead of a company-wide rotation event.

---

## If you suspect the gateway is compromised

Assume-breach runbook. What an attacker on the gateway box has, worst case:
the upstream PATs (write access to the repos the gateway relays), the bare
repo mirrors (your source code), the audit log, and the webhook secret. What
they do NOT have: any developer's private SSH key (only public keys are
stored), access to dev machines, or anything beyond the repos the PATs are
scoped to - which is why per-repo fine-grained PATs matter.

In order:

1. **Revoke the upstream PATs at the git host** (GitHub/GitLab/Gitea settings,
   not on the gateway). This kills the stolen credential's value instantly,
   even before you touch the box. Relaying stops until you issue new ones -
   that is the point.
2. **Freeze pushes:** stop the gateway (`docker compose stop` / stop the
   systemd unit) or block 2222 at the firewall.
3. **Verify upstream history integrity.** The gateway relays byte-for-byte,
   so compare: branch tips upstream vs your team's local clones
   (`git ls-remote` both sides). Diverged or unexpected refs upstream =
   escalate to your git host's support + treat as a code-integrity incident.
4. **Review the audit log** (`audit.log` in the cfg volume, or the dashboard
   feed from a restored copy): what relayed during the suspicious window, from
   which keys.
5. **Rebuild, don't clean.** Fresh host or re-provision via
   `deploy/cloud-init.yaml`, restore the three volumes from a KNOWN-GOOD
   backup (or re-register repos from scratch - they re-sync from upstream),
   issue NEW PATs, set a new webhook secret, reset the admin login.
6. **Re-key the pushers:** remove all pusher keys in the dashboard and have
   each developer/agent re-submit. Their keys were never exposed, but this
   guarantees the authorized set matches reality after the incident.
7. **Rotate agent API tokens** (`nimblegate gateway token list` / `revoke`,
   then mint fresh ones).

Postmortem material is built in: the audit log is append-only and the
decision history survives in backups, so "what went through the gate and
when" is answerable after the fact - that record is the reason several of
these steps are possible at all.
