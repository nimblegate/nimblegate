---
name: sqlite-migration-idempotent-wrapper
category: database
subcategory: migrations
platform: []
framework: []
severity: BLOCK
tier: 1
tags: [migrations, sqlite, postgres, mysql, cf-d1, regex-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/migrations/**/*.sql"
    - "**/migration/**/*.sql"
dedup-key: "file:line"
pattern: non-idempotent-state-change
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:37:33Z
---

# command-safety/sqlite-migration-idempotent-wrapper

Reject `.sql` migration files containing destructive / non-idempotent DDL (`ALTER TABLE ADD/DROP/RENAME COLUMN`, `RENAME TABLE`) unless either:

1. The project ships a wrapper script (`scripts/apply-*-migration*`) that gates the DDL with a state query (e.g. `pragma_table_info`), OR
2. The `.sql` file carries an inline opt-out comment `-- IDEMPOTENT-WRAPPER-NOT-REQUIRED` documenting why an unguarded one-time migration is safe.

## Why

Raw `.sql` migrations with `ALTER TABLE ADD COLUMN` work on first apply, fail on the second run with `duplicate column`, and stop the batch - leaving later `CREATE INDEX` / data backfills skipped. The recovery path is hand-cleanup with partial state, which is exactly where production incidents start.

Typical failure shape: first apply succeeds; second apply errors on `duplicate column: country`, leaving downstream `CREATE INDEX` statements un-executed. Forced workaround is a wrapper script - which then often sprouts its own wrong-env footgun (covered by `command-safety/migration-script-explicit-env`).

## What this catches

For every `.sql` file under a `migrations/` directory:
- `ALTER TABLE … ADD COLUMN …`
- `ALTER TABLE … DROP COLUMN …`
- `ALTER TABLE … RENAME COLUMN …`
- `ALTER TABLE … RENAME TO …`

Suppressed when:
- A wrapper exists anywhere under `scripts/` with a filename matching `apply-*-migration*`, OR
- The `.sql` file contains the literal string `IDEMPOTENT-WRAPPER-NOT-REQUIRED`, OR
- Standard `appframes:disable[-next-line]` markers fire.

## Fix

**Preferred:** add a wrapper that gates the ALTER on a state query.

```bash
#!/usr/bin/env bash
# scripts/apply-add-country-column-migration.sh
set -euo pipefail
SCOPE="${1:?usage: $0 <local|remote>}"
DB="myapp"

# Check before ALTER
if wrangler d1 execute "$DB" --command "SELECT name FROM pragma_table_info('posts') WHERE name = 'country';" --remote --json | jq -e '.[].results | length > 0'; then
  echo "country column already exists, skipping ALTER"
else
  wrangler d1 execute "$DB" --file=migrations/add-country-column.sql --remote
fi
```

The wrapper is also what `command-safety/migration-script-explicit-env` gates - they compose, and together they cover the migration footgun class.

**Escape hatch:** for one-time historical migrations the user has manually audited as safe-to-re-run-once, mark the file:

```sql
-- IDEMPOTENT-WRAPPER-NOT-REQUIRED
-- One-time table create for the 2026-01 launch; will never be re-applied.
CREATE TABLE posts (id TEXT PRIMARY KEY, ...);
```

## Generalizes to

Any DDL surface where the engine doesn't support `IF NOT EXISTS` / `IF EXISTS` on all alter forms:

- SQLite / Cloudflare D1 (SQLite has no `ALTER TABLE ADD COLUMN IF NOT EXISTS`)
- Postgres (has `IF NOT EXISTS` on most ALTERs but not all; check column-rename)
- MySQL (varies by version)
- MongoDB / NoSQL (different shape but same class - index creation, validator updates)
- Even non-DB migrations (Kubernetes manifest applies, Terraform state drift) - the principle is "make it re-runnable, or document that it isn't."

The frame matches based on SQL statement shape; it doesn't currently know about Postgres' specific `IF NOT EXISTS` support. Future refinement could be dialect-aware via a `--dialect` flag in the frame frontmatter.
