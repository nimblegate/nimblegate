# nimblegate: dev machine setup

The dev machine is where you run your AI agent + git client. For the gateway to be a real boundary (not a suggestion), this machine must hold credentials to **the gateway only**, never to the upstream git service. This doc covers from-scratch setup and how to retrofit an existing machine.

For the gateway-side hardening story, see [`SECURITY-MODEL.md`](SECURITY-MODEL.md). The two docs are complementary.

---

## The load-bearing principle

The agent runs as your user on this box. It reads `~/.ssh/`, `~/.git-credentials`, `~/.netrc`, your shell environment, your SSH agent socket. Anything you can use to push directly to upstream, the agent can use too. The gateway gates pushes that go *through* it; it cannot stop a push that bypasses it.

**Rule:** one credential per box. The agent's box gets the gateway-only credential. If you want to push directly to upstream as a human, do it from a *different* device (your personal laptop, the web UI, a yubikey-only key on a separate machine).

The threat is mundane: a credential left in `~/.git-credentials` from before nimblegate existed, an SSH key the agent's box happens to have registered on GitHub, an LSP that wraps `gh` and inherits its token. None of these are exotic; they're the normal state of a developer machine. Closing them is the entire point of dev-machine hygiene.

**Your authorship is preserved.** Routing through the gateway does NOT change commit authorship: your `git config user.name` still appears on every commit at the upstream. The gateway relays commits byte-for-byte (same SHA all the way through) and only records itself as the *pusher* in the upstream's Activity feed, separately from the commits. See [`SECURITY-MODEL.md`](SECURITY-MODEL.md#identity-preservation-through-the-gateway) for the design rationale.

---

## From-scratch setup

Assumes nimblegate is already running on a separate host (see [`README.md`](../../README.md) "Quick start").

### 1. Generate one SSH keypair, never to be registered upstream

```bash
ssh-keygen -t ed25519 -C "agent@$(hostname)" -f ~/.ssh/id_ed25519
```

This pubkey will be added to the gateway only. Never add it to GitHub / GitLab / Gitea / Bitbucket.

### 2. Add the pubkey to the gateway

Open `<gateway>:7900/ssh-keys`, paste `~/.ssh/id_ed25519.pub`, click **Authorize key**.

### 3. Wipe any existing upstream credentials

If this box has been used before, sweep every path that git might read a credential from:

```bash
# HTTPS credential store (the most common leak path)
rm -f ~/.git-credentials
git config --global --unset credential.helper 2>/dev/null

# .netrc (older HTTP basic-auth path; git still uses it via curl)
sed -i.bak '/your-upstream-host/d' ~/.netrc 2>/dev/null

# Provider CLIs that cache tokens
gh auth logout 2>/dev/null    # GitHub
glab auth logout 2>/dev/null  # GitLab
tea logout --all 2>/dev/null  # Gitea
```

Less common (check these too if your workflow has ever touched them):

- `git config --global --get-regexp "url\."`: URL rewrites pointing at upstream
- macOS keychain: `security find-internet-password -s github.com`
- libsecret (Linux GNOME): `secret-tool search server github.com`
- `~/.config/gh/hosts.yml` (gh CLI multi-host)
- `~/.password-store/` (pass + gpg)
- Editor settings: VSCode `~/.config/Code/User/settings.json` may have `"github.gitAuthentication": true`

### 4. Configure git to use the gateway URL format

For every repo:

```bash
git clone git@<gateway>:/srv/gateway/repos/<name>.git
# OR for an existing checkout:
git remote set-url origin git@<gateway>:/srv/gateway/repos/<name>.git
```

For convenience, add one SSH config entry:

```
# in ~/.ssh/config
Host gw
  Hostname     <gateway>
  User         git
  Port         2222
  IdentityFile ~/.ssh/id_ed25519
  IdentitiesOnly yes
```

Then remotes can be short:

```bash
git remote set-url origin gw:<name>.git
```

### 5. Disable SSH agent forwarding to / from this box

