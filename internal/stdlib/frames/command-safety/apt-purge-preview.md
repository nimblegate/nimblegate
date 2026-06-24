---
name: apt-purge-preview
category: commands
subcategory: package-management
platform: []
framework: []
severity: BLOCK
tier: 1
triggers:
  - git-wrap
applies-to:
  commands:
    - apt purge
    - apt-get purge
pattern: destructive-on-protected-resource
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-05-20T14:56:32Z
---

# apt purge - require simulate first

`apt purge X` can cascade through transitive dependencies and remove core
platform packages. Block uninspected purges; require the user to first run
`apt purge --simulate <pkg>` and review the `REMOVING:` block.

## When it fires

On any `apt purge` or `apt-get purge` invocation that is not preceded by
`apt purge --simulate <same args>` within the current shell session (heuristic:
look for a recent matching `--simulate` invocation in shell history; if not
present, BLOCK).

## Override

`nimblegate git --force-yes --reason="reviewed simulate output" apt purge ...`
