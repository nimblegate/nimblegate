# Policy authoring: choosing what the gate checks

How to decide what each repo enforces: applying frame kits, ticking individual
frames, writing your own regex rules, and how all of that sits alongside the
linters and CI you already run. For the full catalog of built-in frames (what
each category catches, severity, tiers) see [`docs/frames.md`](frames.md).

## Contents

- [Selecting frames](#selecting-frames)
- [Custom rules (linters)](#custom-rules-linters)
- [How this differs from your normal linters](#how-this-differs-from-your-normal-linters)
- [Tests: how nimblegate fits with your test suite](#tests-how-nimblegate-fits-with-your-test-suite)

---

## Selecting frames

nimblegate ships **45 rules ("frames")** plus six starter **kits** that pre-tick a
curated set. You pick what runs per repo from the dashboard at
`/policy?repo=<name>`, the **Frame selection** section at the bottom:

- **Quick start row**: one-click apply `core`, `web-app`, `cf-pages-project`,
  `cf-workers-project`, `security-strict`, or `encoding-strict`. Stackable
  (`security-strict` + `encoding-strict` layer on top of any project-shape kit).
- **+ New custom kit**: save your own named selection of frames for one-click
  reuse. Custom kits appear at the top of the browse tree with a Delete control.
- **Browse tree**: every built-in frame organized **Category → Subcategory →
  Frame**, tickable individually. The pills row shows live counts per kit and per
  category as you toggle.

The kits at a glance:

- **`core`** *(recommended for every repo)*: hardcoded credentials, private keys
  committed by accident, force-push to protected branches, `--no-verify` bypass,
  `rm -rf` of protected paths, `curl | sh`-style shell patterns, DB schema drift,
  non-idempotent migrations.
- **`web-app`**: HTML required-meta, SEO meta, image alt-text, markup validity,
  mixed-content URLs. Includes `core`.
- **`cf-pages-project`**: SvelteKit / Astro / Next on Cloudflare Pages with D1.
  Includes `web-app`.
- **`cf-workers-project`**: Cloudflare Workers + Tunnels + Access, no HTML.
  Includes `core`.
- **`security-strict`**: every `security/*` frame, including the invisible-Unicode
  attack class (Trojan Source / bidi override, zero-width identifier forgery,
  homoglyphs). Stackable.
- **`encoding-strict`**: paste-corruption: UTF-8 BOM, curly quotes in config,
  tabs in YAML, mixed CRLF/LF, en-dash flag corruption, control bytes, zero-width
  Unicode in docs. Stackable.

State persists as a per-repo TOML file on the gateway's policy disk:

```toml
[frames]
enabled = [
    "git/folder-branch-lock",
    "security/no-hardcoded-credentials",
    "security/no-private-keys-in-repo",
    # ...
]

[ui]
applied_kits = ["core"]
```

You don't edit this file directly; the dashboard does. (If you run the
`nimblegate` CLI locally against your own repo *without* the gateway, the same
shape lives at `appframes.toml` in your repo root, manipulated by
`nimblegate kits apply <name>` / `nimblegate frames enable <id>`.)

---

## Custom rules (linters)

Beyond the 45 built-in frames, you can author your own regex-based rules from the
dashboard: no Go code, no rebuild, no agent involvement. Useful when the patterns
you want to catch are specific to your codebase or house style.

Examples of what fits:

- Block any commit that introduces `console.log` outside `/test/`
- Warn on `TODO` comments without an owner or date
- Block PII column names (`ssn`, `dob`, …) in migration files
- Block in-house anti-patterns (`assert(false)`, `panic()`, `eval(`, …)
- Catch a vendor-SDK upgrade that broke calling conventions (one-liner pattern,
  applied gateway-wide)

Add them on the Policy page → **Custom linters** section. Each rule has a name, a
regex pattern, a severity (BLOCK / WARN / INFO), and an optional file-glob to
scope it. Before you save, a **live preview** shows *"this rule would have flagged
N files in your latest push"* so you can tune the regex without trial-and-error.

Custom linters run alongside the built-in frames: same gateway, same audit log,
same time-saved tracking, same whitelist mechanism.

**When NOT to use a custom linter:** if the pattern is genuinely universal (every
project on every stack should catch it), open an issue or PR for a new built-in
frame, that way every operator benefits.

---

## How this differs from your normal linters

If you already run ESLint, Prettier, ruff, gofmt, shellcheck, etc. on your dev
machine and in CI: **keep doing that.** They're for style, syntax, and
language-aware quality. nimblegate's frames and custom linters are the
**enforcement layer** that doesn't depend on dev-machine configuration.

- **Where it runs.** Traditional linters run on the dev machine (pre-commit,
  IDE) and CI. nimblegate runs at the gateway, after every dev's hooks,
  regardless of whether the dev installed the linter at all. A dev who skipped or
  misconfigured their local hooks still gets stopped.
- **What it checks.** Traditional linters are AST-based and language-aware.
  nimblegate uses pattern checks (regex), trading language-aware precision for
  one-line authorability + zero toolchain cost on the gateway.
- **Keep the gateway minimal.** No Node / Python / Go runtimes for linting on the
  gateway; those belong on dev machines and CI runners. The gateway only needs
  the regex engine the built-in frames already use.

| Layer | Tool | What it checks |
|---|---|---|
| **IDE / editor** | Same linters via LSP; AI-agent envs often hook the same | Real-time feedback as code is written |
| **Dev machine + CI** | ESLint / ruff / gofmt / Prettier / shellcheck | Style, syntax, language-aware quality |
| **nimblegate gateway** | Built-in frames | Universal must-block patterns (credentials, force-push, schema drift) |
| **nimblegate gateway** | Custom linters | Project-specific must-block patterns (house style, vendor gotchas) |

The layers don't compete; they shift left (fast feedback) and shift right (final
enforcement). nimblegate catches what slipped past the IDE and pre-commit hooks,
so the rule still gets enforced when local tooling was skipped or bypassed.

---

## Tests: how nimblegate fits with your test suite

**The principle: nimblegate doesn't run your tests; your CI does.** Test
execution belongs where the language toolchain already lives. The gateway is a
credential box; installing test runners there would dilute its identity, slow the
gate, and import flaky-test pain into the push path. So nimblegate plays two
complementary roles around your existing tests, neither of which is "test runner":

**1. Frame-level structural checks at push time**: catches the patterns that
*hide* test failures, no execution required:

- A test newly marked `t.Skip()` / `xfail` / `it.skip` without a defer-ledger
  entry → BLOCK (the "agent silenced the failing test to make CI green" pattern).
- A table-driven test committed with zero entries (passes trivially) → BLOCK.
- A `TestFoo` whose `Foo` symbol was renamed/deleted → WARN.

The push is blocked with an actionable message; the agent reads it via the
Auto-PR comment, fixes it, pushes again, the frame re-runs, and the comment
updates to resolved.

**2. CI-result hub via the webhook rail**: when CI finishes, it POSTs the result
to nimblegate's webhook receiver as a `test-result` event (failing tests,
file:line, exit code, count/skip deltas vs. the previous result for the same PR).
The Auto-PR rail consumes it the same way it consumes its own findings: sticky
comment with failing tests, loop closure when the next push triggers a passing
result.

**Net:** keep your existing CI exactly as-is. nimblegate adds (a) gate-level
catching of the structural patterns that hide test failures, and (b) a
comment-driven fix loop around CI's results. Both additive; neither replaces CI
as the actual test runner.
