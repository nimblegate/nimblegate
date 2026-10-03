# nimblegate

> Git push guardrails for AI agents: block unsafe pushes consistently, forward safe ones, record every decision.

**Status:** [![latest release](https://img.shields.io/github/v/release/nimblegate/nimblegate?label=release&color=555)](https://github.com/nimblegate/nimblegate/releases/latest) · used in production since early 2026

nimblegate sits **between your AI agent and your real git host**. Every push your
agent makes is checked against the rules you turned on; clean pushes forward to
your upstream in under a second, unsafe ones are held with a clear report. Same
input, same answer, every time. It catches leaked keys and force-pushes, and the
shortcuts agents take to look finished: skipped or empty tests, CI told to ignore
failures, linters switched off for a whole file.

**→ Try the [live demo](https://demo.nimblegate.com): click through a real dashboard over sample data, nothing to install.**

**→ Read the [docs](https://nimblegate.com/docs/): quick start, guides, and a page for every rule.**

---

## Quick start

Self-hosted in one container. On a machine with **Docker**:

```bash
curl -O https://raw.githubusercontent.com/nimblegate/nimblegate/main/compose.yaml
docker compose up -d
docker logs nimblegate | grep nbg-setup     # one-time setup token
```

Open **http://localhost:7900/setup**, claim the admin login, add your SSH key and
register your repo, then point git at the gateway:

```bash
git remote set-url origin ssh://git@<gateway-host>:2222/~/my-app.git
git push
```

The full ten-minute walkthrough, ending with a blocked fake key:
**[Quick start](https://nimblegate.com/docs/quick-start)**. Bare metal, LAN,
TLS or a fresh VPS: **[full setup guide](https://nimblegate.com/docs/getting-started)**.

---

## How it works

```
   YOUR COMPUTER                THE GATEWAY                   THE UPSTREAM
   (you / your agent            (nimblegate)                  (GitHub / Gitea / GitLab:
    write + git push)                                          your real repo)

   git push ──────────────────► checks your rules ─forwards─► stores the code
   git clone ◄───────────────── serves the code
```

- **Your computer only talks to the gateway.** You push to it and clone from it,
  never the upstream directly.
- **Only the gateway talks to the upstream.** It holds the credential and
  forwards clean pushes, byte for byte: same SHAs, same author, same signatures.

Rejected pushes report the rule, file and line, so an agent can fix and push
again. With **Auto-PR**, findings also land as a PR comment and a webhook.

---

## What it catches

**50+ rules ("frames")**, applied per repo in one-click **kits**:

- **`core`** (every repo): hardcoded credentials, private keys, force-push to
  protected branches, `--no-verify`, `rm -rf` of protected paths, `curl | sh`.
- **`agent-shortcuts`**: skipped or placeholder tests, test-only code paths, a
  no-op test command, CI told to ignore failures, blanket lint disables.
- **`web-app`**, **`cf-pages-project`**, **`cf-workers-project`**,
  **`security-strict`**, **`encoding-strict`**: stack-shaped and stricter sets.

Plus your own regex rules from the dashboard. Every rule:
**[frame reference](https://nimblegate.com/docs/frames/)**.

---

## Docs

Everything lives at **[nimblegate.com/docs](https://nimblegate.com/docs/)**
(source: [`docs/`](docs/)):

- **Start:** [quick start](https://nimblegate.com/docs/quick-start),
  [access tokens](https://nimblegate.com/docs/quick-tokens),
  [run your AI agent through it](https://nimblegate.com/docs/quick-agents),
  [choose what gets checked](https://nimblegate.com/docs/quick-frames),
  [MCP for agents](https://nimblegate.com/docs/quick-mcp)
- **Operate:** [Auto-PR and notifications](https://nimblegate.com/docs/notifications),
  [multiple agents](https://nimblegate.com/docs/multi-agent),
  [operations](https://nimblegate.com/docs/operations)
- **Security:** [security model](https://nimblegate.com/docs/security-model),
  [hardening the gateway](https://nimblegate.com/docs/server-hardening)
- **Help:** [troubleshooting](https://nimblegate.com/docs/troubleshooting),
  [FAQ](https://nimblegate.com/docs/faq) (why not a pre-commit hook, what it
  doesn't do, privacy)

---

## License, privacy, contributing

- **Free for non-commercial use** under [PolyForm Noncommercial 1.0.0](LICENSE):
  the whole app, no time limit. **Commercial use** needs a licence, $10/month or
  $99/year per company: [commercial licence](COMMERCIAL.md).
- **No telemetry.** It sends nothing anywhere except your own upstream:
  [privacy](PRIVACY.md).
- **Contributing:** PRs welcome for rules, docs and fixes:
  [CONTRIBUTING.md](CONTRIBUTING.md). **Security problems:** email
  `security@nimblegate.com`, not a public issue: [SECURITY.md](SECURITY.md).
