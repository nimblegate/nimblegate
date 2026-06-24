# Security Policy

## Reporting a vulnerability

If you find a security issue in nimblegate (especially one that bypasses the
gate, leaks credentials through the audit log, or compromises the agent-proof
premise) please **don't** open a public issue.

Email `security@nimblegate.com` with:

- A description of the issue
- Steps to reproduce
- Affected version
- Your suggested fix if any

I aim to respond within 72 hours and ship a patched release within 14 days
for verified vulnerabilities. Acknowledgement in `SECURITY.md` if you'd like
(optional).

## Supported versions

The latest minor release is supported. Older versions receive security
patches on a best-effort basis.

## Threat model

See `docs/THREAT-MODEL.md` (coming) for the explicit boundaries the project
guards and the ones it deliberately does not (e.g., "nimblegate is not a
sandbox against an adversarial agent with write access to its own install
directory").
