# nimblegate policy gateway: server guide

The **server** side of nimblegate is one subcommand of the same binary: `nimblegate gateway`. It runs on a machine the dev (and the dev's agent) **cannot otherwise reach**, and gates every push between the dev machine and the true upstream. The desktop/CLI side of nimblegate is documented in the [top-level README](../../README.md); this guide is everything you need to **deploy, update, and operate the gateway**.

> **The security boundary is the separate host, not the software.** The gate is un-bypassable only because (1) the dev machine holds push creds to the *gateway* and not to the upstream, and (2) the upstream credential lives *only* on the gateway. A gateway running on the dev's own machine, where their agent can edit files or `docker exec` in, is not a boundary.

---

## How it works

- Devs set `origin` to the gateway and push as if to an ordinary git remote; nothing is installed on their machine or on the upstream.
- A `pre-receive` hook runs the nimblegate engine against the **pushed tree** using **gateway-held** policy (the pushed `.appframes/` / `appframes.toml` / `.appframes-ignore` are ignored, so a push can't weaken its own gate) and rejects synchronously on an open BLOCK to a protected branch (fail-closed).
- A `post-receive` hook relays accepted refs to the true upstream using a credential held **only** on the gateway.
- **Gating** itself needs no long-running daemon, just `sshd` + `git` + the `nimblegate` binary + bare repos carrying those hooks. Default gates protected branches only; feature branches flow freely; per-repo disable-able. To gate **every** ref (so feature branches are checked + fail-closed too, not relayed unfiltered), register with `gateway add --gate-all-refs` (or set `gate-all-refs = true` in the repo's `gateway.toml`).
- The **dashboard** runs as its own service ([Use §4](#4-monitor-tune-and-author-checks-web-ui)) and additionally hosts the **Auto-PR delivery loop**, so if you use PR comments / webhooks, that service must be running (it's not monitoring-only).

---

## Deploy

Two options. **Bare-metal on an isolated VM/CT is the recommended path**; the container is for reproducible packaging where you already have Docker on a separate box.

### Option A: bare-metal (recommended)

Full worked runbook (Proxmox trixie LXC, dev → gateway → Gitea, including the lock-down step that makes the gate un-bypassable): **[`SETUP-proxmox-trixie.md`](SETUP-proxmox-trixie.md)**. In short:

1. On the box: `apt install -y git openssh-server ca-certificates`, enable `ssh`, create a `git` user with `git-shell` as its login shell, and `mkdir` the data dirs (`/srv/gateway/repos`, `/srv/gateway/cfg`).
2. On the dev machine: build the static binary and copy it over (see [Update](#update) for the exact command), then authorize each dev's SSH **public** key in `/home/git/.ssh/authorized_keys`.
3. Register repos and write policy (see [Use](#use)).

`git-shell` means an authorized key grants git push/fetch only, never an interactive shell.

### Option B: container

A `debian:trixie-slim` image with `git` + `openssh-server` + the `nimblegate` binary (multi-stage build; the binary is static). Artifacts live in [`deploy/gateway/`](../../deploy/gateway/). Run it **only on a separate box**; see the boundary note above.

```sh
# from the repo root:
docker compose -f deploy/gateway/docker-compose.yml up -d --build
```

Three persistent volumes hold the state: `/srv/gateway/repos` (bare gated repos), `/srv/gateway/cfg` (per-repo policy + credentials + audit log), `/srv/gateway/ssh` (host keys + the git user's `authorized_keys`). Add dev keys by appending to the `ssh` volume's `authorized_keys` and restarting; register repos with `docker exec -u git nimblegate-gateway nimblegate gateway add …`.

### Sizing (Debian host)

- **Floor** (small team, a few repos, frames + go-vet/shellcheck): **1 vCPU, 1 GB RAM, ~20 GB disk.**
- **Comfortable**: **2 vCPU, 2 GB RAM, ~40 GB disk**; pick this if you enable node-based linters (eslint/tsc/svelte-check spike RAM) or host large repos.
- CPU/RAM scale with per-push work (idle between pushes); **disk scales with bare-repo sizes** (full clones, ×~2–3), so size it to your repos.

---

## Update

The binary is static (`CGO_ENABLED=0`), so it runs on any host Debian regardless of build host. Build on the dev machine, copy it over, and **verify the deploy landed**:

```sh
cd ~/nimblegate
export PATH="$HOME/go/bin:$PATH"
CGO_ENABLED=0 go build -ldflags="-X nimblegate/internal/version.Version=$(git rev-parse --short HEAD)" -o bin/nimblegate ./cmd/nimblegate
scp bin/nimblegate root@<gateway>:/usr/local/bin/nimblegate
ssh root@<gateway> 'chmod 755 /usr/local/bin/nimblegate && nimblegate version'   # must print the commit you just built
```

The hooks reference `/usr/local/bin/nimblegate` by path, so replacing the binary is the whole update: **no re-registration**. The `version.Version` label is set to the commit SHA on purpose: `nimblegate version` on the gateway then tells you exactly which build is live, so "is the gateway running my new code?" is a one-command answer. A local build **never** reaches the gateway on its own; you must `scp` it; that gap is the usual cause of a gateway that looks "stale."

### Update from Gitea (server pulls + builds)

If you publish source to a git host (e.g. Gitea) and prefer the gateway to pull and build it itself instead of receiving an `scp`'d binary, the end state is identical: the same binary swap + the same verify. Only the delivery differs.

One-time on the gateway box: install the Go toolchain (match `go.mod`, currently **Go 1.25**) and `git`, then clone the source with a **read-only** deploy key scoped to just this repo. Never put your dev push key or any write-capable credential on the gateway; the box only ever *reads* source.

```sh
apt install -y git ca-certificates          # plus a Go 1.25 toolchain
git clone git@<git-host>:nimblegate/nimblegate.git ~/nimblegate-src
```

Each update, after you push to the git host from your workstation:

```sh
cd ~/nimblegate-src
git pull --ff-only origin main
export PATH="$HOME/go/bin:$PATH"
CGO_ENABLED=0 go build -ldflags="-X nimblegate/internal/version.Version=$(git rev-parse --short HEAD)" -o /usr/local/bin/nimblegate ./cmd/nimblegate
chmod 755 /usr/local/bin/nimblegate
nimblegate version                           # must print the commit you just pulled
```

Then restart the two long-running services so they drop the old binary they hold in memory (the pre-/post-receive hooks exec the binary fresh per push, so they need no restart):

```sh
systemctl restart nimblegate-dashboard
systemctl restart nimblegate-relay           # only if you run the privilege-separated relay
```

**Trade-off vs. build-on-dev-then-`scp` (above):** pulling on the gateway keeps your publish flow uniform (workstation → git host → server) but adds a Go toolchain, a source checkout, and a read credential to the gateway box. The `scp` path keeps the gateway minimal (no Go, no source, no git-host credential) at the cost of building elsewhere and copying the artifact. Both end at the same binary at `/usr/local/bin/nimblegate`.

Container: rebuild and recreate (`docker compose … up -d --build`); the volumes persist. With the pull-and-build flow that becomes: `git pull --ff-only origin main` in the source checkout, then `docker compose -f deploy/gateway/docker-compose.yml up -d --build`, then `docker exec nimblegate nimblegate version` to confirm the new SHA.

**Migrating from the older trixie / sshd-only container image to the v0.1.0 combined alpine + dashboard image is a one-time cutover, not a routine update.** See [`MIGRATION-trixie-to-combined.md`](MIGRATION-trixie-to-combined.md) for the step-by-step runbook (pre-flight backup, container-name change, host-side dashboard service shutdown, setup-token bootstrap, rollback path).

---

## Use

### 1. Register a gated repo

Run **as the git user** so the bare repo is git-owned and its hooks run on push:

```sh
sudo -u git nimblegate gateway add \
  --name myrepo \
  --upstream "<true-upstream-url>" \
  --protect refs/heads/main \
  --policy-root /srv/gateway/cfg \
  --repos-root /srv/gateway/repos
```

This creates `/srv/gateway/repos/myrepo.git` with the gate hooks (pointing at `/usr/local/bin/nimblegate`) and starts it empty. For an **HTTP** upstream (e.g. a LAN Gitea), put the push token in the upstream URL (`http://USER:TOKEN@host:3000/org/myrepo.git`) because the separate credential file is currently injected only for `https://`; it lives in `/srv/gateway/cfg/myrepo/gateway.toml` on the box the agent can't reach, which is acceptable on a trusted LAN. The cleaner option is an `ssh://` upstream with the git user's key registered as an upstream **deploy key** (no token in any file), but two prerequisites the HTTP path doesn't have: (1) the gateway's relay user must have the upstream's **SSH host key** in its `known_hosts` (`ssh-keyscan -H <upstream-host> >> ~/.ssh/known_hosts`), or the first relay fails `Host key verification failed` and the push silently never reaches the upstream; (2) the gateway's own key, not a dev's, must be the authorized deploy key. A PAT in the URL is ignored for `ssh://` upstreams.

> **Credential hygiene.** That push token sits in **clear text** in `gateway.toml`, so treat the gateway as the box holding that secret. Use a **dedicated, minimally-scoped token** (a gateway/CI *service account* with push access to **only** that repo), never your personal account or a broad all-repo token. When you remove a repo (`rm -rf /srv/gateway/cfg/<repo>`) or rotate, **revoke the old token in Gitea** (Settings → Applications → Access Tokens): deleting the file does **not** invalidate the token. An `ssh://` upstream with a per-repo deploy key avoids storing any token at all and is preferred where practical.

**Enforce vs observe.** A repo has three modes, set at registration and stored in `gateway.toml`:

| Mode | flag | behaviour |
|------|------|-----------|
| **enforce** (default) | *(none)* | check + record; a BLOCK finding **really rejects** the push (exit non-zero, nothing relayed): the dev fixes locally and pushes again. |
| **observe** | `--observe` | check + record, but a would-block is **relayed anyway**, and the push output is **fully silent**, indistinguishable from a repo with no gateway in front. The decision is logged `accept=true, observed=true` so the dashboard shows the catch without stopping the push; only the operator sees it. The silence is deliberate: an agent that knows it's being observed adapts to the observer instead of behaving naturally, which defeats the calibration purpose of observe mode. |
| **pass-through** | `--disabled` | no checking at all; pure relay. |

Observe mode is the recommended way to onboard a repo for real use: route everything through the gateway with zero push-behaviour risk, watch the feed/`/stats` to calibrate the catch rate and false positives, and **flip to enforce per-repo** once a process could push without a human watching, by setting `observe = false` in `/srv/gateway/cfg/<repo>/gateway.toml` (no re-`add` needed).

### 2. Write the gateway-held policy

This is the **source of truth** for what gets enforced; the pushed `.appframes/` is ignored. The `enabled` list holds **frame IDs** (run `nimblegate list` to see them all, or `nimblegate list --kit core` for the catastrophic-prevention starter set). Write a minimal policy directly:

```sh
sudo -u git sh -c 'printf "[frames]\nenabled = [\"security/no-hardcoded-credentials\", \"git/no-force-push-main\", \"commands/curl-pipe-shell\"]\n" > /srv/gateway/cfg/myrepo/appframes.toml'
```

Easier still: leave it minimal here and tune the whole frame selection (apply kits, tick individual frames, author regex checks) from the dashboard's Policy page; see §4.

### 3. Point developers at the gateway, then lock it down

The `git` user's home is the activation root (`/srv/gateway/repos`), so the documented URL is short: no `/srv/gateway/repos/` prefix in what your devs type:

```sh
git remote set-url origin git@<gateway>:myrepo.git    # sshd on default port 22
git push origin main      # gated: rejected if it carries an open BLOCK; relayed to upstream if clean
```

In containerised eval setups where sshd is on host port 2222, add a one-time ssh config entry (`Host gateway / Port 2222`) so the short form stays short, or write the explicit port in the URL: `ssh://git@<gateway>:2222/~/myrepo.git` (note the `~/` - the git-shell-restricted user resolves repo paths relative to its home; a bare `/myrepo.git` won't route).

If the dev box has nimblegate installed locally, leaks are caught at commit/push time and never traverse to the gateway: that's defence-in-depth working, not a fault. The gateway audit log (`/srv/gateway/cfg/<repo>/audit.log`) is the authoritative record of what actually reached the boundary.

A blocked push surfaces findings as ordinary `remote:` lines and fails. **For the gate to be un-bypassable:** devs must hold push access only to the gateway (remove cached upstream push creds for the repo), and the upstream must restrict pushes to the gateway's account (e.g. Gitea/GitHub branch protection on `main`). Otherwise anyone with upstream creds bypasses the gate by pushing straight there.

### 3b. Deploy the privilege-separated relay (recommended for production)

By default the relay runs inline as the `git` user, which means the `git` user can read the upstream credential, a thin boundary (see [`SECURITY-MODEL.md`](SECURITY-MODEL.md) "Privilege-separated upstream relay"). To put the credential beyond the push-facing user, run the relay as a dedicated `nbg-relay` user. One-time setup on the gateway (as root):

```sh
# 1. Dedicated relay user + a group shared with git (so git can connect to the socket)
useradd -r -M -s /usr/sbin/nologin nbg-relay
groupadd -f gwshare && usermod -aG gwshare git && usermod -aG gwshare nbg-relay

# 2. Credential: ONLY in the per-repo file, owned by nbg-relay, never in the URL
printf '%s' "<upstream-token>" > /srv/gateway/cfg/<repo>/credential
chown nbg-relay:nbg-relay /srv/gateway/cfg/<repo>/credential
chmod 600 /srv/gateway/cfg/<repo>/credential

# 3. gateway.toml is non-secret (upstream URL + gating) and read by BOTH users
chgrp -R gwshare /srv/gateway/cfg && chmod -R g+rX /srv/gateway/cfg
chmod 600 /srv/gateway/cfg/*/credential          # re-tighten the credential files

# 4. Bare repos readable by nbg-relay (it reads objects to push; code isn't the secret)
chgrp -R gwshare /srv/gateway/repos && chmod -R g+rX /srv/gateway/repos

# 5. Inbound hardening: authorized_keys ROOT-owned, outside git's home, so a
#    compromised git user can't add its own keys. In /etc/ssh/sshd_config.d/git.conf:
#      Match User git
#          AuthorizedKeysFile /etc/ssh/git_authorized_keys
#    then: touch /etc/ssh/git_authorized_keys && chown root:root … && chmod 644 …
#    (migrate existing keys from /home/git/.ssh/authorized_keys), systemctl reload ssh
#
# 5b. SSH surface hardening: disable password auth + deny the git user any
#     forwarding/pty (a key must only run git, never tunnel through the gateway).
#     Dashboard-added keys already carry `restrict`; this covers hand-added keys
#     and password auth. Writes a drop-in and validates with `sshd -t`:
#       nimblegate gateway harden-sshd --apply     # then, after testing a NEW
#                                                   # key login: systemctl reload ssh
#     (KEEP your current session open until a fresh login is confirmed.)

# 6. Install + start the relay service (copy the unit, don't hand-type it)
scp deploy/gateway/nimblegate-relay.service root@<gateway>:/etc/systemd/system/   # from the dev machine
ssh root@<gateway> 'systemctl daemon-reload && systemctl enable --now nimblegate-relay'
```

Then register (or re-register) repos so the hook routes through the service:

```sh
sudo -u git nimblegate gateway add --name <repo> --upstream "<tokenless-upstream-url>" \
  --relay-socket /run/nimblegate/relay.sock \
  --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos
```

`--relay-socket` bakes `NBG_RELAY_SOCKET` into the generated `post-receive` hook; the push then routes through `nbg-relay`, and the `git` user never reads the credential. The push still gets a synchronous `relay FAILED` if delivery breaks, and the service's reconciler re-pushes anything missed while it was down. **The upstream URL must be tokenless**; `gateway add` warns if it embeds a credential (the token belongs in the credential file from step 2). Verify the boundary with `deploy/gateway/relay-boundary-test/run.sh` (a containerized adversarial check that `git` can't read the credential, push directly, or edit `authorized_keys`).

**SSH relay key (for an `ssh://` / `git@…` upstream).** With an SSH upstream the relay authenticates as its own user via an SSH key - the `credential` file (PAT) is **ignored** (a PAT applies only to `http(s)://` upstreams; the URL scheme decides). Create or regenerate the key on the gateway as root:

```sh
install -d -o nbg-relay -g nbg-relay -m 700 /home/nbg-relay/.ssh
ssh-keygen -t ed25519 -N '' -f /home/nbg-relay/.ssh/id_ed25519 -C nbg-relay@gateway
chown nbg-relay:nbg-relay /home/nbg-relay/.ssh/id_ed25519 /home/nbg-relay/.ssh/id_ed25519.pub
ssh-keyscan <upstream-host> >> /home/nbg-relay/.ssh/known_hosts   # mandatory: strict host-key check rejects unknown hosts before auth
chown nbg-relay:nbg-relay /home/nbg-relay/.ssh/known_hosts
```

`-N ''` is required - the relay is non-interactive, so the key must be passphraseless. Register `id_ed25519.pub` on the **upstream account's** SSH keys (e.g. Gitea → Settings → SSH/GPG Keys) with **write** access - an *account* key survives repo deletes/recreates; a per-repo *deploy* key does not, and must be re-added on every recreate. This is on the upstream, **not** nimblegate's SSH Keys page. Verify before relying on it:

```sh
sudo -u nbg-relay env HOME=/home/nbg-relay ssh -o IdentitiesOnly=yes git@<upstream-host>
```

Success is the upstream's "authenticated, but … no shell access" banner. A password prompt means the key isn't registered (or lacks write) - fix that on the upstream, not the gateway. Then confirm an actual relay with a manual push (`sudo -u nbg-relay env HOME=/home/nbg-relay git --git-dir=/srv/gateway/repos/<repo>.git push <upstream-url> <sha>:refs/heads/main`).

### 3c. Optional: per-key repo scoping (multi-dev gateways)

By default any authorized key can reach **every** repo. To confine each key to specific repos (forced command + per-key ACL; see [`SECURITY-MODEL.md`](SECURITY-MODEL.md) "Per-key repo scoping"):

> **First switch the git user off `git-shell`:** `usermod -s /bin/sh git`. sshd runs the forced command through the login shell, and `git-shell` rejects it (it only accepts git verbs), so scoped access does nothing until the shell is a normal one. After the switch, the forced command + `restrict` on **every** key is the whole inbound boundary, so verify no plain (forced-command-less) line remains in `authorized_keys`. Single-tenant gateways skip this entirely and keep `git-shell`.

```sh
# 1. one-time activation: rewrite existing keys to forced commands + seed grants
#    (each existing key → every current repo, preserving today's access)
sudo -u git nimblegate gateway access-migrate \
  --ssh-authorized-keys /home/git/.ssh/authorized_keys \
  --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos

# 2. run the dashboard with --scoped-access so NEW keys also get forced commands
#    (add --scoped-access to the ExecStart in nimblegate-dashboard.service)

# 3. tighten: grant each key only the repos it needs (or use the dashboard)
sudo -u git nimblegate gateway access grant  --repo myrepo --key SHA256:… --policy-root /srv/gateway/cfg
sudo -u git nimblegate gateway access revoke --repo other  --key SHA256:… --policy-root /srv/gateway/cfg
sudo -u git nimblegate gateway access list   --repo myrepo --policy-root /srv/gateway/cfg
```

`--read` on `grant` makes it fetch-only. A key with **no** grant can reach nothing. The dashboard's SSH Keys page grows a "Repo access (scoped)" section (grant/revoke) when `--scoped-access` is on. Verify the boundary with `deploy/gateway/scoped-access-test/run.sh`.

### 4. Monitor, tune, and author checks (web UI)

Run the built-in dashboard on the gateway box. It defaults to **single-admin auth** (`--auth=setup-token`) and **loopback bind** (`--addr 127.0.0.1`); on first start it writes a one-time setup token (to stdout/the log **and** to `<policy-root>/_setup_token`). Claim the admin login at `/setup` with it.

Get the token, any of these (bare-metal):

```sh
nimblegate gateway setup-token --policy-root /srv/gateway/cfg   # prints it to stdout
cat /srv/gateway/cfg/_setup_token                               # or read the file directly
journalctl -u nimblegate-dashboard | grep nbg-setup            # or from the log
```

(Container: `docker logs <container> | grep "setup token"`.) The token is single-use: once you claim it, `/setup` 404s.

For a **trusted LAN** you can bind to the LAN and (optionally) skip auth:

```sh
nimblegate gateway dashboard --serve --allow-edits --addr 0.0.0.0 \
  --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos \
  --ssh-authorized-keys /home/git/.ssh/authorized_keys \
  --auth off    # trusted-LAN only; omit to require the admin login claimed at /setup
# then open http://<gateway-ip>:7900 from any machine on the LAN
```

> **`--ssh-authorized-keys` matters on bare metal.** Its built-in default is the *container* path `/srv/gateway/ssh/authorized_keys`. On bare metal the host's sshd reads `/home/git/.ssh/authorized_keys`, so without this flag, keys you add on the dashboard's **SSH Keys** page land in a file sshd never reads, and pushes fail `Permission denied (publickey)` with everything *looking* correct. Point it at the file your host sshd actually reads.

For set-and-forget, install it as a **systemd service** so it auto-starts on boot (a binary update is then just `scp` + `systemctl restart nimblegate-dashboard`). The unit ships as a file in this repo: **copy it, do not hand-type or paste it**:

```sh
# from the dev machine:
scp deploy/gateway/nimblegate-dashboard.service root@<gateway>:/etc/systemd/system/
ssh root@<gateway> 'systemctl daemon-reload && systemctl enable --now nimblegate-dashboard'
```

Edit the paths inside the unit only if your install differs from the runbook defaults (`/usr/local/bin/nimblegate`, `/srv/gateway/cfg`, `/srv/gateway/repos`); `User=git` is correct because those dirs are git-owned (use `root` only if the service can't read them).

> **Why copy a file instead of pasting commands?** Every failure setting this up came from pasting structured/long content into a terminal: a heredoc gets auto-indented (a `#!/bin/sh` no longer at column 0 → `Exec format error`), and a long single-line flag list gets a newline injected mid-line so trailing flags are silently dropped (`--policy-root` lost → the dashboard reads the default empty root and shows **0 repos**, with no error). Copying a version-controlled file removes both. Apply the same rule to anything long or sensitive (the upstream-token credential below): put it in a file or write it with `printf`/short lines, never a pasted heredoc, and verify it (`cat -A`) before relying on it.

If `systemctl status` shows `bind: address already in use`, a dashboard you started by hand is still holding port 7900: only one process can bind it. Stop the manual one first, clear the failed state, and restart:

```sh
pkill -f 'nimblegate gateway dashboard'       # stop the stray manual instance
systemctl reset-failed nimblegate-dashboard   # clear the crash-loop rate limit
systemctl restart nimblegate-dashboard
```

**Common failure modes (all silent, no crash, just wrong behavior):**

| Symptom | Cause | Check / fix |
|---|---|---|
| Dashboard shows `0 repos` / empty Policy dropdown | service reading the **default** `/etc/nimblegate-gateway/repos` (the `--policy-root` flag was dropped) | `journalctl -u nimblegate-dashboard \| grep 'reading decisions'`: must say `/srv/gateway/cfg`; if not, the `ExecStart` lost its flags (copy the unit file) |
| Repo registered but absent from the dropdown | no `gateway.toml` under `<policy-root>/<repo>/` | the dropdown globs `*/gateway.toml`; run `nimblegate gateway add …` (it writes that file) |
| Service fails `203/EXEC` / `Exec format error` | a hand-created launcher script whose `#!/bin/sh` isn't at column 0 | `cat -A script \| head -1` must show `#!/bin/sh$`; copy a file rather than pasting a heredoc |
| Accepted pushes never reach the upstream | empty/missing/wrong `credential` (or token-in-URL) | the relay fails closed; check the per-repo `credential` file is non-empty and has push scope |
| Accepted pushes never reach the upstream; relay logs `Host key verification failed` (exit 128) | upstream is an `ssh://` / `git@…` URL and the gateway's relay user has **never accepted the upstream's SSH host key** (strict host-key check rejects the connection *before* auth) | on the gateway, as the relay user: `ssh-keyscan -H <upstream-host> >> ~/.ssh/known_hosts`, **and** confirm the gateway's own key is an authorized deploy key on the upstream repo. Simpler: switch the upstream to `http(s)://USER:TOKEN@…` (PAT in URL) to skip SSH host-key/key setup entirely. Note: a **PAT does nothing for an `ssh://` upstream**; the URL scheme decides which credential is used. |
| Relay won't start; `systemctl` shows `status=217/USER` | the unit's relay `User=` (e.g. `nbg-relay`) **no longer exists** - most often dropped during a host rebuild / OS migration | `getent passwd nbg-relay` (empty = gone). Recreate it (see "privilege-separated relay" step 1) + re-add to the socket group (`usermod -aG git nbg-relay`), then `systemctl restart nimblegate-relay`. Recreating the user does **not** restore its `~/.ssh` key - see the next row. |
| Relay is `active` but pushes never reach the upstream and **nothing is logged** | reconcile logs only on success, so a failing relay is currently silent. After a relay-user or upstream-repo recreate the usual cause is a missing/unregistered relay SSH key (`useradd -M` leaves no `~/.ssh`; or a per-repo deploy key died when the repo was recreated) | confirm the key exists (`ls ~nbg-relay/.ssh/`) and its **public** half is on the upstream **account's** SSH keys (an account key survives repo recreates; a deploy key does not) with **write**. Prove it with a manual relay push, which surfaces the error reconcile hides: `sudo -u nbg-relay env HOME=~nbg-relay git --git-dir=<repo>.git push <upstream-url> <sha>:refs/heads/main` |

**Relay health check** - run after any host rebuild / OS migration (a migration can silently drop the relay user and its `~/.ssh`, leaving the unit pointing at a `User=` that no longer exists):

```sh
getent passwd nbg-relay              # relay user exists?  (empty ⇒ 217/USER on start)
systemctl is-active nimblegate-relay # service running?
ls -la ~nbg-relay/.ssh/              # has a private key + known_hosts?
```

The relay's upstream key lives on the **upstream account** (e.g. Gitea → Settings → SSH/GPG Keys), **not** in nimblegate's own SSH Keys page - that page authorizes *inbound* pushes to the gateway, a different role.

**Access scope, read this.** `--addr 0.0.0.0` makes the UI reachable by anyone on the network, and with `--allow-edits` that includes the *write* surface (policy tuning + check authoring) with **no authentication**: the CSRF/same-origin guards stop cross-site abuse, not someone who simply opens the IP. That's an accepted trade on a trusted LAN. When the dashboard needs to leave the LAN, switch to one of:

- **Read-only on LAN**: drop `--allow-edits` so the IP serves monitoring only.
- **Localhost + SSH tunnel**: omit `--addr` (it defaults to `127.0.0.1`, not exposed) and reach it through a tunnel: `ssh -L 7900:localhost:7900 root@<gateway>`, then open `http://localhost:7900`.

The dashboard ships with **single-admin auth** (claim the login at `/setup`); for safe exposure beyond the trusted LAN, front it with a **reverse proxy + TLS** or reach it over the SSH tunnel above. Multi-operator / RBAC is a commercial-tier follow-up; until then, anything beyond the trusted LAN should go through the tunnel, a reverse proxy, or stay read-only.

The feed shows accept/reject decisions across all gated repos, rejections highlighted with their block messages, and WARN/INFO findings as severity chips on every row (so advisory findings on *accepted* pushes are visible too). A repo drop-down and "only rejects" toggle narrow the view; it auto-refreshes every 5 s. Options: `--port` (default 7900), `--tail` (max decisions per repo, default 500).

With `--allow-edits`, open the **Policy** link (or `/policy`), pick a repo, and you can:

- **Tune severity**: set any frame's severity (BLOCK / WARN / INFO) or toggle the repo's gating on/off. Written immediately to that repo's `appframes.toml`; effective on the next push.
- **Author a regex check**: name + file-glob patterns + a regular expression + severity (default WARN). A **live preview** runs the regex against the latest pushed tree and shows "would flag N files" plus sample matching lines before you save. Stored as a `kind="regex"` linter; runs on the next push alongside stdlib frames. List / re-severity / enable-disable / delete authored checks from the same page.

**`--repos-root`** points at the bare-repos directory (same value as `nimblegate gateway add`); it's required for the live preview: without it the dashboard still serves, but preview is unavailable.

**Security stance, regex only, never `command`:** the `/policy` UI authors only `kind="regex"` checks; it cannot configure a linter `command` (a subprocess that runs on every push). Command/subprocess linters are managed by editing `appframes.toml` over SSH. This is intentional: the gateway holds the sole upstream credential, so a web form able to write a linter `command` would let anyone with dashboard access configure arbitrary code execution on every push: one session-compromise from a supply-chain vector. A regex pattern can't execute anything; that asymmetry is the boundary.

---

## Recover

### Lost admin password

The dashboard's login lives in an SQLite auth db at `/srv/gateway/cfg/_auth.db`. There's no email reset in v0.1.0 (single-admin auth): since you control the gateway box, recovery is to wipe the auth db and re-claim the admin slot via a fresh setup token. Your repos, SSH keys (`/srv/gateway/ssh/`), per-repo policy, audit logs, and upstream credentials are untouched; only the admin username + password are reset.

```sh
ssh root@<gateway>
# (then on the gateway)
systemctl stop nimblegate-dashboard.service
rm -f /srv/gateway/cfg/_auth.db /srv/gateway/cfg/_auth.db-shm /srv/gateway/cfg/_auth.db-wal
systemctl start nimblegate-dashboard.service
sleep 1
journalctl -u nimblegate-dashboard.service -n 50 | grep nbg-setup
# expect a line like: [nbg-setup] first-run setup token: XXXX-XXXX-XXXX-XXXX - visit /setup to claim
```

Open `http://<gateway>:7900/setup`, paste the token, pick a fresh username + password (8+ characters). The token is single-use: once you claim it, `/setup` 404s.

The container variant has the same recovery with `docker exec` in front; see the top-level [`README.md`](../../README.md) "Forgot your password?" block. Email-based password reset is on the commercial roadmap for the multi-user tier.

---

## Inspect / operate

```sh
ssh root@<gateway> 'cat /srv/gateway/cfg/myrepo/audit.log'   # JSONL: each push decision + findings
ssh root@<gateway> 'tail /var/log/auth.log'                  # ssh access (or: docker logs nimblegate-gateway)
```

### Remove a repo: archive vs delete, and where the files live

A repo's footprint spans **two roots**, each with an activation symlink that points at the real directory under `_repos/`:

| What | Active symlink | Real files (the delete targets) |
|---|---|---|
| Bare repo (all git history) | `/srv/gateway/repos/<repo>.git` | `/srv/gateway/repos/_repos/<repo>.git/` |
| Policy + audit + credential | `/srv/gateway/cfg/<repo>` | `/srv/gateway/cfg/_repos/<repo>/` |

(`gateway.toml`, `appframes.toml`, `credential`, `audit.log`, and scan/access state all live in the policy dir.)

**Archive (reversible).** The dashboard's Archive button and `nimblegate gateway archive` remove only the two symlinks; the `_repos/` files stay, and `restore` re-links them. Use this to take a repo offline without losing history:

```sh
nimblegate gateway archive --name myrepo --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos
```

**Delete (permanent).** To remove a repo's files entirely, use `gateway delete`: it clears the symlinks **and** both `_repos/` dirs. Irreversible, so it requires `--yes`:

```sh
nimblegate gateway delete --name myrepo --yes \
  --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos
```

Or by hand (same four paths):

```sh
rm -rf /srv/gateway/repos/myrepo.git  /srv/gateway/repos/_repos/myrepo.git \
       /srv/gateway/cfg/myrepo         /srv/gateway/cfg/_repos/myrepo
```

The dashboard intentionally offers **only Archive**: there's no one-click permanent delete; erasing history is a deliberate CLI/manual action.

**Don't forget the token.** Deleting the files does **not** revoke the upstream push credential. If the repo used an HTTP PAT or an SSH deploy key, revoke it in Gitea/GitHub (Settings → Access Tokens, or the repo's Deploy Keys) so a leaked copy can't be reused.

### Decision analytics (SQLite)

The gateway keeps a queryable SQLite index of its decision history at `<policy-root>/analytics.db`, **derived** from the JSONL audit logs (which stay the source of truth; the DB is rebuildable). Query it from the box:

```sh
# canonical aggregates (text or --json); --since accepts RFC3339 or a duration (e.g. 720h)
nimblegate gateway analytics stats --policy-root /srv/gateway/cfg
nimblegate gateway analytics stats --policy-root /srv/gateway/cfg --repo myrepo --since 720h --json

# explicit (re)ingest, e.g. from cron; stats also ingests lazily before querying
nimblegate gateway analytics ingest --policy-root /srv/gateway/cfg
```

It's a standard SQLite file, so ad-hoc SQL works too (WAL mode → safe to read while the service runs):

```sh
apt install -y sqlite3
sqlite3 /srv/gateway/cfg/analytics.db "SELECT repo, COUNT(*) FROM decisions GROUP BY repo;"
```

The same data is browsable in the dashboard at **`/stats`** (linked from the feed and policy headers): a read-only summary + per-repo + top-frames view with repo and time-window (All / 24h / 7d / 30d) filters. It reads the SQLite index and refreshes on load, available regardless of `--allow-edits`.

---

## Not-yet (deferred)

Notable items not yet implemented: force-push / non-fast-forward protection on protected refs (today it rejects protected-ref *deletion* but not history rewrite), a test/build validation stage, relay retry / HA, tamper-evident audit chain, and relay support for http-auth / ssh identities via the credential store.

## See also

- [`SETUP-proxmox-trixie.md`](SETUP-proxmox-trixie.md): the full bare-metal first-deploy runbook.
- [`SECURITY-MODEL.md`](SECURITY-MODEL.md): trust boundaries + the gateway-held-policy rationale.
- [`deploy/gateway/`](../../deploy/gateway/): container artifacts (Dockerfile, compose, entrypoint).
