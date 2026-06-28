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

## Test fixtures and static-analysis findings

nimblegate's whole job is to detect insecure patterns, so its test fixtures and
its built-in rule definitions necessarily **contain** those patterns. This is by
design, and it produces predictable false positives in scanners (CodeQL, secret
scanning, etc.). Before treating a scanner finding as a real issue, check whether
it points at one of these:

- **Frame test fixtures** under `**/testdata/`, `internal/stdlib/**/positives/`,
  and `internal/stdlib/**/negatives/` deliberately include insecure examples
  (mixed-content `http://` script tags, control/bidi/zero-width characters, fake
  keys) so the frames that catch them have something to match. These files are
  never shipped or served. `.github/codeql/codeql-config.yml` excludes them from
  CodeQL; under GitHub default-setup scans they may still appear and should be
  dismissed as "used in tests".
- **Detection markers** in the stdlib rules - for example the PEM private-key
  header lines (the `BEGIN ... PRIVATE KEY` markers) in
  `internal/checks/noprivatekeys.go` and the no-private-keys /
  no-hardcoded-credentials frame docs - are literal patterns the frames match
  against. They carry no key body and no live secret.
- **Documentation placeholders** such as AWS's published example access-key id
  (the `AKIA...EXAMPLE` form) are illustrative, not credentials.

Genuine secrets are never committed. If you believe a fixture or marker has
crossed the line into a real exposure, report it via the process above rather
than assuming it is intentional.

For the security-relevant code paths, untrusted inputs are validated before use:
repo names are checked at every HTTP entry and again with `safeRepoName` before
any path construction; upstream URLs are validated and git invocations use the
`--` option terminator; reflected dashboard output is HTML-escaped; and redirect
targets are confined to local paths. CodeQL findings against these paths that
persist after a rescan are barrier-not-recognized false positives and may be
dismissed with that rationale.
