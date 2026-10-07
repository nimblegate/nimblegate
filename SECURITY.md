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

## Threat model and hardening

What nimblegate defends against, its trust boundaries and the attack scenarios
it was designed around: [security model](https://nimblegate.com/docs/security-model).
How to deploy the gateway so it is a real boundary (separate host, git-shell
only, the privilege-separated relay that keeps the upstream credential from the
`git` user, outbound firewall): [hardening the gateway](https://nimblegate.com/docs/server-hardening)
and [dev machine setup](https://nimblegate.com/docs/dev-machine-setup).

One boundary stated plainly: nimblegate is not a sandbox against an agent that
can write to the gateway's own installation, configuration or host. Run it on a
machine the agent cannot reach.

## Data handling

What the gateway keeps, what it discards, and what leaves the machine.

- **Repositories.** The gateway is a git server: accepted pushes are stored in
  its bare repositories, like on any git host, and forwarded to your upstream.
- **Rejected pushes.** Git receives a push into a quarantine area and discards
  those objects when the pre-receive check rejects it; they are not added to the
  gateway's repository and never reach the upstream.
- **Scan copies.** Each checked push is unpacked into a temporary directory,
  which is deleted when the check finishes.
- **Audit log.** One line per push decision: time, repository, refs, old and new
  commit, accept or reject, the findings (rule id, severity, file and line, a
  short description) and, for overlapping branches, the shared file names. A
  description can quote the matched text for ordinary rules (for example the
  path in an `rm -rf`), but secret values found by the credential, private-key,
  kubeconfig and personal-data rules are redacted: only their location and kind
  are recorded. Files themselves are not copied into the log.
- **Other local files.** Per-repo policy and whitelist, an events log of
  configuration changes, and the dashboard's login database.
- **Outbound connections.** Only to your configured upstream (pushing accepted
  commits; fetching history when a repo is registered or synced; reading pull
  requests and posting Auto-PR comments when notifications are on) and to webhook
  URLs and external linters you configure yourself. No telemetry, update checks,
  licence checks or crash reports. See also [PRIVACY.md](PRIVACY.md).

## Verifying a release

Every release archive, its `checksums.txt` and the container image carry a
signed build-provenance attestation from the release workflow (from the first
release after 0.6.1). To check that a download was built from this repository:

```sh
sha256sum -c checksums.txt --ignore-missing
gh attestation verify nimblegate_<version>_linux_amd64.tar.gz --repo nimblegate/nimblegate
gh attestation verify oci://ghcr.io/nimblegate/nimblegate:<version> --repo nimblegate/nimblegate
```

The container's base images are pinned by digest in the `Dockerfile`, and the
s6-overlay downloads are checked against their published SHA-256.

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
any path construction; upstream URLs are rejected when option-shaped (leading
`-`) or in git's `<helper>::` remote-helper form, the seed fetch restricts
transports via `GIT_ALLOW_PROTOCOL`, and git invocations use the `--` option
terminator; reflected dashboard output is HTML-escaped; and redirect targets are
confined to local paths. CodeQL findings against these paths that persist after
a rescan are barrier-not-recognized false positives and may be dismissed with
that rationale.