If you SSH from this box to another host using `-A` or `ForwardAgent yes`, processes on the other host can reach back through the tunnel and use your keys. If you SSH from a credential-holding host TO this box with forwarding on, the agent on this box can reach the forwarded sockets.

In `~/.ssh/config`:

```
Host *
  ForwardAgent no
```

Add explicit `ForwardAgent yes` only for hosts where it's necessary, and never for a host that holds upstream credentials.

### 6. Verify the air gap

The load-bearing step. After setup, every probe below should fail. If any succeeds, you're not done: go back to step 3 and find the credential.

```bash
# SSH to common upstreams - should refuse this box's pubkey
for host in github.com gitlab.com bitbucket.org <your-gitea-host>; do
  printf "%-30s " "$host:"
  ssh -o BatchMode=yes -o ConnectTimeout=4 -T git@$host 2>&1 \
    | grep -qE "(successfully authenticated|Welcome|logged in)" \
    && echo "BYPASS - pubkey accepted" \
    || echo "OK - refused"
done

# HTTPS clone of any known-private upstream repo - should prompt or fail
git ls-remote https://github.com/<your-org>/<your-private-repo>.git 2>&1 \
  | grep -qE "Authentication failed|could not read Username|fatal" \
    && echo "OK - HTTPS refused" \
    || echo "INVESTIGATE - HTTPS may be working"
```

---

## Retrofitting an existing machine

If you've been using direct upstream access and now want to switch to gateway-only:

### 1. Inventory direct-upstream remotes

Find every checkout that points at upstream instead of the gateway:

```bash
find ~ /media -maxdepth 5 -name "config" -path "*/.git/*" 2>/dev/null \
  | xargs grep -lE "url = (https?|git@|ssh://)[^/]*(<your-upstream-host>|github\.com|gitlab\.com)" 2>/dev/null
```

### 2. Re-point each to the gateway

For every match:

```bash
cd <repo-path>
git remote set-url origin git@<gateway>:/srv/gateway/repos/<name>.git
```

If the repo doesn't exist on the gateway yet, register it via the dashboard's `/repos` page first.

### 3. Wipe credential stores

Same as From-scratch step 3 above; every path matters.

### 4. Rotate any upstream credential the box has read

If `~/.git-credentials` held a PAT or password, treat it as exposed. Revoke it in the upstream's settings UI, generate a new one, and set it **only on the gateway** (dashboard → repo → "Rotate upstream credential").

The leaked credential might be in shell history too: `grep -E "(ghp_|glpat-|gh[so]_)" ~/.bash_history ~/.zsh_history 2>/dev/null` finds GitHub / GitLab tokens. If any turn up, rotate them.

### 5. Verify

Run the air-gap probe from From-scratch step 6. Confirm every probe says OK before resuming work.

---

## Two-key setup (recommended once you also deploy to the gateway)

The from-scratch flow generates one SSH key for git push. That's enough if you only push code. Once you also need to **deploy binaries** to the gateway (`scp` + `systemctl restart`), an agent on your dev machine inherits whatever key you use. The fix: a second, **passphrase-protected** key just for admin SSH; the agent can't use it without typing the passphrase. The push key stays passphrase-less so the agent can push autonomously through the gate, where content filtering does the actual gating.

### 1. Generate two keys with distinct comments

```bash
# Admin key - passphrase-protected. Enter a strong passphrase when prompted.
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_nbg_admin -C "$(whoami)-admin@$(hostname)"

# Push key - no passphrase (-N ''). Distinct comment so future sed-by-comment
# cleanup doesn't catch both keys.
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_nbg_push  -N '' -C "$(whoami)-agent@$(hostname)"
```

Distinct comments (`-admin` vs `-agent`) matter: `cat authorized_keys` shows them side by side, and a `sed '/.../d'` cleanup that targets just one role won't accidentally remove both.

### 2. Install each in the right place

