# Privacy

## No telemetry

nimblegate does not phone home. The binary makes zero outbound network calls
on its own. The only network traffic is:

1. The git relay path - the gateway forwards accepted pushes to your
   configured upstream (Gitea, GitHub, GitLab, etc.). That's the entire
   point.
2. Whatever you, the operator, configure (a webhook, an external linter).

There is no usage analytics, no crash reporting, no opt-in / opt-out toggle
because there's nothing to opt out of.

## What it stores locally

- `audit.log` per repo - every push decision with frame findings
- `_events.jsonl` - gateway-wide mutation log (add/archive/severity-change/etc.)
- `_archived.md` - human-readable archive/restore history
- `whitelist.toml` per repo if you've whitelisted any findings

All on disk under your `--policy-root`. Nothing leaves the box.

## Secret redaction

Findings that detect credentials, private keys, or other secrets store the
finding *metadata* (file, line, frame ID, severity) but NOT the literal
secret content. The audit log will tell you "private key detected at
foo.pem:1" but won't include the key bytes.
