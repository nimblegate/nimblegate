---
id: broken-reference
description: Link or reference whose target is missing - broken markdown link, dangling import, missing asset.
anticipated-siblings: []
---

# Pattern: broken-reference

A markdown link points to a file that was renamed. An import statement names a symbol that was removed. An asset path references an image deleted three commits ago. Each individual break is small; the aggregate erodes trust in the project's internal consistency.

The structural defense: walk the reference graph at lint time and report unresolved targets. For docs, check that internal links resolve. For code, check that imports actually export what's named. The check is cheap and doesn't change runtime behavior - it just catches the slow rot before it accumulates.
