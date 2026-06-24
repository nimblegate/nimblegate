---
id: shared-history-rewrite
description: Modifying immutable shared state (committed history, deployed releases) in a way that destroys collaborators' work.
anticipated-siblings: []
---

# Pattern: shared-history-rewrite

Some changes are irreversible from the perspective of everyone who already saw the old state. Rewriting committed git history, replacing already-published artifacts, modifying a deployed release in place - these all break the invariant that what was shared stays what was shared. Every collaborator who pulled the old state now holds a divergent local view.

The structural fix is the same across surfaces: refuse the destructive operation when the target has been published; require an explicit new-version / new-commit instead of mutating the old one.