```bash
# Admin key → /root/.ssh/authorized_keys on the gateway (one-time, uses root password)
ssh-copy-id -i ~/.ssh/id_ed25519_nbg_admin.pub root@<gateway>

# Push key → dashboard /ssh-keys page (browser).
# Paste the output of: cat ~/.ssh/id_ed25519_nbg_push.pub
```

The dashboard writes the push key to `/srv/gateway/ssh/authorized_keys`. On bare-install gateways, `sshd` reads from `/home/git/.ssh/authorized_keys` by default; bridge them once with a symlink (the container image sets this up automatically):

```bash
ssh root@<gateway>
mkdir -p /srv/gateway/ssh /home/git/.ssh
chown git:git /srv/gateway/ssh && chmod 700 /srv/gateway/ssh
ln -sf /srv/gateway/ssh/authorized_keys /home/git/.ssh/authorized_keys
chown -R git:git /home/git/.ssh
chown -h git:git /home/git/.ssh/authorized_keys
chmod 700 /home/git/.ssh
```

### 3. SSH config aliases

```
# in ~/.ssh/config
Host nbg-admin
  Hostname       <gateway>
  User           root
  IdentityFile   ~/.ssh/id_ed25519_nbg_admin
  IdentitiesOnly yes

Host nbg
  Hostname       <gateway>
  User           git
  IdentityFile   ~/.ssh/id_ed25519_nbg_push
  IdentitiesOnly yes
```

### 4. Verify both paths

```bash
ssh nbg-admin 'hostname && id'   # prompts for passphrase first time; should print gateway hostname + uid=0
ssh -T nbg                        # should error with "Interactive git shell is not enabled." - that's success
```

### 5. Deploying binaries (admin path)

```bash
# Cache the passphrase once for a short window so scp/ssh don't prompt per command
ssh-add -t 30m ~/.ssh/id_ed25519_nbg_admin

# Then deploy
scp build/nimblegate-linux-amd64 nbg-admin:/tmp/
ssh nbg-admin '
  install -m 0755 /tmp/nimblegate-linux-amd64 /usr/local/bin/nimblegate
  systemctl restart nimblegate-dashboard.service
  nimblegate version
'
```

The 30-minute cache window is a deliberate trade: convenience for deploys vs. tightness of the air gap. **During the cache window, processes on your dev machine that share the ssh-agent socket CAN reach root@gateway without a prompt.** If that matters for a given session, narrow the window or drop the cache:

```bash
ssh-add -t 5m  ~/.ssh/id_ed25519_nbg_admin   # tighter window
ssh-add -D                                    # drop all cached identities now
```

For routine push work the agent never touches the admin path at all; its only route is `nbg` (git-shell, content-gated).

### 6. Rotating a key

Always use the comment as the cleanup primary key. Pattern:

```bash
# On the gateway, surgically remove just one role's key:
sed -i.bak '/-admin@/d' /root/.ssh/authorized_keys   # removes the admin key only
# (or '/-agent@/d' for the push key in /srv/gateway/ssh/authorized_keys via dashboard's delete control)
```

If keys default to bare `<user>@<host>` (no role suffix), the same `sed` will match every line. That's the comment-collision trap; distinct comments at keygen time prevent it.

---

## What to never do

- **Never register the dev machine's pubkey directly on GitHub / GitLab / Gitea.** That makes the gateway optional.
- **Never paste a PAT into a config file on the dev machine.** PATs belong on the gateway, scoped narrowly (`write:repository` only).
- **Never enable SSH agent forwarding from a credential-holding box to the dev machine.** The agent on the dev machine can reach the forwarded sockets.
- **Never share the dev machine's SSH key across machines.** Each machine gets its own key with a hostname-tagged comment.
- **Never claim "agents can't push directly" without verifying.** Run the air-gap probe.

---

## Related

- [`SECURITY-MODEL.md`](SECURITY-MODEL.md): gateway-side hardening, threat model, quarterly verification.
- [`../../README.md`](../../README.md): the short version of dev-machine setup is in "Quick start".
