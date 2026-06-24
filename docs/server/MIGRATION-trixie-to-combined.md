# Migrating from the trixie gateway image to the combined alpine image

**Audience:** Operators running the old `nimblegate-gateway` container (debian:trixie-slim, sshd-only) and wanting to move to the v0.1.0 combined image (`nimblegate`, alpine + s6-overlay, sshd + dashboard supervised together).

**Surgery, not maintenance.** Routine binary updates (`scp /usr/local/bin/nimblegate` on bare-metal hosts) still work. This runbook is only for the **one-time architectural cutover** between two different container images, not for routine version bumps.

---

## What survives the migration

Everything that matters to your existing devs and audit history:

- **Bare repos** under `gw-repos:/srv/gateway/repos/_repos/<name>.git` + activation symlinks at `/srv/gateway/repos/<name>.git`. Same layout in both images. Owner: `git:git` uid/gid 1000:1000 in both images.
- **Pre-receive hooks** reference `/usr/local/bin/nimblegate` by absolute path; the new image installs the binary at the same path, so the hooks fire on first push after cutover with no re-registration.
- **Per-repo policy** at `gw-cfg:/srv/gateway/cfg/<repo>/gateway.toml` + `appframes.toml` + `credential` + `audit.log`. Same paths, same formats; the new dashboard reads them unchanged.
- **SSH host keys** at `gw-ssh:/srv/gateway/ssh/hostkeys/ssh_host_{rsa,ecdsa,ed25519}_key`. The new image's init script honors these if present and does NOT regenerate. **Dev-box `known_hosts` trust survives the cutover: no TOFU warning to your developers.**
- **Authorized SSH keys** at `gw-ssh:/srv/gateway/ssh/authorized_keys`. New sshd_config points at the same absolute path. All existing dev keys keep working.
- **Existing dev-box `git remote` URLs**: if they use the absolute form `ssh://git@<gateway>/srv/gateway/repos/<repo>.git`, they keep working unchanged. (The new short form `git@<gateway>:<repo>.git` is the documented quickstart URL, but it's an addition, not a replacement.)

## What's new in the combined image

- **Dashboard now requires login** (single-admin auth, `--auth=setup-token` default). v0.1.0 dashboard auth didn't exist in the trixie image, so the cfg volume carries no auth state: **first start prints a one-time setup token you'll need to claim**. After claiming you log in once per 12-hour session.
- **Dashboard now runs inside the container.** Trixie image: sshd only, dashboard ran as a systemd service on the host (`nimblegate-dashboard.service`). Combined image: both supervised by s6-overlay inside the container. After cutover you'll **stop the host-side `nimblegate-dashboard.service`**, otherwise both will fight for port 7900.
- **Welcome banner + setup token in `docker logs`**: the new image prints a first-run banner; the setup token line follows under `[nbg-setup]`.
- **Operator helper scripts**: `docker exec <container> nbg-status | nbg-restart | nbg-logs | nbg-reset | nbg-regen-keys`.
- **Asymmetric port binding by default**: `-p 127.0.0.1:7900:7900` for the dashboard (loopback-only: auth is on, but localhost is belt-and-braces), `-p 2222:22` for sshd. The old image only exposed sshd via `-p 2222:22`; the dashboard was a host service. Adjust your firewall rules accordingly if anything was pointing at the host's port 7900 directly.
- **Container name changes**: `nimblegate-gateway` → `nimblegate`. The new `deploy/gateway/docker-compose.yml` uses the new name; if you have orchestration scripts/firewall hooks/log-tail scripts hard-coded to `nimblegate-gateway`, update them.

---

## Bare-repo layout check (do this FIRST: required if your gateway predates 2026-05-30)

The combined image's dashboard discovers registered repos by scanning `<reposRoot>/_repos/`. If your gateway was set up before commit `616d73e` (2026-05-30, "MigrateToSymlinkLayout: one-time legacy → _repos/ + symlinks"), your bare repos live at `<reposRoot>/<name>.git/` directly with no `_repos/` subdirectory. The dashboard won't find them.

Convert with the binary's built-in migrator BEFORE you do anything else:

```sh
# As the git user inside the OLD container (or directly on the host for bare-metal):
ssh root@<gateway> 'docker exec -u git nimblegate-gateway \
  nimblegate gateway migrate-layout \
  --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos'
```

The migrator is idempotent: running it on an already-converted layout is a no-op. After conversion the repo directory will look like:

```
/srv/gateway/repos/
  _repos/
    <name>.git/           # the real bare repo
  <name>.git -> _repos/<name>.git    # activation symlink
```

If you're not sure when the gateway was set up, run the migrator anyway; idempotent means it's safe.

## Pre-flight (do these before stopping anything)

```sh
# 1. Snapshot. If on Proxmox: take a VM snapshot of the gateway host. If on bare-metal docker:
#    back up the three volumes. Replace <gateway> with your host alias.
ssh root@<gateway> 'docker run --rm \
  -v gw-repos:/from/repos -v gw-cfg:/from/cfg -v gw-ssh:/from/ssh \
  -v /root/migrate-backup:/to busybox \
  sh -c "cd /from && tar czf /to/gw-state-$(date -u +%Y%m%dT%H%M%SZ).tar.gz repos cfg ssh"'

# 2. Baseline - confirm the gateway is currently healthy so you can compare after cutover.
ssh root@<gateway> 'docker ps --filter name=nimblegate-gateway --format "{{.Names}} {{.Status}}"'
ssh root@<gateway> 'docker exec -u git nimblegate-gateway nimblegate version'
ssh root@<gateway> 'ls -la /srv/gateway/cfg | head -10'                   # repo count
ssh root@<gateway> 'wc -l /srv/gateway/ssh/authorized_keys'               # dev-key count

# 3. From a dev box that already pushes to this gateway: do one push to a non-protected
#    branch (or a no-op rebase + force-push to a sandbox repo). Confirm BLOCK + accept
#    flow both work today. This is the baseline you'll re-test after cutover.
```

If step 3 fails BEFORE you've touched anything, stop here: you're chasing a different problem than the migration.

---

## Cutover

### 1. Stop the host-side dashboard (if it was running outside the old container)

The trixie image only contained sshd; the dashboard ran on the host as `nimblegate-dashboard.service`. After cutover the new container runs the dashboard internally. To prevent the two binding the same port:

```sh
ssh root@<gateway> '
  systemctl is-active nimblegate-dashboard && {
    systemctl stop nimblegate-dashboard
    systemctl disable nimblegate-dashboard
    echo "host-side dashboard stopped"
  } || echo "no host-side dashboard found - nothing to stop"
'
```

The systemd unit file itself can stay on disk (`docs/server/README.md` Option A still references it for bare-metal deploys); we just disable the running instance.

### 2. Stop the old container, leave volumes in place

```sh
ssh root@<gateway> '
  cd /path/to/nimblegate &&                               # adjust to your repo clone
  docker compose -f deploy/gateway/docker-compose.yml down
'
```

`down` (not `down -v`) removes the container but keeps the named volumes. Verify:

```sh
ssh root@<gateway> 'docker volume ls | grep -E "gw-(repos|cfg|ssh)"'
# Expected: three volumes still present.
```

### 3. Pull the latest repo + rebuild from the new compose file

```sh
ssh root@<gateway> '
  cd /path/to/nimblegate &&
  git fetch && git checkout feat-container-eval-ready &&   # or whatever branch carries v0.1.0
  docker compose -f deploy/gateway/docker-compose.yml build &&
  docker compose -f deploy/gateway/docker-compose.yml up -d
'
```

The new compose file points at the root `Dockerfile` (single combined image), names the container `nimblegate`, and uses the asymmetric port binding shown above.

### 4. Read the setup token from logs and claim the admin account

```sh
ssh root@<gateway> 'docker logs nimblegate 2>&1 | grep nbg-setup | tail -1'
# Output: [nbg-setup] first-run setup token: XXXX-XXXX-XXXX-XXXX - visit /setup to claim
```

Open `http://<gateway-loopback-or-tunnel>:7900/setup`, paste the token, pick admin username + password (≥8 chars). The token file is deleted after a successful claim; `/setup` then 404s.

If the gateway is on a separate host you can't reach via localhost: SSH-forward the port for the claim, e.g. `ssh -L 7900:127.0.0.1:7900 root@<gateway>`.

---

## Verify

Tick all of these before declaring the migration done:

```sh
# A. Services up under s6 supervision
ssh root@<gateway> 'docker exec nimblegate nbg-status'
# Expect both sshd and dashboard "up", non-zero uptime, repo count matches your baseline,
# auth = "admin claimed".

# B. Dashboard reachable + audit history visible
curl -s http://<gateway-or-tunnel>:7900/feed | grep -c 'data-feedsev'
# Non-zero - old audit lines render in the new feed.

# C. Existing dev key still authenticates
ssh -i ~/.ssh/<dev-key> -p 2222 git@<gateway>      # from a dev box
# Expected: "fatal: Interactive git shell is not enabled" (git-shell rejecting interactive
# - auth itself succeeded). Same outcome as before cutover.

# D. Existing dev-box `git remote` URLs still push
git push origin main      # from a dev clone that pushed yesterday
# Expected: same accept/reject behavior as before. Audit log appended.

# E. New BLOCK fires
# From a sandbox dev clone, add a deliberately leaky commit and push to a
# protected branch. Confirm BLOCK is rejected synchronously. Use an AWS
# access-key SHAPE - the frame's regex is \bAKIA[0-9A-Z]{16}\b (exactly 20
# chars). A handy test value: AKIAZZZZMIGRATION007 (4 + 16, never issued).
# Don't use the docs sentinel AKIAIOSFODNN7EXAMPLE - it's whitelisted as a
# canonical fake.
```

If any of these fail, see Rollback.

---

## Rollback

The volumes are intact; rollback is just "stop new, start old":

```sh
ssh root@<gateway> '
  cd /path/to/nimblegate &&
  docker compose -f deploy/gateway/docker-compose.yml down &&
  git checkout <pre-migration-commit> &&
  docker compose -f deploy/gateway/docker-compose.yml up -d --build
'

# Re-enable the host-side dashboard service if you stopped it
ssh root@<gateway> 'systemctl enable --now nimblegate-dashboard'
```

The new image will have written two files to the cfg volume that the old image ignores: `_auth.db` (SQLite DB with your claimed admin), `_setup_token` (deleted on claim, may not exist). They're inert to the old image; no cleanup required for rollback. If you re-attempt the migration later, the auth DB is still there: first start of the new image will see the admin already claimed and won't print a new setup token.

---

## Optional post-migration cleanup

- **Old image:** `docker image rm nimblegate-gateway` once you've confirmed a week of stable operation.
- **Backup tar:** delete `/root/migrate-backup/gw-state-*.tar.gz` after the same observation window.
- **Documentation:** if you have an operator runbook of your own that references `nimblegate-gateway` (the old container name), update those references to `nimblegate`.
- **DNS / reverse-proxy config:** if anything was pointed at `nimblegate-gateway` by container name on a shared docker network, retarget at `nimblegate`.

---

## What you can tell your devs

Nothing, unless you want to. Their existing `git remote` URLs keep working. If they ask about the new short form (`git@<gateway>:<repo>.git`), point them at the root README quickstart. The cutover is invisible to them apart from the audit-log timestamp gap during the swap.

If a dev opens the dashboard at `http://<gateway>:7900/` and bounces on the login screen, that's the new auth in action; they need an account, which only an admin can create (single-admin v0.1.0; multi-user is commercial-tier).

---

## Dry-run validation (2026-06-02)

This runbook was end-to-end exercised against a throwaway old-image instance before being promoted to canonical:

- Built the OLD trixie image from commit `b309aad` (the parent of `0cc9858`, T1 of the rebuild).
- Seeded three fresh volumes (`migr-repos`, `migr-cfg`, `migr-ssh`) with realistic production state: one repo registered via the OLD CLI (`nimblegate gateway add`), one authorized dev key, one accept event in the audit log.
- Walked every step of the runbook in order. All seven verify gates went green: services up under s6 supervision, dashboard reachable, old audit-log timestamps rendered in the new feed, existing dev key authenticated, existing absolute-path `ssh://…/srv/gateway/repos/<repo>.git` URL pushed successfully, the auth setup token claimed cleanly, and a follow-up push with a 20-char `AKIA[0-9A-Z]{16}` value triggered `BLOCK security/no-hardcoded-credentials`.
- Rollback path: stopped the new container, started the OLD image back on the same volumes. The new-image artifacts (`_auth.db`, `_auth.db-shm`, `_auth.db-wal`) live in `cfg/` and are inert to the old binary; reads + appends to existing audit logs continued working; the OLD CLI registered a new repo on the same volumes without error.

Two surprises surfaced during the dry-run, both folded into the steps above:

1. The OLD trixie image's `useradd --create-home git` leaves the `git` account locked (no password), so sshd rejects logins with "account is locked". Operators who used the **container** path in production had to apply `sed -i -E 's/^(git:)[!*]+/\1*/' /etc/shadow` manually; operators who used the **bare-metal** path didn't hit this. The new alpine image fixes it at build time. Mentioned here so it doesn't surprise anyone re-validating against an OLD container.
2. AKIA values for the "BLOCK fires" verify gate must be **exactly 20 chars** (`AKIA` + 16 of `[0-9A-Z]`). Off-by-one values (19 or 21 chars) silently pass the check, masquerading as a broken gate. The runbook's verify step (gate E above) now states this explicitly.
