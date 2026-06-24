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
