# Gateway setup: Proxmox trixie LXC (dev → gateway → Gitea)

A worked runbook for running the nimblegate policy gateway **bare-metal** on a Debian **trixie** Proxmox CT (2 GB RAM / 2 cores / 25 GB is plenty), gating pushes between your dev machine and your Gitea.

```
  this dev machine                gateway CT 192.0.2.10             Gitea
  origin = gateway   --ssh push-> nimblegate gate (pre-receive) --http relay-> 192.0.2.20:3000
  (no direct Gitea push)          holds the Gitea push token              you/<repo>.git
```

## The two SSH connections: set these up first (this is where installs go wrong)

There are **two independent SSH hops** in this architecture. Both involve a user named `git` and SSH keys, but they run in **opposite directions with different key files**; conflating them is the #1 install snag. Get both green *before* you register any repo.

| | **① Dev → Gateway** (inbound) | **② Gateway → Gitea/GitHub** (outbound) |
|---|---|---|
| Purpose | you push to the gate | gateway relays accepted pushes upstream |
| Who connects | your dev key → the gateway's `git` user | the gateway's `git` user → Gitea/GitHub |
| **Client private key** | `~/.ssh/id_ed25519` (your push key) on your **dev machine** | `/home/git/.ssh/id_ed25519` on the **gateway** |
| **Server registers the .pub** | in the gateway's `/home/git/.ssh/authorized_keys` | as a **deploy key with write** in Gitea/GitHub |
| Host key trusted by | your dev machine trusts the gateway | the **gateway's** `git` user trusts Gitea/GitHub (`ssh-keyscan`) |
| Login shell (server side) | `git-shell`, push/fetch only, never an interactive shell | n/a |

**The trap that bites everyone:** the gateway's `git` user owns **two different key files with opposite roles**:

- `authorized_keys`: the public keys allowed to connect **IN** (your dev keys). **INBOUND.**
- `id_ed25519` / `id_ed25519.pub`: the gateway's **OWN** identity used to reach **OUT** to Gitea. **OUTBOUND.**

They are not the same key and not interchangeable. Hop ① fails (`Permission denied`) if your dev key isn't in `authorized_keys`; hop ② fails **silently** if the gateway's own key isn't a registered deploy key, or the gateway hasn't trusted Gitea's host key.

Also: `git` means two different things. On the **gateway** it's the local OS user (with `git-shell`). On **Gitea/GitHub SSH**, *every* connection authenticates as their `git` user and is identified by your key, so `git@<host>` is always the right SSH user, never a personal username.

