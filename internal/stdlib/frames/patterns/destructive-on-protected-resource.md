---
id: destructive-on-protected-resource
description: Irreversible operation directed at a critical or system-scope target without sufficient guardrails.
anticipated-siblings: []
---

# Pattern: destructive-on-protected-resource

`rm -rf` against the wrong directory, `apt purge` against the wrong package set, dropping a production database, force-deleting a branch with unmerged work - all share the same shape: a destructive op + a target that should never have been touched. The op itself is fine in isolation; the danger is the combination.

The structural defense is a known-protected-list pattern: maintain a static catalog of "do not touch" targets, and refuse destructive ops against them. The check is cheap; the saved damage is large.
