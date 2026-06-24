---
id: multi-env-cli-silent-default
description: Multi-environment CLI silently uses default env when explicit selection is missing, often hitting the wrong account or cluster.
anticipated-siblings: []
---

# Pattern: multi-env-cli-silent-default

Tools that manage multiple environments (dev / staging / prod, multiple cloud accounts, multiple clusters) almost universally pick a default when the user forgets to specify. The default is whatever was last used, or whatever's in a config file three directories up, or simply "the first one listed." When it matches the user's intent, fine. When it doesn't, the result is a deploy to the wrong place.

The structural defense: require explicit environment selection in committed scripts. The user types `--env staging` or `--project foo` every time, even when redundant. Friction in the writing is far cheaper than a misdirected production change.