> **Prefer HTTP for hop ②?** It can use a **PAT** (`user:token`) instead of a deploy key, simplest on a LAN: one credential file, no per-user SSH plumbing, no host-key step. See [Upstream auth: HTTP token vs SSH key](#upstream-auth-http-token-vs-ssh-key) below. Hop ① is always SSH.

Once both hops are green, registering a repo is the easy part: give it the **SSH upstream URL** (deploy-key auth) **or** the **HTTP URL + PAT**, plus the protected branch. The per-hop setup commands are in steps 2–5; the upstream-auth detail (both schemes, with verify commands) is in [Upstream auth](#upstream-auth-http-token-vs-ssh-key).

---

**Why bare-metal, not Docker:** the LXC *is* the separate-host isolation boundary already; Docker-in-LXC needs `nesting=1`/privileged tweaks for no gain. (If you ever do want the container: enable CT features `nesting=1,keyctl=1`, then use `deploy/gateway/docker-compose.yml`.)

**The security model, read this first.** The gate is only un-bypassable if the dev machine **cannot push to Gitea directly**; only the gateway can. Two musts:
1. The gateway holds a Gitea push token; the dev machine should *not* hold Gitea push creds for the protected repo.
2. In Gitea, set **branch protection** on the repo's `main` so only the gateway's Gitea account may push it. Otherwise anyone with Gitea creds bypasses the gate by pushing straight to Gitea.

Prerequisites: the CT is up with root SSH at **`192.0.2.10`**; you have a **Gitea access token** with repo write scope (Gitea → Settings → Applications → Generate Token) for the relay; your dev machine has an SSH key.

---

## 1. [CT, as root] Install software

```sh
apt update
apt install -y git openssh-server ca-certificates unattended-upgrades
systemctl enable --now ssh

# Enable automatic security updates so CVEs in git, openssh, openssl
# land without operator intervention. Recommended for any gateway box.
# Edit /etc/apt/apt.conf.d/50unattended-upgrades to tighten further
# (auto-reboot, mail on errors, etc) - defaults are safe.
dpkg-reconfigure -plow unattended-upgrades
systemctl enable --now unattended-upgrades
```

No Go needed on the CT: we build the static binary on the dev machine and copy it over (step 2). (`ca-certificates` is only needed if you later relay to an HTTPS upstream; harmless to install.)

**Why `unattended-upgrades` is part of the base install:** the gateway is internet-adjacent (relays pushes from authenticated agents) and runs git, which has had server-side CVEs historically (`.gitmodules` injection, submodule URL parsing, etc). The operator most likely to skip this step is the operator least likely to manually `apt upgrade` quarterly. Default-on with security-only is the conservative-correct choice. If you run your own patch management (Ansible, custom apt mirror), `systemctl disable --now unattended-upgrades` after the install.

## 2. [dev machine] Build the binary and copy it to the CT

The binary is static (`CGO_ENABLED=0`), so it runs on trixie regardless of build host:

```sh
cd /srv/projects/apps/nimblegate
export PATH="$HOME/go/bin:$PATH"
CGO_ENABLED=0 go build -ldflags="-X nimblegate/internal/version.Version=$(git rev-parse --short HEAD)" -o bin/nimblegate ./cmd/nimblegate
scp bin/nimblegate root@192.0.2.10:/usr/local/bin/nimblegate
ssh root@192.0.2.10 'chmod 755 /usr/local/bin/nimblegate && nimblegate version'   # prints the commit you just built - confirm it matches
```

Labeling the build with the commit SHA is deliberate: `nimblegate version` on the CT then tells you exactly which build is running, so you can confirm a deploy actually landed rather than guessing. A local build alone never touches the CT; you must `scp` it over.

## 3. [CT, as root] Create the git user + data dirs

```sh
useradd --create-home --shell /usr/bin/git-shell git
mkdir -p /srv/gateway/repos /srv/gateway/cfg /home/git/.ssh
chown -R git:git /srv/gateway/repos /srv/gateway/cfg /home/git/.ssh
chmod 700 /home/git/.ssh
```

`git-shell` restricts the git user to push/fetch; a key never grants an interactive shell.

## 4. [dev machine → CT] Authorize the dev's SSH key for the git user

```sh
# from the dev machine:
cat ~/.ssh/id_ed25519.pub | ssh root@192.0.2.10 'cat >> /home/git/.ssh/authorized_keys && chown git:git /home/git/.ssh/authorized_keys && chmod 600 /home/git/.ssh/authorized_keys'
```

(Use your actual pubkey filename: `id_ed25519.pub` or `id_rsa.pub`.)

## 5. [CT, as the git user] Register the repo on the gateway

Pick the repo name and your Gitea owner/user. The upstream URL carries the Gitea token **in the URL** because the relay's credential-file injection currently only triggers for `https://` (your LAN Gitea is `http://`); see the note at the end.

```sh
sudo -u git nimblegate gateway add \
  --name myrepo \
  --upstream "http://GITEA_USER:GITEA_TOKEN@192.0.2.20:3000/you/myrepo.git" \
  --protect refs/heads/main \
  --policy-root /srv/gateway/cfg \
  --repos-root /srv/gateway/repos
```

This creates `/srv/gateway/repos/myrepo.git` with the gate hooks (pointing at `/usr/local/bin/nimblegate`) and saves the policy metadata. The bare repo starts empty.

## 6. [CT, as the git user] Write the gateway-held policy

This is the source of truth for *what gets enforced*; the pushed `.appframes/` is ignored. Enable the frame groups you want:

```sh
sudo -u git sh -c 'cat > /srv/gateway/cfg/myrepo/appframes.toml' <<'EOF'
[frames]
enabled = ["@tier-1", "@web"]
EOF
```

(Run `nimblegate list --group` to see groups; start strict with `@tier-1` and add more.)

## 7. [dev machine] Point the repo at the gateway and push

In your local clone of the repo:

```sh
git remote set-url origin ssh://git@192.0.2.10/srv/gateway/repos/myrepo.git
# (first push of an existing history seeds the gateway + relays to Gitea)
git push origin main
```

- **Clean push** → accepted, relayed to `you/myrepo` on Gitea.
- **Push with a finding the policy BLOCKs** → rejected at push time with `remote:` lines naming the frame; nothing reaches Gitea.
- **Feature branches** (`git push origin feature/x`) → not gated by default, relayed freely.

## 8. Lock it down (make the gate un-bypassable)

1. On the dev machine, ensure `origin` is the gateway and you have **no other remote** pushing to Gitea directly for this repo. Remove cached Gitea push creds for it if present.
2. In Gitea: repo → Settings → Branches → protect `main`, restrict push to the gateway's Gitea account only. Now `main` can only be reached through the gate.

## Operate / verify

```sh
ssh root@192.0.2.10 'cat /srv/gateway/cfg/myrepo/audit.log'   # JSONL: each push decision + findings
ssh root@192.0.2.10 'tail /var/log/auth.log'                  # ssh access
```

## Upgrade the gateway binary

Rebuild on the dev machine (step 2) and `scp` the new binary over `/usr/local/bin/nimblegate`. Hooks reference that path, so no re-registration needed. Run `ssh root@192.0.2.10 nimblegate version` afterward and confirm it prints the commit you just built; that's how you verify the new code is actually live (the symptom of a "stale" gateway is almost always a build that was never copied over). To use the `/policy` tuning + check-authoring UI, relaunch the dashboard with `--allow-edits --repos-root /srv/gateway/repos` (see the server guide).

## Upstream auth: HTTP token vs SSH key

The gateway talks to Gitea in two places: the **relay** (pushes accepted commits upstream) and the dashboard's **Sync from upstream** (pulls existing history into the gateway). Both run **as the `git` user** (the dashboard unit is `User=git`, and without the optional relay service the relay runs inline in the git-owned post-receive hook). So whichever scheme you pick, the credentials must belong to the gateway's `git` user.

> **Failures here are silent by design.** Relay/sync errors are *not* shown to the pusher or in the dashboard (that camouflage keeps the gateway from advertising itself). When upstream delivery isn't working, don't expect an error in the UI; diagnose with the commands at the end of this section.

### Option 1: HTTP token (simplest on a LAN)

One `user:token` string, injected into the upstream URL for **both** fetch and push (the gateway does this for `http://` *and* `https://`). Set it either way:

- dashboard: repo → **Rotate upstream credential** → paste `GITEA_USER:TOKEN`, or
- embed it at registration: `--upstream "http://GITEA_USER:TOKEN@192.0.2.20:3000/you/myrepo.git"`

The token ends up in `/srv/gateway/cfg/myrepo/{credential,gateway.toml}` on the CT (the box the agent can't reach), acceptable on a trusted LAN. The PAT needs **`write:repository`** scope (write covers the relay's push; read-only can sync but can't relay). Verify it **as git, on the gateway**:

```sh
runuser -u git -- git ls-remote http://GITEA_USER:TOKEN@192.0.2.20:3000/you/myrepo.git
#   lists refs = good;  401 = wrong token or missing scope
```

### Option 2: SSH key (no token on disk, cleaner)

The credential is the git user's SSH key instead of a token in a file. It has **three** prerequisites, all on the gateway's `git` user; miss any one and relay/sync fail silently:

```sh
# on the gateway, as root:

# 1. an OUTBOUND key for the git user. This is separate from
#    /home/git/.ssh/authorized_keys, which holds INBOUND dev keys.
runuser -u git -- ssh-keygen -t ed25519 -f /home/git/.ssh/id_ed25519 -N ''
runuser -u git -- cat /home/git/.ssh/id_ed25519.pub
#    → add this PUBLIC key in Gitea: repo → Settings → Deploy Keys → Add Key,
#      and TICK "Enable write access".

# 2. trust Gitea's host key (else "Host key verification failed", silently):
runuser -u git -- ssh-keyscan -p <gitea-ssh-port> 192.0.2.20 >> /home/git/.ssh/known_hosts

# 3. register the repo with an ssh upstream and NO credential:
sudo -u git nimblegate gateway add --name myrepo \
  --upstream "ssh://git@192.0.2.20:<gitea-ssh-port>/you/myrepo.git" \
  --protect refs/heads/main --policy-root /srv/gateway/cfg --repos-root /srv/gateway/repos
```

Verify (it must authenticate **without** a password prompt):

```sh
runuser -u git -- ssh -T -p <gitea-ssh-port> git@192.0.2.20
#   "Hi <user>! You've successfully authenticated"  = good
#   "git@…'s password:"                             = key isn't a registered deploy key (Ctrl-C; redo step 1)
#   "Host key verification failed"                  = do step 2
```

(`runuser -u git -- …` is used throughout because the git user's login shell is `git-shell`; it also sidesteps a broken `sudo` if `sudo -u git` errors with `audit plugin`.)

### Diagnosing a silent failure

When the relay or Sync isn't working but nothing shows in the dashboard, read the operator-side logs:

```sh
cat /srv/gateway/cfg/_events.jsonl        # relay-failed events (the real reason)
cat /srv/gateway/cfg/myrepo/audit.log     # per-push gate decisions
journalctl -u nimblegate-dashboard        # Sync-from-upstream errors
```
