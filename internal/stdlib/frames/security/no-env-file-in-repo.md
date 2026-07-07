---
name: no-env-file-in-repo
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
  positives: 2/2
  negatives: 3/3
  last-run: 2026-07-07T18:42:07Z
---

# No environment file in repo

Detect live environment files (`.env`, `.env.production`, `.env.staging`,
...) committed to the repository. The values in an environment file are
credentials by definition - database URLs, API keys, signing secrets. Once
pushed to a remote, every value in the file must be treated as leaked and
rotated.

## What's detected

- A file whose basename is exactly `.env`, or starts with `.env.`, **and**
  contains at least one live assignment (`KEY=value` with a non-empty
  value, `export` prefix tolerated).
- Placeholder variants are allowed by their final suffix segment:
  `.env.example`, `.env.sample`, `.env.template`, `.env.dist`, and
  dotenv-vault's encrypted `.env.vault` never fire.
- A `.env` containing only comments or blank lines does not fire - it
  leaks nothing.

## Severity

`BLOCK`. There is no legitimate reason for a live environment file to
enter git history; the committed-on-purpose variants are excluded by name.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path. Binary files (NUL byte in content) and
  files over 1 MiB are skipped; noise dirs (`node_modules/`, `dist/`) are
  excluded uniformly.

## Failure message

The reason names the file and the line of the first live assignment -
values are **never echoed**; the audit log must not re-leak them.

```
✗ security/no-env-file-in-repo (security)
   environment file committed (values redacted):
   - app/.env.production:1 - environment file with live assignments
   fix: remove the file from the push and ROTATE every value in it
        (assume leaked); keep a committed `.env.example` with placeholder
        values instead
```

## Override

Per-file disable (inside the environment file):
```
# appframes:disable security/no-env-file-in-repo
```

Use only for files verified to contain no real values. When in doubt,
rename to `.env.example`.

## What's NOT detected

- **Values leaked into other files** - `security/no-hardcoded-credentials`
  matches credential values by pattern wherever they appear; this frame is
  the file-level complement that fires regardless of value shape.
- **Non-dotenv config carrying secrets** (`settings.py`, `config.json`) -
  covered by the value-matching frames, not by filename.
- **Encrypted secret files** (`.env.vault`, SOPS output) - encrypted by
  design and intended to be committed.
