---
id: side-effect-at-wrong-lifecycle-phase
description: Initialization or side effect at module level when it should be deferred to page / component / request scope.
anticipated-siblings: []
---

# Pattern: side-effect-at-wrong-lifecycle-phase

Module-level code runs once per import, eagerly, before any page-level context exists. Calling `env.PUBLIC_X` at module top-level evaluates X at import time - but X may not exist at that phase, and the import order may not be what the developer assumed. The effect leaks across pages that didn't intend to depend on it.

The structural defense: detect side-effecting calls at the wrong scope and require them to be deferred (inside a function, hook, or component setup). The pattern shows up in many frameworks under different names: top-of-page imports in SvelteKit, top-level awaits in Astro, side-effecting class fields in React. Same shape; same fix.
