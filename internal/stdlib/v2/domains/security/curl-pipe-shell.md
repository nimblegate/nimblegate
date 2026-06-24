---
name: curl-pipe-shell
category: commands
subcategory: trusted-execution
platform: []
framework: []
severity: BLOCK
tier: 1
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.sh"
    - "**/*.bash"
    - "**/*.zsh"
    - "**/*.ksh"
    - "**/*.dash"
    - "**/*.fish"
    - "**/Dockerfile"
    - "**/Dockerfile.*"
pattern: arbitrary-code-from-network
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:37:33Z
---

# No curl | sh

Block committed shell scripts (and Dockerfiles) that pipe a remote
fetch directly into an interpreter. The pattern is convenient and
ubiquitous - `curl install.example.com | sh` - but it's also the
canonical remote-code-execution vector: even trusted publishers get
compromised, and once the install runs there's no audit trail for
what executed.

## Detected shapes

| Shape | Example |
|---|---|
| Direct pipe | `curl https://example.com/install \| sh` |
| Wget direct pipe | `wget -qO- https://example.com/install \| bash` |
| Sudo-elevated pipe | `curl https://example.com/install \| sudo sh` |
| Eval pipe | `curl https://example.com/script \| eval` |
| Process substitution | `bash <(curl https://example.com/install)` |
| Process substitution (wget) | `sh <(wget -qO- https://example.com/install)` |

Detection is regex on the same line. Multi-line commands using `\`
continuation aren't caught in V0.5 (could be added in V0.6+ if it
becomes a real false-negative).

## Scope (deliberately narrow)

This frame scans **shell scripts** (`.sh`, `.bash`, `.zsh`, `.ksh`,
`.dash`, `.fish`) and **Dockerfile** files (any name starting with
`Dockerfile`, e.g. `Dockerfile.alpine`). In those contexts, the pattern
means "execute on every run" - a persistent risk.

It deliberately does **NOT** scan markdown files. READMEs and install
docs commonly document the `curl | sh` pattern as instructions
(telling the reader what command to type). Flagging documentation as
a leak would be a false-positive deluge.

CI workflow files (`.github/workflows/*.yml`, `.gitlab-ci.yml`, etc.)
also use the pattern in `run:` steps and SHOULD be detected, but
that's V0.6+ scope - the YAML extraction needs care to avoid scanning
literal documentation embedded in YAML.

## Severity

`BLOCK` - committing a script that pipes to shell is virtually
always either:

1. A legitimate install/bootstrap step that should be replaced with
   a vetted package, a pinned-checksum download, or a release-tagged
   binary fetch.
2. A test fixture or example that should carry a disable marker.
3. A copy-paste from an install-doc that nobody reviewed.

If (1), do the work to harden the install. If (2), disable per-line.
If (3), the frame just caught your bug - that's the design.

## Failure message

```
❌ command-safety/curl-pipe-shell (command-safety)
   pipe-to-shell patterns detected:
   - scripts/install.sh:3 - curl | sh
   - scripts/bootstrap.sh:12 - bash <(curl ...)
   fix: replace with a checksum-pinned download + verify + exec,
        or a package manager install; add
        `# appframes:disable-next-line command-safety/curl-pipe-shell`
        above the line ONLY for vetted test fixtures.
```

## Override

Per-file:
```
# appframes:disable command-safety/curl-pipe-shell
```

Per-line:
```sh
# appframes:disable-next-line command-safety/curl-pipe-shell
curl -sSL https://known-safe.example.com/installer | sh
```

Use per-line for vetted bootstrap scripts where you've separately
audited the hosting and the script content. Use per-file ONLY for
files the project has decided are not subject to this scan
(e.g. test fixtures of malicious-input shapes).

## What's NOT detected

Documented so a future "why didn't it catch X?" has an answer:

- **Multi-line continuations:** a `curl ... \` line followed by `| sh`
  on the next line isn't matched. Same-line patterns only in V0.5.
- **Indirect chains:** `curl url > /tmp/x && sh /tmp/x` would pass -
  the install runs but not as a single pipe. Same risk class, but
  catching every "fetch then exec" path is the much-harder fully-
  contextual analysis V0.5 explicitly avoids.
- **Markdown documentation:** intentional. See "Scope" above.
- **CI YAML files:** intentional for V0.5. See "Scope" above.
- **Interactive shell invocations:** the frame is file-scanning, not
  command-intercept. If you type `curl url | sh` in your terminal
  it'll run; the frame doesn't see one-off interactive commands. The
  related Tier 1 frame `command-safety/apt-purge-preview` is the
  command-intercept model; this one is the file-scanning model.
