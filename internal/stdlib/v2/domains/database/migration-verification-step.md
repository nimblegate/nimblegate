---
name: migration-verification-step
category: database
subcategory: migrations
platform: [cloudflare]
framework: []
severity: BLOCK
tier: 1
tags: [migrations, cloudflare, cf-d1, sqlite, postgres, mysql, command-parse]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/scripts/apply-*-migration*"
    - "**/apply-*-migration*"
runs-after: [database/migration-script-explicit-env]
pattern: write-without-verify
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-05-20T14:37:33Z
---

# command-safety/migration-verification-step

Reject `apply-*-migration*` wrapper scripts that DDL-apply but never query the target afterwards to confirm the change is visible. Silent failures on the apply call (network, auth, quota, wrong env, partial batch) go undetected - the classic failure mode of a migration that "succeeded" against the local D1, prod schema never changed, every API request 500's after deploy.

## What this catches

For every `apply-*-migration*` script:

1. Looks for a DDL apply line - `wrangler d1 execute … --file=…`, `wrangler kv`, `wrangler r2`, `gcloud sql …`, `psql -f …`, `mysql < …`, `mongosh --file`
2. Looks for a verification pattern anywhere in the file: `pragma_table_info`, `EXPLAIN QUERY PLAN`, `DESCRIBE`, `SHOW TABLES`, `SHOW COLUMNS`, `information_schema.*`, psql `\d+`, `SELECT name FROM sqlite_master`, or a `wrangler d1 execute … --command "SELECT …"` follow-up
3. BLOCKs if apply present but no verification

The verification pattern list is broad - false negatives (real verification using unusual syntax) are cheaper to suppress per-file than false positives that pass unverified migrations.

## Fix

After every apply call in your wrapper, query the target env with the same CLI to confirm the change:

```bash
#!/usr/bin/env bash
# scripts/apply-add-country-column-migration.sh
set -euo pipefail
SCOPE="${1:?usage: $0 <local|remote>}"
DB="myapp"

# Apply
wrangler d1 execute "$DB" --file=migrations/add-country-column.sql --remote

# Verify - fails the script if the column doesn't exist on the target
count=$(wrangler d1 execute "$DB" --remote --json \
  --command "SELECT COUNT(*) AS n FROM pragma_table_info('posts') WHERE name = 'country';" \
  | jq '.[].results[].n')
if [[ "$count" -ne 1 ]]; then
  echo "VERIFICATION FAILED: 'country' column missing after apply"
  exit 1
fi
echo "✓ country column visible on $SCOPE"
```

For Postgres:

```bash
# Apply
psql "$DATABASE_URL" -f migrations/add-country-column.sql

# Verify
psql "$DATABASE_URL" -c "\d+ posts" | grep -q country || {
  echo "VERIFICATION FAILED"
  exit 1
}
```

The shape is the same across stacks: apply, then query the same destination, then assert.

## Suppressing intentional cases

For dev-only wrappers that legitimately don't verify (e.g. local-emulator seed scripts), add a file-level disable + justification:

```bash
#!/usr/bin/env bash
# appframes:disable command-safety/migration-verification-step
# (local emulator only; verification handled by the test harness instead)
```

## Composes with

This frame fires AFTER `command-safety/migration-script-explicit-env`. The verification call must itself use an explicit env scope - otherwise it's verifying the wrong environment, which is its own variety of footgun. The `runs-after:` hint in this frame's frontmatter ensures lint output groups them.

## Generalizes to

Any deploy / config-push surface where the apply call can succeed locally and fail remotely:

- DB migrations
- Kubernetes manifest applies (`kubectl apply -f …` succeeds locally even when the target cluster rejects)
- Terraform applies (the plan succeeds, the apply silently no-ops on a state-locked workspace)
- DNS / CDN config push (`cloudflared`, Route53 batch changes)

Add the relevant "query the target afterwards" idiom to your wrapper. The frame doesn't currently understand these stacks; it relies on file-level disable when you go beyond DB migrations.
