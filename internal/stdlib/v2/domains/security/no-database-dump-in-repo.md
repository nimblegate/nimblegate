---
name: no-database-dump-in-repo
category: security
subcategory: content-safety
platform: []
framework: []
severity: WARN
severity-source: frame
tier: 2
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: pii-in-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-07-07T18:42:23Z
---

# No database dump in repo

Detect machine-generated database dumps committed to the repository. A
dump is a snapshot of real data - customer records, hashed credentials,
tokens - and once it reaches history the exposure is permanent. Agents
commit dumps "for testing" more often than people expect.

## What's detected

Detection keys on **tool-written signatures only**, so hand-written seed
and fixture SQL never fires:

| Signal | Kind |
|---|---|
| `-- MySQL dump ` / `-- MariaDB dump ` banner in the header window | mysqldump / mariadb-dump output |
| `-- PostgreSQL database dump` banner in the header window | pg_dump plain output |
| `PGDMP` leading bytes | pg_dump custom-format archive (binary) |
| `SQLite format 3` leading bytes | SQLite database file (binary) |

The binary signatures are checked against raw leading bytes before the
usual binary-file skip - these formats would otherwise never be seen.

## Severity

`WARN`. Intentional dump fixtures exist (tiny sanitized SQLite files in
test suites); the whitelist-with-a-written-reason path is the intended way
to keep them. Escalate to `BLOCK` via severity tuning on repos where no
dump is ever legitimate.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path; text markers are only looked for in the
  first 2 KiB (dump tools write their banner at the top). Files over
  1 MiB are read bounded; noise dirs are excluded uniformly.

## Failure message

Dump content is **never echoed**.

```
⚠ security/no-database-dump-in-repo (security)
   database dump committed (content redacted):
   - backup/prod.sql:1 - mysqldump output
   fix: remove the dump from the push; if it contains real records treat
        it as a data incident; keep hand-written seed SQL instead - it
        does not trip this frame
```

## Override

Per-file disable (for a verified-sanitized SQL fixture):
```
-- appframes:disable security/no-database-dump-in-repo
```

Binary fixtures (SQLite test databases) cannot carry a marker - whitelist
them with a written reason instead.

## What's NOT detected

- **Hand-written seed/fixture SQL** - by design; no tool banner, no hit.
- **CSV/JSON data exports** - no reliable machine signature; real personal
  data inside them is `security/no-pii-in-source`'s job.
- **Dumps inside archives** (`backup.tar.gz`) - archive contents are not
  unpacked.
- **mongodump output** - BSON payloads with generic names; expansion
  candidate once a stable signature is catalogued.
