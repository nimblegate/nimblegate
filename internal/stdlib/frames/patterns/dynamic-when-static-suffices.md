---
id: dynamic-when-static-suffices
description: Runtime mechanism chosen when a build-time mechanism would be safer and equally functional.
anticipated-siblings: []
---

# Pattern: dynamic-when-static-suffices

`$env/dynamic/public` resolves at request time and can return `undefined` if the variable is missing. `$env/static/public` inlines at build time and fails the build if the variable is missing. Both work; only one fails loudly. The runtime version is convenient ("can change without a redeploy") but transfers a guaranteed-at-build invariant to a maybe-at-runtime check.

The structural defense: prefer static / build-time mechanisms when the value is actually static. Use dynamic only when the value genuinely changes between deploys without rebuilding. Build-time failures are catchable in CI; runtime undefineds reach users.
