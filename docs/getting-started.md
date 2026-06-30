# Getting started: install nimblegate and connect your first repo

A complete, step-by-step walkthrough from nothing to "my agent's pushes are
being checked." It assumes you can run commands in a terminal and edit a text
file, **not** that you're a git expert. Every step says *what it does* and *which
machine it runs on*, because the most common confusion is mixing up the three
places involved.

> **Want to see it before installing?** [Try the live demo](https://demo.nimblegate.com): a real dashboard over sample data, nothing to set up.

## Contents

- [The mental model: three places, who talks to whom](#the-mental-model)
- [The day-to-day workflow: branch → push → PR → main](#the-day-to-day-workflow)
- [Step 1: Install the gateway](#step-1-install-the-gateway)
- [Step 2: Claim your admin login](#step-2-claim-your-admin-login)
- [Dashboard access (local, remote, LAN)](#dashboard-access)
- [Step 3: Authorize your SSH key](#step-3-authorize-your-ssh-key)
- [Step 4: Register the repo to guard](#step-4-register-the-repo-to-guard)
- [Step 5: Point your computer at the gateway and push](#step-5-point-your-computer-at-the-gateway-and-push)
- [Step 6: (optional) Turn on Auto-PR](#step-6-optional-turn-on-auto-pr)
- [Step 7: Make the gateway a real boundary](#step-7-make-the-gateway-a-real-boundary)
- [Command-line reference (on the gateway)](#command-line-reference-on-the-gateway)
- [Troubleshooting: quick checks](#troubleshooting-quick-checks)

---

## The mental model

There are **three** machines/places in play. Mixing them up is the #1 source of
"why isn't this working." Get this picture first and the rest is easy.

```
   YOUR COMPUTER                THE GATEWAY                   THE UPSTREAM
   (where you / your agent      (nimblegate, the new          (your real git host:
    write code and run git)      thing you're installing)      GitHub / Gitea / GitLab)

   git push ──────────────────► checks the push ──forwards──► stores the code
   (over SSH, port 2222)        against your rules            (your "real" repo)
   git clone ◄───────────────── serves the code
```

Three rules that follow from this picture:

1. **Your computer only ever talks to the gateway.** You push to the gateway and
   clone from the gateway. You do **not** point your computer at the upstream
   anymore. That would skip the checks entirely.
2. **Only the gateway talks to the upstream.** It holds the credential to your
   real git host and forwards clean pushes there for you.
3. **The gateway lives on its own machine** (a small server, a VM, a Proxmox
   container, anywhere reachable on your network). It is *not* your laptop and
   *not* GitHub.

Put another way, there are **two connections**, and each uses a *different*
credential, set up in Steps 3 and 4:

- **Your computer → nimblegate gateway** *(inbound: who may push)* - your SSH key
  (Step 3). The public half goes on the gateway; the private half never leaves
  your computer.
- **nimblegate gateway → upstream** *(outbound: where clean pushes go)* - a
  credential the **gateway** owns: an HTTPS token, or its own SSH deploy key on
  the upstream (Step 4). Your computer never holds it.

Throughout this guide:

- **"on the gateway"** = a command you run on the server where nimblegate is
  installed (often via `docker exec`).
- **"on your computer"** = your laptop/workstation where you write code.

---

## The day-to-day workflow

Once it's set up, here's how a change actually flows - this is the loop the
gateway is built around:

1. **Your agent works on its own feature branch.** Coding agents often use a git
   **worktree** per task, so several branches are in flight at once. One task =
   one branch.
2. **The agent pushes that branch to the gateway** (its `origin`, over SSH). It
   can't reach your real host directly - only the gateway holds that credential -
   so every push is checked.
3. **The gate decides:**
   - **Clean** → forwarded to your upstream. A PR is opened there
     (`feature → main`), by the agent or by you.
   - **Finding** → **rejected**. The bad commit never reaches your upstream. If
     that branch has an open PR, the gate posts the finding as a comment on it
     (see [Step 6: Auto-PR](#step-6-optional-turn-on-auto-pr)) - the agent reads
     it, fixes, and re-pushes until the gate passes.
4. **You review the PR and merge it into `main`** on your git host - the normal
   human review step. (That merge happens on your host, not through the gateway.)

Two things make this safe and low-friction:

- **Rejected commits bounce at the gate**, so the branch on your upstream only
  ever contains gated-clean code. By the time you review a PR it has already
  passed the automated checks - you spend review time on judgment, not hunting
  for leaked keys or `rm -rf`.
- **Two layers, two jobs:** the gate catches the mechanical/catastrophic things
  instantly on every push (no human needed); you stay the final authority on the
  PR merge.

The agent's *feature* branches are gated by default - not just `main` - because a
new repo's **protected refs default to `refs/heads/*`** (changeable in Step 4, or
via *Edit repo settings* later). If you narrow it to `refs/heads/main`, only
`main` is checked and feature-branch pushes sail through unchecked, so the auto-PR
loop never fires on them.

---

## Step 1: Install the gateway

*On the gateway machine.* You need **Docker**. If you don't have it, follow
[Docker's install guide](https://docs.docker.com/engine/install/) (on Proxmox,
the [community helper-scripts](https://community-scripts.github.io/ProxmoxVE/)
give you a Docker container in one command). No Docker? There's a bare-metal
install in [`docs/server/README.md`](server/README.md).

```bash
curl -O https://raw.githubusercontent.com/nimblegate/nimblegate/main/compose.yaml
docker compose up -d
```

That's the whole install. The recipe starts one container with the dashboard on
port **7900** (admin web UI, bound to localhost) and git-push on port **2222**.

**Air-gapped / can't reach the internet from the gateway?** Build the image on a
machine that *can*, then transfer it:

```bash
# on a build machine with the source checked out:
docker build -t ghcr.io/nimblegate/nimblegate:0.1.0 .
docker save ghcr.io/nimblegate/nimblegate:0.1.0 | gzip > nimblegate.tar.gz
# copy nimblegate.tar.gz to the gateway, then on the gateway:
docker load -i nimblegate.tar.gz
docker compose up -d
```

**Port already in use?** Set `NIMBLEGATE_DASHBOARD_PORT` / `NIMBLEGATE_SSH_PORT`
inline or in a `.env` file next to `compose.yaml`, no need to edit the recipe.

**Reaching the dashboard.** It binds to localhost on the gateway by default (it's
an admin surface). To open it from your laptop, either SSH-tunnel
(`ssh -L 7900:localhost:7900 you@gateway`) or set
`NIMBLEGATE_DASHBOARD_HOST=0.0.0.0`, but only behind a reverse proxy with auth.

---

## Step 2: Claim your admin login

*On the gateway.* A one-time setup token is printed on first start:

```bash
docker logs nimblegate | grep nbg-setup
# [nbg-setup] first-run setup token: XXXX-XXXX-XXXX-XXXX - visit /setup to claim
```

Open `http://<gateway>:7900/setup`, paste the token, and pick a username +
password (8+ chars). The token is single-use; after you claim it, `/setup` 404s.

*(Forgot the password later? See [Operations → Forgot the admin
password](operations.md#forgot-the-admin-password); it resets only the login,
not your repos or keys.)*

---

## Dashboard access

The dashboard binds the host's **loopback** (`127.0.0.1:7900`) by default - it's the
admin surface, so it's deliberately off the network. How you reach it depends on where
the gateway runs:

- **On the gateway machine itself:** open `http://localhost:7900`.
- **Remote / headless host (recommended):** tunnel from your computer, then open the
  *local* URL:
  ```bash
  ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>
  # then open http://localhost:7900
  ```
  Use **`127.0.0.1`**, not `localhost`, in the `-L` forward - the dashboard is published
  on IPv4, and `localhost` can resolve to IPv6 `::1` first, which connects to nothing
  (you'd see an empty response).
- **Trusted LAN (home lab):** bind it to the network so you can reach it by the box's IP
  with no tunnel:
  ```bash
  NIMBLEGATE_DASHBOARD_HOST=0.0.0.0 docker compose up -d   # then http://<box-ip>:7900
  ```
  Only on a network you trust - it's the admin surface. Behind a home router/NAT is fine;
  **never** port-forward it raw to the internet.
- **Public internet:** put TLS + a domain in front instead of a raw bind:
  ```bash
  nimblegate gateway tls-setup --domain dash.example.com
  ```

---

## Step 3: Authorize your SSH key

*This is the **inbound** connection: your computer → nimblegate gateway. It
decides **who** is allowed to push.* Your computer proves who it is with an SSH
key. You paste the **public half** into the gateway; the **private half** never
leaves your computer.

**Make a dedicated key** (recommended, keeps it separate from any keys you
already use). *On your computer:*

```bash
ssh-keygen -t ed25519 -f ~/.ssh/nimblegate -C "nimblegate"
cat ~/.ssh/nimblegate.pub      # the PUBLIC half - this is what you paste
```

`-f ~/.ssh/nimblegate` names the file so it sits alongside your existing keys
instead of overwriting them. The file **without** `.pub` is the private half:
never paste or share it. (Already have a key you want to reuse? Just
`cat ~/.ssh/id_ed25519.pub` instead.)

Then in the dashboard, open **SSH Keys** (`/ssh-keys`), paste the public key,
give it a label, and click **Authorize key**. It's active immediately.

---

## Step 4: Register the repo to guard

*This is the **outbound** connection: nimblegate gateway → upstream. It decides
**what** gets watched and **where clean pushes go**.* In the dashboard, open
**Repos** (`/repos`) → **+ Add new repo to gateway**:

- **Name**: a short name for the repo on the gateway, e.g. `myapp`. This becomes
  the push path `myapp.git`. It does **not** need to match the upstream's
  owner/name. On the gateway, repos are flat (`myapp.git`, no `owner/` folder).
- **Upstream URL**: your *real* repo's clone URL, where the gateway forwards
  accepted pushes. Use the **HTTPS** URL (e.g.
  `https://gitea.example.com/yourname/myapp.git`). HTTPS is what pairs with a
  token.
- **Upstream credential**: the token the gateway uses to push to the upstream.
  Give it the **minimum scope that allows pushing to the repo** - nothing wider:
  - **GitHub**: a classic PAT with **`repo`**, or a fine-grained PAT scoped to
    that repo with **Contents: Read and write**.
  - **Gitea**: **`write:repository`**.
  - **GitLab**: **`write_repository`**.

  Stored locked-down (0600), never logged. *(Auto-PR posts findings as PR
  comments, which needs a little more: on **Gitea** also add **`write:issue`**;
  on **GitLab** use **`api`**; on **GitHub** classic `repo` already covers it
  (fine-grained: add **Pull requests: Read** to find the PR **and Issues: Read
  and write** to post the comment - PR comments use the Issues API). See Step 6.)*
- **Protected refs**: which branches the gate actually checks. `refs/heads/main`
  by default. To check **every** branch (recommended if your agent works on
  feature branches), use `refs/heads/*`.
- **Status**: leave **enabled** ticked. Leave **observe-only** *unticked* to
  actually enforce. (Observe mode records findings but never blocks and is
  silent, for measuring an agent, not for protection.)

Click **Register**. The `core` kit (the catastrophic-prevention rules) is applied
automatically; you can refine the rule set on the Policy page anytime
(see [policy authoring](policy-authoring.md)).

**If the upstream already has commits**, the gateway mirrors that history down
automatically at registration, so it's immediately in sync. If the mirror
couldn't run (e.g. the token wasn't set yet), the Repos page shows a one-click
**Sync from upstream** button: set the credential, click it, done.

**Where these live on the upstream (to set them, or verify they exist).** When a
push reaches the gateway but never shows up on your real host, the usual cause is
a missing or wrong credential on the *upstream* side. This is where to check:

| What | Gitea | GitHub |
|---|---|---|
| **PAT** (for an `https://` upstream) | Settings → Applications → **Access Tokens** - `/user/settings/applications` | Settings → Developer settings → **Personal access tokens** - `github.com/settings/tokens` |
| **Deploy key** (for an `ssh://` upstream) | repo → Settings → **Deploy Keys** → Add Key, tick **write access** - `/<owner>/<repo>/settings/keys` | repo → Settings → **Deploy keys** → Add deploy key, tick **Allow write access** - `github.com/<owner>/<repo>/settings/keys` |

For the deploy-key path the key you register is the **gateway's own public key**
(not your dev key) - print it on the gateway as the git user
(`cat ~/.ssh/id_ed25519.pub`) - and the gateway must also trust the upstream's
host key once: `ssh-keyscan -H <upstream-host> >> ~/.ssh/known_hosts`. Full SSH
relay setup is in [`docs/server/README.md`](server/README.md).

> **HTTPS is the supported default - and it covers private repos.** A PAT
> authenticates HTTPS for **both public and private** upstreams, so you don't
> need SSH for a private repo. The container ships **without an SSH client, by
> design** (minimal image, smaller attack surface), so the `ssh://` + deploy-key
> path above is **opt-in**: it works only if you install an SSH client into the
> container image yourself. Out of the box, an `ssh://` / `git@…` upstream is
> rejected at registration - use the `https://` URL + PAT.

---

## Step 5: Point your computer at the gateway and push

*On your computer.* This is where the three-places model matters most: you point
your repo's `origin` at the **nimblegate gateway**, never the upstream.

**Set up an SSH shortcut once** so you don't have to remember the port and key.
Add this to `~/.ssh/config`:

```
Host nimblegate
  Hostname  192.0.2.10          # ← the gateway's address (IP or hostname)
  Port      2222                  # the gateway's git port (bare-metal: 22)
  User      git
  IdentityFile   ~/.ssh/nimblegate # the key from Step 3
  IdentitiesOnly yes              # use ONLY this key
```

Now connect your repo. **Which command depends on whether you already have the
code:**

- **The upstream already has the project** (most common): clone it *from the
  gateway*:
  ```bash
  git clone nimblegate:myapp.git
  cd myapp
  ```
- **Brand-new, empty project**: turn your folder into a repo and point it at the
  gateway:
  ```bash
  cd my-project
  git init                       # if it isn't a repo yet
  git add -A && git commit -m "initial"
  git remote add origin nimblegate:myapp.git
  git push -u origin main
  ```
- **You already have a clone pointed at the upstream**: just re-point it:
  ```bash
  git remote set-url origin nimblegate:myapp.git
  ```

Then work normally: `git add`, `git commit`, `git push`. Every push goes to the
gateway, gets checked, and (if clean) is forwarded to your upstream. Watch it
live at `http://<gateway>:7900/feed`.

> **The `nimblegate:myapp.git` shorthand** uses the `Host nimblegate` block above
> (scp-style, so the path is *relative* - git-shell resolves it under the git
> user's home). On the **Docker image** the git user's home *is* the repos root,
> so the relative path lands on your repo. If you'd rather not use `~/.ssh/config`,
> the long form is `ssh://git@192.0.2.10:2222/~/myapp.git` - **note the `~/`.**
>
> **Bare-metal differs in BOTH port and path.** A bare-metal install runs sshd on
> the default **22** (so drop the `:2222`), and it keeps the git user's home at
> `/home/git` while repos live under `/srv/gateway/repos/`. So the `~/` shorthand
> (and the scp `nimblegate:myapp.git` form) point at the wrong place - use the
> **absolute path** instead:
>
> ```
> ssh://git@192.0.2.10/srv/gateway/repos/myapp.git
> ```
>
> See [server setup](server/README.md).
>
> **Why git-shell, and what resolves:** on the gateway the SSH user is locked to
> **git-shell** - it can run `git` push/clone and *nothing else* (no shell, no
> arbitrary commands, no reading the gateway's stored upstream token). It accepts
> any path that points at a real bare repo - the `~/` shorthand on the container,
> or the full `/srv/gateway/repos/...` path on bare-metal; a bare `/myapp.git`
> fails because nothing lives at the filesystem root. This is deliberate security:
> a dev/agent key can *only* move git data through the gate, so it can't bypass the
> gate or lift your upstream credential.
>
> **Do not** use the `git@192.0.2.10:2222/myapp.git` form - there `:2222` is read
> as part of the *path*, not the port (see Troubleshooting).

---

## Step 6: (optional) Turn on Auto-PR

When a push is rejected, the gateway can post the findings as a comment on the
upstream Pull Request and fire a webhook, so an agent can read the rejection and
fix it. Enable it per repo on **`/auto-pr`** → Setup, or in `gateway.toml`. Full
guide: [`docs/notifications.md`](notifications.md).

**Token scope for comments (the #1 first-time gotcha):** posting a PR comment is a
*different* permission than relaying the push, so relay can work while comments
fail with **HTTP 403**. Required scopes:

- **GitHub:** a classic token with **`repo`**, or fine-grained with **Pull
  requests: Read** (to find the PR) **+ Issues: Read and write** (to post the
  comment - PR comments use the Issues API), alongside Contents: Read and write.
- **Gitea:** **`write:issue`** as well as `write:repository`.
- **GitLab:** the **`api`** scope - there's no narrower scope that allows MR
  comments, and `api` also covers the push + finding the MR.

If deliveries fail, the **Auto-PR → Repos** tab shows the error and a hint inline
(no `docker logs` needed). After you regenerate + rotate the token on `/repos`,
click **Retry now** on the repo's row - it resets the retry backoff and re-queues
any deadlettered comments so they deliver immediately, instead of waiting out the
multi-hour backoff.

---

## Step 7: Make the gateway a real boundary

*On your computer (where the agent runs).* The gateway only protects you if the
agent **can't reach the upstream directly**. If your machine still has GitHub
credentials or an upstream-authorized SSH key, the agent can push around the gate.

```bash
# 1. Remove stored HTTPS credentials for your upstream
rm -f ~/.git-credentials
git config --global --unset credential.helper

# 2. Make sure this machine's key isn't authorized on the upstream directly
ssh -o BatchMode=yes -T git@github.com 2>&1 | grep -q "Permission denied" \
  && echo "OK: github refused" || echo "BYPASS: this key works on github directly"
```

Full hardening (keychain/libsecret sweeps, the `gh`/`glab`/`tea` CLIs, agent
forwarding) is in [`docs/server/DEV-MACHINE-SETUP.md`](server/DEV-MACHINE-SETUP.md).

---

## Command-line reference (on the gateway)

Most operating is done from the dashboard, but everything has a CLI equivalent for
scripting or when the web UI isn't handy. These run **on the gateway machine**:

- **Container install:** prefix with `docker exec -u git nimblegate` - e.g.
  `docker exec -u git nimblegate nimblegate gateway setup-token`.
- **Bare-metal install:** run `nimblegate gateway …` directly, adding
  `--policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos` (the dirs from
  your install).

| Command | What it does |
|---|---|
| `nimblegate version` | Print the running version/commit. Use it after a binary update to confirm the new code is actually live (a stale gateway is almost always a binary that was never copied over). |
| `nimblegate gateway setup-token` | Print the one-time admin setup token for `/setup` (bare-metal equivalent of `docker logs nimblegate \| grep nbg-setup`). |
| `nimblegate gateway add --name <n> --upstream <url>` | Register a repo: the CLI form of **Repos → Add** (Step 4). `--protect` defaults to `refs/heads/*` (gate every branch); pass `--protect refs/heads/main` to narrow it to `main` only. |
| `nimblegate gateway archive --name <n>` | Deactivate a repo but keep its data (removes the activation symlinks; the bare repo and history stay). |
| `nimblegate gateway restore --name <n>` | Re-activate a previously archived repo. |
| `nimblegate gateway delete --name <n> --yes` | **Permanently** delete a repo and all its data (bare repo + policy). No undo. |
| `nimblegate gateway rescan --name <n>` | Re-run the first-push scan that recommends additional kits for a repo. |
| `nimblegate gateway token new <label>` | Mint a bearer token for the MCP/REST analytics API. Also `token list` and `token revoke <id>`. |
| `nimblegate gateway access grant\|revoke\|list` | Per-key repo permissions (which SSH key may push/fetch which repo), if you scope access per key. |
| `nimblegate gateway dashboard --serve` | Run the dashboard web server. The container starts this for you; you'd only run it by hand on bare-metal (see the systemd unit in the server guide). |
| `nimblegate gateway relay-service` | The daemon that forwards clean pushes to the upstream. Also container-managed; bare-metal runs it as a systemd unit. |

The `pre-receive` / `post-receive` hooks are invoked **automatically by git** on
each push - you never run those by hand. Full server-side detail (systemd units,
paths, flags) is in [`docs/server/README.md`](server/README.md).

---

## Troubleshooting: quick checks

| Symptom | What it means / fix |
|---|---|
| **`git@…'s password:`** prompt | You're **not** reaching the gateway. Almost always the wrong **port** or **address**. The `git@host:2222/path` form treats `2222` as a path, not a port; use `ssh://git@host:2222/~/path.git` (note the `~/`) or the `~/.ssh/config` shortcut. Double-check you used the **gateway's** IP, not the upstream's. The gateway never has a password; a prompt = wrong door. |
| **`Permission denied (publickey)`** | The gateway got your connection but doesn't recognize your key. Confirm you authorized the **public** key in `/ssh-keys`, and that SSH is offering the right key: `ssh -p 2222 -i ~/.ssh/nimblegate git@<gateway>`. With multiple keys, add `IdentityFile` + `IdentitiesOnly yes` to `~/.ssh/config`. |
| **`does not appear to be a git repository`** | The repo path is wrong. Paths are **flat** on the gateway: it's `myapp.git`, not `owner/myapp.git`. Use the exact **Name** from the `/repos` page. |
| **`git clone` gives an empty repo** | The gateway wasn't seeded from the upstream. On `/repos`, click **Sync from upstream** for that repo (set the credential first if it's an HTTP upstream). |
| **Push is accepted but never appears on the upstream** | The gateway→upstream credential is missing/wrong on the *upstream* side. Verify it exists: **PAT** at Gitea `/user/settings/applications` or GitHub `settings/tokens`; **deploy key** (with write access) at the repo's Deploy Keys page. For an `ssh://` upstream also confirm the gateway trusts the host key (`ssh-keyscan`). See [Step 4: where these live on the upstream](#step-4-register-the-repo-to-guard). |
| **`src refspec main does not match any`** | Your local repo has no commits yet (or the branch is `master`). Make a commit, then `git push -u origin main`, or push `master` / rename with `git branch -m main`. |
| **Push rejected, but no PR comment appears** | Auto-PR delivery failed - check the **Auto-PR → Repos** tab, which now shows the error + hint inline (or `docker compose logs nimblegate \| grep "notification daemon"`). An **HTTP 403** means the token lacks comment scope: GitHub → classic `repo` or fine-grained **Issues: Read and write** + **Pull requests: Read**; Gitea → `write:issue`; GitLab → `api` (Step 6). After rotating the token, click **Retry now** on the repo's row. |
| **`bind: address already in use`** at install | Another service holds 7900 or 2222. Set `NIMBLEGATE_DASHBOARD_PORT` / `NIMBLEGATE_SSH_PORT` (inline or in `.env`). |
| **Forgot the admin password** | See [Operations → Forgot the admin password](operations.md#forgot-the-admin-password). |

More operator gotchas (stale client shims, `master`-vs-`main`, bare-repo
ownership) are in [`docs/troubleshooting.md`](troubleshooting.md).
