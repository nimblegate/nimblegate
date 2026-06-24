---
name: no-hardcoded-credentials
category: security
subcategory: credentials
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: secret-in-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T11:45:47Z
---

# No hardcoded credentials

Detect committed secrets - API keys, access tokens, OAuth tokens - by
matching well-known issuer prefixes. The intent: once a credential is
pushed to a remote, rotation is the only remedy. Pre-commit is the
cheapest gate that catches the leak before it spreads.

## What's detected (V0.5)

The frame matches credentials with **distinctive issuer prefixes**.
These are high-confidence patterns with near-zero false-positive rates
in real code. Generic high-entropy detection (random 40-char hex
strings) is explicitly **out of scope for V0.5** - too noisy on UUIDs,
hashes, and compiled assets.

### BLOCK - real leaks; fail the gate, rotate immediately

| Pattern | Example prefix |
|---|---|
| AWS access key ID | `AKIA…` |
| GitHub personal access token (classic) | `ghp_…` |
| GitHub fine-grained PAT | `github_pat_…` |
| GitHub OAuth token | `gho_…` |
| GitHub user-to-server token | `ghu_…` |
| GitHub server-to-server token | `ghs_…` |
| GitHub refresh token | `ghr_…` |
| Stripe secret key (live) | `sk_live_…` |
| Stripe secret key (test) | `sk_test_…` |
| Stripe restricted key | `rk_live_…` / `rk_test_…` |
| Slack token | `xoxb-…` / `xoxp-…` / `xoxa-…` / `xoxr-…` / `xoxs-…` |
| Google API key | `AIza…` |

### INFO - publishable by design; catalogued, doesn't block

| Pattern | Example prefix |
|---|---|
| Stripe publishable key (live) | `pk_live_…` |
| Stripe publishable key (test) | `pk_test_…` |

INFO entries are reported in `nimblegate check` / `lint` output but
do not fail the gate. The intent is inventory: where in the codebase
does my public key appear? Did a test fixture accidentally use
`pk_live_` when it should have used `pk_test_`? When migrating between
Stripe accounts, where do I have to update?

If a commit contains both a BLOCK-severity leak AND an INFO-severity
publishable key, the overall outcome is BLOCK and the reason lists
both findings.

Each pattern is matched with an anchored character-class regex of the
documented length so partial prefixes alone don't trigger.

## Severity

Mixed: `BLOCK` for real credentials, `INFO` for publishable-by-design
keys. The frame's frontmatter declares the maximum severity it can
emit (`BLOCK`); per-pattern severity controls what actually fires for
each match.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to: every staged/scanned file regardless of extension - secrets
  hide in `.env`, `.yaml`, `.json`, `.toml`, `.md`, source code, and CI
  scripts. We rely on the noise-dir exclusion (default + project-configured)
  to skip vendored code (`node_modules/`, `dist/`, `build/`).
- Files larger than 1 MB are skipped (assumed binary / generated).

## Failure message

The reason names the file, line, and pattern - but **never echoes the
matched bytes**. Audit log + terminal would otherwise re-leak the
credential.

```
❌ security/no-hardcoded-credentials (security)
   credentials detected (raw bytes redacted):
   - src/aws-client.js:14 - AWS access key
   - .env.example:3 - GitHub personal access token
   fix: remove the credential, ROTATE IT NOW (assume compromised),
        store via a secret manager / env var, and add a per-line
        `appframes:disable-next-line security/no-hardcoded-credentials`
        only if the value is a known-fake test fixture.
```

## Override

Per-file disable (suppresses every pattern in the file):
```
# appframes:disable security/no-hardcoded-credentials
```

Per-line disable (suppresses the line that follows the marker):
```js
// appframes:disable-next-line security/no-hardcoded-credentials
const FAKE_TEST_KEY = "AKIAIOSFODNN7EXAMPLE";
```

Use per-line for genuine test fixtures. Use per-file ONLY for files
that the project has decided are not subject to this scan
(generated/vendored configs). When in doubt: don't disable; rotate.

## What's NOT detected

Documented so a future "why didn't it catch X?" has an answer:

- **Generic high-entropy strings** - UUIDs, SHA hashes, base64 blobs in
  HTML/JS minified output produce too many false positives. Wait for
  V0.6+ context-aware detection.
- **Stripe publishable keys** (`pk_live_…`, `pk_test_…`) - these are
  intentionally public; not a leak.
- **Private SSH keys / TLS certs** - handled by a separate frame
  (`security/no-private-keys-in-repo`, Tier 1 candidate).
- **AWS secret access keys** (40-char strings) - too ambiguous without
  context. The access-key-ID match is usually paired with the secret
  in the same file and is the higher-leverage detection.
- **Database connection strings with embedded passwords** - pattern is
  too varied (`postgres://`, `mysql://`, `mongodb+srv://…`). Future
  expansion candidate.
