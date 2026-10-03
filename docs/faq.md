# FAQ

Short answers, each with a link to the full story.

## Is it free?

Yes, for non-commercial use: personal projects, learning, research, teaching
and non-profits. That's the whole app, with no time limit and no feature locked
away. The licence is [PolyForm Noncommercial 1.0.0](../LICENSE).

## What does commercial use cost?

Using nimblegate in a for-profit setting, including gating code that ships a
paid product, needs a commercial licence: **$10 a month or $99 a year per
company**, covering all your developers, with best-effort email support and no
SLA. Terms and buy links: [commercial licence](../COMMERCIAL.md). Larger
organisations needing an SLA or signed terms: `contact@nimblegate.com`.

## Does it phone home?

No. There is no telemetry, no usage analytics and no crash reporting. The only
network traffic is forwarding accepted pushes to your own git host, plus
anything you configure yourself, such as a webhook. Secrets found in a push are
recorded as metadata only (file, line, rule), never the secret itself. See
[privacy](../PRIVACY.md).

## Does it change my commits?

No. Accepted pushes arrive at your git host byte for byte: same SHAs, same
author, same signatures. The gateway checks and forwards; it never rewrites.

## Which git hosts does it work with?

Pushes are forwarded to any git host over HTTPS with a token: GitHub, GitLab,
Gitea, Forgejo and others. Auto-PR comments on pull requests currently work on
GitHub and Gitea. Writing an adapter for another host:
[upstream adapters](adapters.md).

## Why not just a pre-commit hook?

A pre-commit hook runs on the machine the agent controls. It can be skipped
with `--no-verify`, edited, or simply missing in a fresh clone or worktree,
since git doesn't copy hooks. nimblegate runs on a separate machine the agent
has no shell on, and it holds the only credential for your real git host, so
there is nothing to skip and no way around it. Keep your hooks for fast local
feedback; the gate is the layer that holds when they're skipped.

## Why not branch protection or CODEOWNERS?

They protect the *merge*, not the *push*. By the time they act, a leaked key
is already an object in your git host's history, and deleting the branch
doesn't un-leak it: rotating the key is the only real fix. The gate rejects
the push, so the object never reaches your host. Keep branch protection too;
they solve different problems.

## Why not CI?

CI runs after the push has landed, and its configuration lives in the repo the
agent is editing, so the agent can weaken it. The gate runs before anything
lands, and its rules live on the gateway, out of the agent's reach.

## Why not an AI code reviewer?

An agent fixing its own push needs a reviewer that gives the same answer every
time: push, fail, fix, pass. nimblegate's checks are deterministic pattern
checks with no AI in the loop, so the loop converges. An AI reviewer is useful
for judgment, but it can answer differently on the same code.

## What doesn't it do?

- **It catches what you turn on.** A rule that's off catches nothing, and a
  problem outside the rule set goes straight through. See
  [choosing what the gate checks](policy-authoring.md).
- **It isn't a vulnerability scanner or a code reviewer.** It runs the same
  pattern checks the same way every time; that's the value and the limit.
- **It isn't a substitute for reviewing human work.** It's built for agent
  pushes, code written faster than anyone reads every line.
- **It's only as strong as its deployment.** Run it on a separate machine the
  agent can't reach, keep the git user on git-shell, and don't leave the agent
  a direct route to your git host. Set up loosely, someone with a shell on the
  gateway could read the upstream token and push around the gate. See
  [hardening the gateway](server/SECURITY-MODEL.md) and
  [dev machine setup](server/DEV-MACHINE-SETUP.md).

## Does it slow pushes down?

A clean push is checked and forwarded in about a second. Large repos have a
time budget for the checks; see [operations](operations.md#self-maintaining-storage).

## Where do I report a bug or a security problem?

Bugs and ideas: [GitHub issues](https://github.com/nimblegate/nimblegate/issues).
Security problems: email `security@nimblegate.com`, not a public issue. See
[security policy](../SECURITY.md).
