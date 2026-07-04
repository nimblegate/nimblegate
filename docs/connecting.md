# Connecting your machine and agents - Windows / macOS / Linux

Once your gateway is running (see [Getting started](getting-started.md)), this
guide gets your dev machines and AI agents pushing through it, on any OS, with
a troubleshooting section at the end.

You need two facts from your setup, used throughout:

- **Gateway address:** `<gateway-host>` - your gateway's IP or hostname.
- **Repo name:** `<repo-name>` - the name the gateway knows your repo by (from
  **Repos -> Add a repo** in the dashboard).

Two ports, on purpose: **pushes** go to `git@<gateway-host>:2222`; **admin SSH**
(for the dashboard tunnel) goes to your server on port 22. Different ports,
different users, by design.

---

## Part A - For everyone who pushes code

Do this once per developer machine.

### A1. Get an SSH key (skip if you already have one)

**Windows** (PowerShell - OpenSSH ships with Windows 10/11):
```powershell
ssh-keygen -t ed25519
Get-Content $env:USERPROFILE\.ssh\id_ed25519.pub    # prints your PUBLIC key
```

**macOS / Linux** (Terminal):
```bash
ssh-keygen -t ed25519
cat ~/.ssh/id_ed25519.pub                            # prints your PUBLIC key
```

The `.pub` file is your **public** key - safe to share. The file without
`.pub` is your **private** key - never share it.

### A2. Register your key with the gateway

Add the public-key line (`ssh-ed25519 AAAA... you@machine`) in the dashboard's
**Keys** page: **Keys -> Add a key**, paste, label, save. The gateway only ever
sees your public key. You cannot push until it is registered.

### A3. Point your repo at the gateway and push

In an existing clone:
```bash
git remote set-url origin ssh://git@<gateway-host>:2222/~/<repo-name>.git
git push
```
Or clone fresh:
```bash
git clone ssh://git@<gateway-host>:2222/~/<repo-name>.git
```

The `~/` in the URL is required, not a typo (the gateway's SSH user is locked
to git-shell, which resolves paths relative to its home). A clean push forwards
to your real git host in about a second; a push that trips a rule is rejected
with a report and never reaches your host.

### A4. (Recommended) A shortcut so you never type that again

Add an SSH config entry so a plain `git push` just works and the right key is
always offered.

- **File:** Windows `$env:USERPROFILE\.ssh\config` - macOS/Linux `~/.ssh/config`

```
Host mygateway
    HostName <gateway-host>
    Port 2222
    User git
    IdentityFile ~/.ssh/id_ed25519
    IdentitiesOnly yes
```
Then the remote can be `ssh://mygateway/~/<repo-name>.git`, and SSH always
offers the right key - which fixes most "Permission denied" surprises.

### A5. Day-to-day: how you and your AI agent use it

**There is nothing to configure in your AI agent.** This is the part people
overthink. Claude Code, Cursor, Copilot, Aider, or your own scripts do not need
to know the gateway exists. Because you pointed the repo's remote at the gateway
(A3), *every* `git push` from that working copy - yours or the agent's - goes
through the gate automatically. The agent just uses git the way it always has.

The normal loop, unchanged except that pushes are now checked:

1. **Work on a feature branch** (agents often use one branch or git worktree per
   task). You and the agent commit as usual.
2. **Push to the gateway** (`git push`). A clean push forwards to your real git
   host in about a second. A push that trips a rule is **rejected**, and the bad
   commit never reaches your host.
3. **On a rejection, the reason is right there** in the `git push` output - so an
   agent watching its own command output reads why it failed, fixes the file,
   and pushes again. If Auto-PR is enabled and a pull request is open, the same
   finding also posts as a comment on the PR (and fires a webhook), so more
   automated loops can pick it up. Either way the loop closes when a push passes.
4. **Open a PR and merge on your real git host** (GitHub/GitLab/Gitea) as you
   always have. The gateway checks pushes; it does not change how you review or
   merge. By the time you look at a PR, its commits already passed the automated
   checks, so your review is about judgment, not hunting for keys.

**About that "Create a pull request" link:** when you push a new branch, your
git host (GitHub etc.) prints a "Create a pull request for ... by visiting"
link. That message is from your git host, not the gateway - and it only shows
until a PR exists for that branch, which is why you see it on some pushes and
not others. The gateway does not open PRs; it posts findings as comments on a PR
once one is open. So the practical order is: push the branch, open the PR once
(click that link), and from then on every rejected push comments on that PR.

Two things worth knowing:

- **Agents in a container or sandbox:** the agent's environment needs the SSH
  key too. Either mount your key into it, or generate a separate key inside it
  and register that one (A1-A2). Each actor can have its own key - that is also
  how the gateway tells pushes apart.
