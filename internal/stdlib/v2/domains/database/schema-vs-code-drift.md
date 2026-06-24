---
name: schema-vs-code-drift
category: database
subcategory: schema-drift
platform: []
framework: []
severity: WARN
tier: 1
tags: [migrations, sqlite, postgres, mysql, cf-d1, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/db/**/*.js"
    - "**/db/**/*.ts"
    - "**/database/**/*.js"
    - "**/database/**/*.ts"
    - "**/models/**/*.js"
    - "**/models/**/*.ts"
dedup-key: "file:line"
pattern: declared-vs-referenced-divergence
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
  last-run: 2026-05-20T14:37:33Z
---

# database/schema-vs-code-drift

Reject code that references a column name in an `UPPER_SNAKE_CASE_COLS` array when that column doesn't exist in any committed `schema.sql` or `migrations/*.sql`. Catches the wrong-migration footgun **before** the migration runs - independent of which apply script was or wasn't executed.


## What this catches

Typical failure shape: `db/content.js` declares `POST_LIST_COLS = ['id', 'title', 'country', 'alternates', 'noindex']`, but `schema.sql` and `migrations/*.sql` only declare `id` and `title` - the migrations that add the other columns haven't been applied. Every `/api/content?type=posts` request 500s after deploy.

The check:

1. Walk `**/schema.sql` and `**/migrations/*.sql` - extract every column declared by `CREATE TABLE` body or `ALTER TABLE … ADD COLUMN`
2. Walk `**/db/**/*.{js,ts}`, `**/database/**/*.{js,ts}`, `**/models/**/*.{js,ts}` - find `const FOO_COLS = [...]` / `const FOO_COLUMNS = [...]` / `const FOO_FIELDS = [...]` declarations
3. For every string literal in such an array, verify the name exists in the declared-columns union
4. BLOCK on any mismatch

The directory scope is narrow on purpose - limited to `db/` / `database/` / `models/` so unrelated `UPPER_SNAKE_COLS` constants in business logic don't accidentally trigger the gate.

## Fix

Three options:

**1. Add the missing column** (the case where the migration is missing):

```sql
-- migrations/2026-05-add-country-column.sql
ALTER TABLE posts ADD COLUMN country TEXT;
ALTER TABLE posts ADD COLUMN alternates TEXT;
ALTER TABLE posts ADD COLUMN noindex INTEGER;
```

…and run the wrapper. Both `migration-script-explicit-env` and `migration-verification-step` will then govern the wrapper.

**2. Remove the reference from code** (the case where the column was never going to ship):

```javascript
// db/content.js
const POST_LIST_COLS = ['id', 'title']; // dropped country/alternates/noindex
```

**3. Suppress per-declaration** (computed field / alias / external mapping):

```javascript
// appframes:disable-next-line database/schema-vs-code-drift
const POST_VIRTUAL_COLS = ['id', 'computed_url', 'derived_status'];
```

## Suppression scope

The frame supports three levels:

- File-level: `// appframes:disable database/schema-vs-code-drift` anywhere in the file
- Line-level: marker on the line immediately above the const declaration
- Whitelist entry: standard `.appframes/_canonical/whitelist.toml` (use `frame = "database/schema-vs-code-drift"`)

## What it does NOT do

- **No type checking.** It only verifies "this column name is declared somewhere." Type / nullability / default-value mismatches need a real type system.
- **No dialect awareness.** Postgres / SQLite / MySQL all express ALTER differently; the regex set is broad enough to catch most shapes but not exhaustive.
- **No live DB connection.** Can't tell whether your *deployed* schema has the column - only whether *committed* schema does. Pair this with `migration-verification-step` for the deploy-time confidence.

## Generalizes to

Any code-vs-schema convention where columns are named in source:

- Drizzle ORM / Prisma - they generate types from schema, so they self-protect; this frame doesn't add value there
- Raw SQL builders (knex, kysely, query-as-string) - exactly the population this targets
- Python: `COLUMNS = [...]` in DAO files
- Go: `var PostListCols = []string{...}` (extend the regex to match Go consts)

Adding more languages requires extending `codeColumnArrayRegex` + `codeApplicableFileForSchemaDrift`.
