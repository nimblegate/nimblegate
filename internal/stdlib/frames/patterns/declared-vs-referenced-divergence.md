---
id: declared-vs-referenced-divergence
description: Code references things (env vars, columns, types, modules) that aren't declared in the canonical source of truth.
anticipated-siblings: []
---

# Pattern: declared-vs-referenced-divergence

Code reads `process.env.STRIPE_KEY` - but `.env.example` doesn't list it, `wrangler.toml [vars]` doesn't list it, the deploy config doesn't list it. The reference exists; the declaration doesn't. Same shape across surfaces: a SQL query references columns not in the schema, a TypeScript import references a symbol not exported, an API call references endpoints not in the OpenAPI spec.

The fix is symmetry: every reference has a declaration in the canonical source; every declaration is referenced or pruned. The check is mechanical (parse both sides, compare). The bug it prevents - "works locally, undefined in prod because the env var was added to dev but never to deploy config" - costs hours each time.
