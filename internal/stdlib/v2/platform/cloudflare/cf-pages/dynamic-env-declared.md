---
name: dynamic-env-declared
category: app-correctness
subcategory: env-config
platform: [cloudflare, cf-pages]
framework: []
severity: WARN
tier: 1
tags: [cloudflare, cf-pages, sveltekit, runtime-env, regex-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.svelte"
    - "**/*.ts"
    - "**/*.js"
dedup-key: "file:line"
pattern: declared-vs-referenced-divergence
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
  last-run: 2026-05-20T14:37:33Z
---

# app-correctness/dynamic-env-declared

Reject SvelteKit components / TypeScript / JavaScript that read `env.PUBLIC_X` via `$env/dynamic/public` when `PUBLIC_X` isn't declared in `wrangler.toml` `[vars]` or `.env.example`. The local `.env` doesn't count - that's the "works on my machine" trap.

Typical failure mode: a component references `PUBLIC_FEATURE_FLAG` via `$env/dynamic/public`; CF Pages doesn't have the var declared; runtime returns undefined; `.env` access throws `TypeError: Cannot read properties of undefined (reading 'env')`. If the component is imported at the top of `+page.svelte`, every initial page load crashes. App dead in prod until rollback.

## What this catches

The combination of:
1. An import statement: `import { env } from '$env/dynamic/public'` (or `"..."`)
2. An access: `env.PUBLIC_FOO` somewhere in the file
3. `PUBLIC_FOO` not present in either `wrangler.toml` `[vars]` OR `.env.example`

`.env.example` accepts two styles:

```bash
# Direct declaration (local fallback exists)
PUBLIC_API_URL=https://api.example.com

# Dashboard-only declaration (vars set in the CF Pages dashboard)
# DASHBOARD-ONLY: PUBLIC_SECRET_TOKEN
```

The second style is the documented convention for "this var exists in production but is set in the dashboard rather than committed." Acknowledging the var in `.env.example` is the gate - local `.env` alone is the trap.

## Fix

Three options:

**1. Add the var to `wrangler.toml` `[vars]`** (best - committed, deployed via wrangler):

```toml
[vars]
PUBLIC_MULTI_COUNTRY_ENABLED = "true"
```

**2. Add a `# DASHBOARD-ONLY:` marker in `.env.example`** (when the value is sensitive / per-environment):

```bash
# .env.example
# DASHBOARD-ONLY: PUBLIC_MULTI_COUNTRY_ENABLED
```

…and set it in the CF Pages dashboard for each environment.

**3. Switch to `$env/static/public`** (best when the value is build-time-known) - see the companion frame `app-correctness/prefer-static-public`.

## Suppressing intentional cases

```ts
// appframes:disable-next-line app-correctness/dynamic-env-declared
const flag = env.PUBLIC_EXPERIMENTAL_ONLY;
```

## Generalizes to

Any framework's runtime-env access pattern:

- Next.js `process.env.NEXT_PUBLIC_*`
- Remix `context.env.*`
- Astro `import.meta.env.PUBLIC_*` (runtime side)
- Workers / Pages Functions `env.X` (typed via Bindings)

The frame currently covers SvelteKit's `$env/dynamic/public` shape. Adding Next.js / Remix / Astro requires extending the import-pattern regex and the env-key-prefix convention.
