---
name: prefer-static-public
category: app-correctness
subcategory: routing
platform: [cloudflare, cf-pages]
framework: []
severity: INFO
tier: 3
tags: [cloudflare, cf-pages, sveltekit, runtime-env, regex-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.svelte"
    - "**/*.ts"
    - "**/*.js"
dedup-key: "file:line"
pattern: dynamic-when-static-suffices
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 2/2
  last-run: 2026-05-20T14:37:33Z
---

# app-correctness/prefer-static-public

Surface INFO-level findings on any `$env/dynamic/public` import. For build-time-known values, `$env/static/public` is safer: inlined at build, undefined imports return undefined cleanly, no runtime crash on missing env.

Companion to `app-correctness/dynamic-env-declared`:

| Frame | Severity | Catches |
|-------|----------|---------|
| `dynamic-env-declared` | BLOCK | THE BUG - `env.PUBLIC_X` referenced when `PUBLIC_X` isn't declared anywhere outside local `.env` (will crash prod) |
| `prefer-static-public` | INFO | THE PATTERN - dynamic-public used at all (consider switching to static-public) |

Both can fire on the same line. The first prevents an outage; the second is a hint that the dynamic indirection probably isn't earning its keep.

## When dynamic-public IS the right answer

You genuinely need to change the value **without a redeploy**:
- An A/B-test flag toggled from a control plane
- A feature flag that flips on a fixed date
- A per-environment endpoint that the dashboard owns

Anything else - API URL, feature defaults, version strings, third-party site IDs - is build-time-known and should use static-public.

## Fix

```ts
// BEFORE (dynamic - value read at runtime)
import { env } from '$env/dynamic/public';
const url = env.PUBLIC_API_URL;

// AFTER (static - inlined at build time)
import { PUBLIC_API_URL } from '$env/static/public';
const url = PUBLIC_API_URL;
```

Note: `$env/static/public` requires the var to be set **at build time**. CF Pages dashboard env vars are NOT set at build time - they're set at runtime. If you need a dashboard-set value, dynamic-public is the right choice - accept the INFO and run `nimblegate incident new` if it surprises you.

## Suppressing intentional cases

```ts
// appframes:disable-next-line app-correctness/prefer-static-public
import { env } from '$env/dynamic/public';
```

Or whole-file:

```ts
// appframes:disable app-correctness/prefer-static-public
import { env } from '$env/dynamic/public';
// ...
```

## Generalizes to

Any framework with a build-time vs runtime env distinction:

- Next.js `process.env.NEXT_PUBLIC_*` (build-time replaced) vs runtime config files
- Astro `import.meta.env.PUBLIC_*` (build) vs runtime side-channels
- Vite's `import.meta.env` (build) vs `process.env` (runtime in Node adapter)

The frame currently surfaces only the SvelteKit shape; adding the others is straightforward but each needs the corresponding "static vs dynamic" import pattern.
