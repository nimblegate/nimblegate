---
name: no-out-of-tree-references
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
pattern: broken-reference
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-11-08T19:04:53Z
---

# No out-of-tree references

Warn when the repository points outside itself: a symlink whose target
resolves outside the repository, or a relative import that climbs above the
repository root. On any other checkout the target is missing, so the code is
broken; and a link aimed at a host path is how pushed content tries to make a
scanner or build step read files the repository does not hold.

On a server-side scan, a symlink like that is never followed: it is replaced
by an empty file before any frame reads the tree, and this frame records that
it happened, so the push carries a trace of the attempt.

## What's detected

- **Symlinks leaving the repository.** An absolute target
  (`config/creds -> /home/deploy/.netrc`), a climbing relative target
  (`docs/notes -> ../../../shared/notes`), and a chain that leaves
  (`a -> b`, where `b` points outside). The target is resolved without
  opening it.
- **Imports climbing out of the repository.** A relative
  `import ... from '...'` in a JavaScript, TypeScript, Svelte or Vue file
  whose path, resolved from the importing file, lands above the repository
  root.

## Never fires

- Symlinks that stay inside the repository, including dangling ones (a
  broken link inside the tree is ordinary content).
- Relative imports that stay inside the repository, whether or not the
  imported file exists - a missing file inside the tree is a broken
  reference, not an escape.
- Package imports (`import x from 'react'`) and the `$lib` alias.
- Paths excluded from scanning (`node_modules/`, `.venv/`, `dist/` and
  the other default and configured excludes).

## Severity

`WARN`. A broken relative import must not stop a push. Raise it to `BLOCK`
per repository if outward symlinks should never be accepted there.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Symlinks of any name; import lines in `.js`, `.mjs`, `.cjs`, `.jsx`,
  `.ts`, `.mts`, `.cts`, `.tsx`, `.svelte` and `.vue` files up to 1 MiB.

## Failure message

The reason names the file and the reference. Targets are shortened to 120
characters and control characters are removed before printing.

```
⚠ security/no-out-of-tree-references (security)
   references outside the repository:
   - config/creds - symlink -> /home/deploy/.netrc points outside the repository
   - src/app.js:3 - import "../../../shared/util" points outside the repository
```

## Fix

Point the symlink or import at a path inside the repository, or copy the
file in. For code shared between repositories, publish it as a package or
use a git submodule instead of a path that only exists on one machine.

## Override

Import lines, per line (on the line above):
```
// appframes:disable-next-line security/no-out-of-tree-references
```

Per file:
```
// appframes:disable security/no-out-of-tree-references
```

A symlink has no content to carry a marker; whitelist its path instead.

## What's NOT detected

- `require()`, dynamic `import()` and side-effect `import '...'` lines.
- Paths built at runtime, and references in config files (`tsconfig.json`
  paths, bundler aliases).
- Hard links, which git does not store.