- **Fresh clones:** if an agent clones the repo anew, clone it from the gateway
  URL (A3) so the new copy is gated. A clone pulled straight from GitHub would
  push straight to GitHub and skip the gate.

To gate feature-branch pushes and not just `main`, set the repo's protected refs
to `refs/heads/*` in the dashboard.

### A6. Running more than one agent on the same repo

Point several agents (say Claude Code and Cursor) at one repo, and if one gets
stuck failing the gate repeatedly, the gateway can **hand the work off to the
next agent**, then escalate to a human:

- The gateway keeps **one comment on the PR**, updated in place, that @-mentions
  **one agent at a time**. The handoff is that mention changing, with a
  "🔄 Rotated from @agent-a to @agent-b" note explaining why.
- **A hands off to B when** either A has used its allowed attempts, or A pushed a
  "fix" that tripped the *same* finding again (clearly stuck).
- **If every agent fails,** the comment tags a fallback human, and a hard cap on
  total attempts stops the loop from spinning forever.

**The catch:** that @-mention is a *signal*, not a trigger. For agent B to
actually start when tagged, B has to be wired to respond to its mention - the
gateway also fires a webhook carrying whose turn it is. So there are two modes:
**hands-free** (each agent has a webhook receiver that kicks it off on its turn),
or **human-in-the-loop** (you see "@cursor, your turn" and start it yourself).
Configuring the agent list, attempts-per-agent, and fallback human is an admin
job - see [Auto-PR / notifications](notifications.md).

---

## Part B - Reaching the dashboard

The dashboard is deliberately not exposed to the internet. Reach it by tunneling
over admin SSH, then browsing a local URL.

**Windows (PowerShell) / macOS / Linux** - same command:
```bash
ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>
```
Leave that window open, then browse to **http://localhost:7900**.

Use **`127.0.0.1`** exactly, not `localhost`, in the `-L` part - the dashboard is
published on IPv4, and `localhost` can resolve to IPv6 first and connect to
nothing (you would see an empty page).

First time: open `http://localhost:7900/setup` and claim the one-time setup token
shown in the server's startup log / login banner.

---

## Part C - If your gateway is behind a VPN

Skip this unless your gateway sits on a mesh VPN (e.g. Tailscale) with its public
push port closed - a common hardening choice.

### C1. Join the VPN

- **Windows / macOS:** install the Tailscale app, sign in to your tailnet.
- **Linux:** `curl -fsSL https://tailscale.com/install.sh | sh` then
  `sudo tailscale up`.

Every machine that pushes, and every machine where your agents run, must be on
the tailnet.

### C2. Reach things by the tailnet address

Use the gateway's tailnet address (a `100.x.y.z` IP or its MagicDNS name):

- **Dashboard:** browse directly to `http://<tailnet-address>:7900` - no SSH
  tunnel needed.
- **Push:** as in A3, but with the tailnet address:
  `ssh://git@<tailnet-address>:2222/~/<repo-name>.git`.

With the public push port closed, pushes work only from machines on the tailnet -
which is the point: the gateway has no public attack surface.

---

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `Permission denied (publickey)` on push | Key not registered, or SSH offered the wrong key | Confirm your **public** key is added in the dashboard (A2). With several keys, use the A4 config, or push with `GIT_SSH_COMMAND="ssh -i ~/.ssh/id_ed25519 -o IdentitiesOnly=yes" git push`. |
| Dashboard shows an empty page over the tunnel | Used `localhost` instead of `127.0.0.1` in `-L` (IPv6 vs IPv4) | Re-run the tunnel with `127.0.0.1` exactly (Part B). |
| `repository does not exist` / `not a git repository` | Missing `~/` in the URL, or wrong repo name | URL must be `ssh://git@<gateway-host>:2222/~/<repo-name>.git` - keep the `~/`. |
| `ssh: command not found` (Windows) | OpenSSH client not enabled | Settings -> Apps -> Optional features -> add "OpenSSH Client", or use Git Bash. |
| Push hangs or times out | Wrong port, or a firewall in the way | Use port **2222** (not 22) for pushing; on a VPN, confirm you're connected to the tailnet. |
| Push rejected with a findings report | Working as intended - the gate caught something | Read the report, fix the flagged file(s), push again. If Auto-PR is on, the finding is also a PR comment. |
| Passphrase prompt on every push | Key has a passphrase and isn't cached | Add it once: `ssh-add ~/.ssh/id_ed25519` (Windows: `Start-Service ssh-agent` first). |

More operator-side gotchas: [Troubleshooting](troubleshooting.md).
