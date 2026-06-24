---
name: top-of-page-import-safety
category: app-correctness
subcategory: module-loading
platform: [cloudflare, cf-pages]
framework: []
severity: INFO
tier: 3
tags: [cloudflare, cf-pages, sveltekit, runtime-env, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/+page.svelte"
    - "**/+layout.svelte"
runs-after: [app-correctness/dynamic-env-declared]
pattern: side-effect-at-wrong-lifecycle-phase
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
  last-run: 2026-05-20T14:37:33Z
---

# app-correctness/top-of-page-import-safety

Surface an INFO-level finding when a SvelteKit `+page.svelte` or `+layout.svelte` imports a component whose module body uses `$env/dynamic/public`. Components imported at the top of a route-root page execute at module-load time - if their script block touches env before runtime initialization completes (e.g. SSR cold start, network glitch on the dynamic-public fetch), the whole route fails with no upstream error boundary.

Typical failure shape: a component imported at the top of `+page.svelte` accesses a `PUBLIC_FEATURE_FLAG` in its script block; on production where the var isn't declared, every initial page load crashes. App dead until rollback.

## What this catches

For every `+page.svelte` / `+layout.svelte` in the project:
1. Extract its top-level `import ... from '...'` statements
2. Resolve each local import (relative paths and `$lib/...` aliases)
3. Read each resolved file
4. If the imported file contains `import { env } from '$env/dynamic/public'` → surface INFO

The check follows ONE level deep - direct imports only. Transitive cases (A imports B which imports C using dynamic-public) are out of scope to keep false-positive risk low.

## Companion to `dynamic-env-declared`

| Frame | Severity | Catches |
|-------|----------|---------|
| `dynamic-env-declared` | BLOCK | Specific bug - `env.PUBLIC_X` accessed when undeclared anywhere (will crash prod) |
| `top-of-page-import-safety` | INFO | Risk pattern - root-page imports of dynamic-env users (will crash IF env unavailable for any reason) |

The INFO fires alongside the BLOCK when both apply. Together they answer "is this safe now" and "is the architecture defensible later."

## Fix

Three patterns that defuse the module-load risk:

**1. Move env access inside `onMount`** (runs after hydration):

```svelte
<script lang="ts">
  import { env } from '$env/dynamic/public';
  import { onMount } from 'svelte';

  let flag: string | undefined;
  onMount(() => {
    flag = env.PUBLIC_MULTI_COUNTRY_ENABLED;
  });
</script>
```

**2. Guard at top-of-script** (degrades gracefully when env is unavailable):

```svelte
<script lang="ts">
  import { env } from '$env/dynamic/public';

  // Safe even if env is partially loaded or the import undefined.
  const flag = env?.PUBLIC_MULTI_COUNTRY_ENABLED ?? null;
</script>
```

**3. Switch to `$env/static/public`** (best when the value is build-time-known - see `app-correctness/prefer-static-public`):

```svelte
<script lang="ts">
  import { PUBLIC_MULTI_COUNTRY_ENABLED } from '$env/static/public';
</script>
```

## Suppressing intentional cases

For root pages that legitimately need module-time env (rare, but exists):

```svelte
<!-- appframes:disable-next-line app-correctness/top-of-page-import-safety -->
import FeaturePanel from './FeaturePanel.svelte';
```

Or whole-file:

```svelte
<!-- appframes:disable app-correctness/top-of-page-import-safety -->
```

## Generalizes to

Any framework where the root-route module executes synchronously at request handling:

- Next.js `app/layout.tsx` / `app/page.tsx` (Server Components run at request time; module-top env access is also dangerous there)
- Astro `pages/*.astro` frontmatter
- Remix root route loaders / module-level code

The frame currently codifies the SvelteKit shape. Extending to Next.js / Remix / Astro requires matching their root-route filename conventions and module-init patterns.
