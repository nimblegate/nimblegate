# nimblegate

> Git push guardrails for AI agents: block unsafe pushes consistently, forward safe ones, record every decision.

**Status:** v0.1.0 · used in production since early 2026

nimblegate sits **between your AI agent and your real git host**. Every push your
agent makes is checked against the rules you turned on; clean pushes forward to
your upstream in under a second, unsafe ones are held with a clear report. Same
input, same answer, every time.

**→ Try the [live demo](https://demo.nimblegate.com): click through a real dashboard over sample data, nothing to install.**

**→ New here? The [Getting Started guide](docs/getting-started.md) walks you from
install to your first guarded push, step by step, no git expertise assumed.**

---

## Contents

- [Quick install](#quick-install)
- [How it works](#how-it-works)
- [The workflow](#the-workflow)
- [Auto-PR: the agent fix-loop](#auto-pr-the-agent-fix-loop)
- [What it catches and how it acts](#what-it-catches-and-how-it-acts)
- [MCP analytics for agents](#mcp-analytics-for-agents)
- [Guides](#guides)
- [Where the responsibility lives](#where-the-responsibility-lives)
- [What's in the non-commercial version](#whats-in-the-non-commercial-version)
- [Commercial use · License · Privacy](#commercial-use)

---

## Quick install

Self-hosted in one container. You need **Docker** with the **`docker compose`** plugin on the
machine that runs the gateway.

**1. Start the gateway.**

```bash
curl -O https://raw.githubusercontent.com/nimblegate/nimblegate/main/compose.yaml
docker compose up -d
```

**2. Claim the admin account.** Grab the one-time setup token, then set your password:

```bash
docker logs nimblegate | grep nbg-setup     # prints the setup token
```

Open **http://localhost:7900/setup**, paste the token, choose your admin password.

> **Remote/headless host?** The dashboard binds the host's loopback (it's the admin surface). Tunnel to it:
> ```bash
> ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>   # then open http://localhost:7900
> ```
> Use `127.0.0.1`, **not** `localhost` (it's published on IPv4). For a trusted LAN instead, set `NIMBLEGATE_DASHBOARD_HOST=0.0.0.0`. → [dashboard access options](docs/getting-started.md#dashboard-access)

**3. Register the repo to guard.** In the dashboard: **Repos → Add a repo**, then fill in:

- **Name** - letters, numbers, hyphens (e.g. `my-app`). This is the name you'll push to.
- **Upstream URL** - your real repo as **HTTPS**: `https://github.com/you/my-app.git` (not the `git@…` SSH form). The container relays over **HTTPS only** - it ships without an SSH client by design - and a PAT authenticates **both public and private** repos.
- **Upstream credential** - a **Personal Access Token** from your git host, scoped to **write that repo**:
  - GitHub **fine-grained**: repository = that repo, **Contents → Read and write**
  - GitHub **classic**: the `repo` scope
  - GitLab / Gitea: a token with write/push on the repo

  *(Auto-PR's fix-loop needs a little more - see [Getting Started](docs/getting-started.md#step-4-register-the-repo-to-guard).)*

Click **Register**. If the upstream **already has commits**, click **Sync from upstream** on the repo row so the gateway mirrors its current history (so your existing clones push cleanly) - a brand-new/empty upstream needs no sync. The **core** rule kit applies automatically. → [other git hosts, scoped access](docs/getting-started.md)

**4. Authorize your push key.** On your dev machine, print your SSH **public** key:

```bash
cat ~/.ssh/id_ed25519.pub
```

Copy the whole line, then in the dashboard go to **Keys → Add a key**, paste it, and save. The gateway only ever sees your **public** key - never the private one.

**5. Point your computer at the gateway and push.** Add the gateway as your remote and push to it instead of your real host.

> **This hop is SSH, not HTTPS.** You push to the gateway over **SSH on port 2222** - that's *not* the HTTPS upstream URL from step 3. The two hops use different protocols on purpose: **your machine → gateway = SSH**, **gateway → your real host = HTTPS**.

```bash
git remote set-url origin ssh://git@<gateway-host>:2222/~/my-app.git   # my-app = the name from step 3
git push
```

> **Note the `~/` in the URL** - it's required, not a typo. On the gateway the SSH user is locked to **git-shell** (it can run git push/clone and *nothing* else - no shell, no commands), which resolves repo paths relative to its home. So the path is `~/<repo>.git`, not a bare `/<repo>.git`. This restriction is a security feature: a key can only move git data through the gate, never run commands or read the gateway's upstream token - so a compromised dev key can't bypass the gate or steal your credential.

**6. See it work.** A clean push is accepted and forwarded to your upstream; a push that trips a rule is blocked and never reaches the real host. Watch it live on the dashboard **Feed**.

Bare-metal install, SSH-key upstreams, multi-dev scoped access, or public TLS? → **[Getting Started](docs/getting-started.md)** - full walkthrough + troubleshooting.

---

## How it works

There are **three places**, and keeping them straight is the whole game:

```
   YOUR COMPUTER                THE GATEWAY                   THE UPSTREAM
   (you / your agent            (nimblegate)                  (GitHub / Gitea / GitLab:
    write + git push)                                          your real repo)

   git push ──────────────────► checks your rules ─forwards─► stores the code
   git clone ◄───────────────── serves the code
```

- **Your computer only talks to the gateway:** you push to it and clone from it,
  never the upstream directly (that would skip the checks).
- **Only the gateway talks to the upstream:** it holds the credential and
  forwards clean pushes for you.

Your commits arrive at the upstream **byte-for-byte unchanged**: same SHA, same
author, same signature. The gateway checks and forwards; it never rewrites. You
see every push and decision live on the dashboard, and you can change the rules
anytime; the next push uses the new set.

**What that gets you:**

- **Hours back on review.** The gate is the filter between an agent's output and
  your real repo, so you're not eye-scanning every generated diff yourself.
- **Caught at push time, not deploy time.** Leaked credentials, force-pushes to
  main, schema drift: held at the gateway, never in your upstream history.
- **A reviewer the fix-loop converges against.** "Push → fail → fix → pass" only
  works when the reviewer answers the same way every time. Pattern checks do; AI
  reviewers don't.
- **Proof of what it stopped.** A live feed of every decision, plus a stats page
  estimating the hours saved.
- **Invisible when clean.** A clean push forwards in under a second; the gate
  speaks up only when there's something to see.

Full walkthrough with the mental model spelled out: **[Getting
Started](docs/getting-started.md)**.

---

## The workflow

Day to day, the gate fits the normal feature-branch flow:

1. **Your agent works on a feature branch** - coding agents often use a git
   worktree per task, so several branches are in flight at once (one task, one
   branch) - and pushes it **to the gateway**.
2. The gate checks that push. **Clean** → forwarded to your upstream, where a PR
   is opened (`feature → main`). **Finding** → **rejected**, and the bad commit
   never reaches the upstream.
3. On a rejection with an open PR, the gate posts the finding as a PR comment
   (see Auto-PR below) - the agent reads it, fixes, and re-pushes until it passes.
4. **You review the PR and merge it to `main`** on your git host, as usual.

Because rejected commits bounce at the gate, the branch on your upstream only
ever holds gated-clean code - so by the time you review a PR, it has already
passed the automated checks; you spend review time on judgment, not hunting for
leaked keys or `rm -rf`. Set the repo's **protected refs to `refs/heads/*`** so
the agent's *feature* branches are gated, not just `main`.

---

## Auto-PR: the agent fix-loop

When a push is rejected, the gateway posts the findings as a **structured comment
on the upstream PR** and fires a **webhook** with the same JSON, so an agent
(Claude Code, Cursor, Copilot, custom CI) reads the rejection and fixes itself.
The sticky comment updates in place, tracking **attempt N/M** and @-mentioning the
agent; when a later push passes, it flips to **✅ All findings resolved** and the
loop closes. The `/auto-pr` dashboard surfaces it as Dashboard / Repos / Activity
/ Setup tabs.

Minimum config (per repo, `/auto-pr` → Setup or `gateway.toml`): set
`[notification] enabled = true` and a mention handle and/or webhook URL. The
upstream token needs **comment** permission (separate from the relay's push
permission): **GitHub** - classic `repo`, or fine-grained **Issues: Read and
write** + **Pull requests: Read** (PR comments use the Issues API); **Gitea** -
`write:issue`; **GitLab** - the **`api`** scope (no narrower one allows MR
comments). If deliveries fail, the **Auto-PR → Repos** tab shows the error +
a **Retry now** button. Full setup + receiver examples:
**[notifications.md](docs/notifications.md)**.

---

## What it catches and how it acts

nimblegate ships **45 rules ("frames")** for the mistakes agents make most, grouped
into one-click **kits**:

- **`core`** *(every repo)*: hardcoded credentials, committed private keys,
  force-push to protected branches, `--no-verify` bypass, `rm -rf` of protected
  paths, `curl | sh` patterns, DB schema drift, non-idempotent migrations.
- **`web-app`** / **`cf-pages-project`** / **`cf-workers-project`**: stack-shaped
  bundles (HTML/SEO, Cloudflare Pages + D1, Workers).
- **`security-strict`** / **`encoding-strict`**: the invisible-Unicode attack
  class and paste-corruption that silently breaks parsers. Stackable on any kit.

Plus your own **regex rules**, authored from the dashboard with a live "would have
flagged N files" preview: no code, no rebuild. Choosing and authoring policy:
**[policy-authoring.md](docs/policy-authoring.md)**. Full catalog:
**[frames.md](docs/frames.md)**.

Every repo then runs those rules in one of two modes, set per repo:

| | Enforce (default) | Observe (`observe = true`) |
|---|---|---|
| **BLOCK finding** | push **rejected** | recorded, **relayed** |
| **Pusher / agent sees** | findings + `push rejected` | **nothing**, silent |
| **Operator sees** | dashboard + audit log | dashboard + audit log (⚠ banner) |
| **Acts as a guardrail?** | **yes** | **no, advisory only** |
| **Use it for** | day-to-day protection | measuring/benchmarking agents |

Observe mode is **deliberately silent** so an agent can't adapt to the gate; it
measures behavior, it is **not** a safety net (a credential or `curl | sh` is
logged and let through). Switching modes takes effect on the next push.

---

## MCP analytics for agents

The gateway exposes a read-only analytics API (MCP + REST) so an agent can query
the decision log directly: *"which repo had the biggest bounce-back this month?"*,
*"how much debugging time has the gate prevented?"*, *"what changed this week?"*,
answered from SQL over the validated log. Mint a bearer token
(`nimblegate gateway token new <label>`) and point your MCP client at
`http://<host>:7900/mcp`. No agent? The same reports are one-click buttons on the
dashboard **Reports** page. Reference: **[agent-api.md](docs/agent-api.md)**.

---

## Guides

| Guide | What's in it |
|---|---|
| **[Getting started](docs/getting-started.md)** | Install → first guarded push, step by step, with a troubleshooting quick-check. Start here. |
| **[Policy authoring](docs/policy-authoring.md)** | Choosing frame kits, writing custom regex rules, fitting alongside your linters + CI. |
| **[Frame catalog](docs/frames.md)** | Every built-in frame: what each category catches, severity, tiers. |
| **[Auto-PR / notifications](docs/notifications.md)** | The fix-loop rail: config, webhook receivers, troubleshooting. |
| **[Agent API](docs/agent-api.md)** | MCP + REST analytics surface for agents. |
| **[Operations](docs/operations.md)** | Update, backup/recovery, monitoring, password reset. |
| **[Dev-machine setup](docs/server/DEV-MACHINE-SETUP.md)** | Making the gateway a real boundary (close the bypass paths). |
| **[Troubleshooting](docs/troubleshooting.md)** | Operator gotchas beyond the getting-started table. |
| **[Security model](docs/security-model.md)** | What's hardened, the trust boundaries, the threat model. |

---

## Where the responsibility lives

nimblegate is honest about its scope so it stays trustworthy:

- **The rules you enable are exactly what gets caught.** A rule that isn't on
  catches nothing; a pattern outside the rule set flows straight through. The tool
  is *secure as configured*: coverage is whatever you turn on.
- **It is not a substitute for human review of human work.** The audience is
  agent pushes, code generated faster than anyone reads every line.
- **It is not a vulnerability scanner or an AI reviewer.** It runs the same
  pattern checks the same way every time. That's the value, and the limit.

The *tool itself* is hardened: separate machine your agent can't reach, holds the
only upstream credential, single-admin auth, append-only audit log. But **you're
the operator; the coverage is yours to decide.** Full version:
[security-model.md](docs/security-model.md).

**Operator security - this part matters:**

- **Use a separate, minimal-scope upstream token per repo, and rotate it
  regularly.** A fine-grained PAT scoped to *Contents: write on that one repo*
  (nothing wider) means a leaked token can push only to that repo, not your whole
  account - and rotation bounds how long any leak stays useful. Encrypting the
  token at rest does **not** protect a *running* gateway (it must decrypt the
  token to use it); minimal scope + rotation are what actually limit the blast
  radius.
- **The guarantee depends on correct deployment.** Run the gateway on a
  **dedicated box** your agent can't otherwise reach, keep the git SSH user on
  **git-shell** (push/clone only - no shell, no commands), don't hand out keys or
  expose the box loosely, and never authorize a dev key directly on the upstream.
  **Set up improperly, it can be compromised:** an attacker who obtains a shell as
  the git user could read the stored upstream token and push **directly to your
  real repo, bypassing the gate entirely.** Treat the gateway as
  security-sensitive infrastructure.

---

## What's in the non-commercial version

Non-commercial use is free and unrestricted under [PolyForm Noncommercial 1.0.0](LICENSE)
- no time limit, no feature gating, today and for good.

**Free, non-commercial** - for example:

- Personal projects and your own learning
- Research and teaching
- Use by a non-profit / not-for-profit
- Trying it out before a commercial decision

**Needs a commercial license** (`contact@nimblegate.com`) - for example:

- Use inside a for-profit business, including gating code that ships a paid product
- Offering nimblegate, or a derivative, as a hosted service to others

The [LICENSE](LICENSE) is authoritative; if your case is unclear, just ask.

You get:

- **One combined container** (sshd + dashboard, s6-supervised, auto-restart).
- **Single-admin auth** built in (bcrypt, server-side sessions, setup-token bootstrap).
- **Same-rules-every-time gate** across pre-commit / pre-receive / post-receive.
- **45 built-in frames** + your own regex linters.
- **Auto-PR rail** (vendor-neutral): structured PR comment + webhook on every
  rejected push, `@bot` mention + multi-bot rotation, loop guardrails, fix-loop
  closure, exponential-backoff delivery with deadletter. Gitea + GitHub adapters.
- **Dashboard**: live feed, time-saved/recurring stats, policy + custom linters +
  whitelist, repo lifecycle, frames catalog, Auto-PR tabs, health, SSH key
  management, events log, per-page help.
- **Upstream credential management** + **whitelist** (with required reason).

---

## Commercial use

Using nimblegate in a for-profit setting requires a commercial license - a flat
**$99/year per company** (all your devs, the app as-is, best-effort email support,
no SLA). Terms and the buy link: **[commercial license](COMMERCIAL.md)**.

Larger organisations needing an SLA, signed terms, or bespoke features: email
`contact@nimblegate.com`.

## Your responsibility

You define which rules fit your repo, stack, and threat model; you review the
coverage you enabled and test the gate in your environment; you keep the rest of
your security posture in place (nimblegate is **one layer**); and you keep the
gateway machine itself secure. Found a bypass or coverage gap? Please report it
via [SECURITY.md](SECURITY.md).

## Privacy

No telemetry. The binary phones home zero times. The gateway logs what *you push
to it* and sends nothing anywhere except your configured upstream. See
[PRIVACY.md](PRIVACY.md).

## License

nimblegate is source-available under **[PolyForm Noncommercial 1.0.0](LICENSE)**,
provided AS-IS without warranty. Non-commercial use is free and unrestricted,
today and for good. **Commercial use requires a [commercial license](COMMERCIAL.md)**
($99/year per company; email `contact@nimblegate.com` for larger orgs).

## Contributing · Security

- **Donations:** [GitHub Sponsors](https://github.com/sponsors/nimblegate) keep
  the [demo gateway](https://demo.nimblegate.com) hosted and the project alive.
- **Contributing:** PRs welcome for rules, docs, and fixes; see
  [CONTRIBUTING.md](CONTRIBUTING.md).
- **Security:** don't open a public issue. Email `security@nimblegate.com`. See
  [SECURITY.md](SECURITY.md).
