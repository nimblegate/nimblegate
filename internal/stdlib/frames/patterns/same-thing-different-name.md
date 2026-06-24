---
id: same-thing-different-name
description: Identifier referring to one logical entity but spelled inconsistently across artifacts.
anticipated-siblings: []
---

# Pattern: same-thing-different-name

A website-id is `mysite-1` in one branch's config and `mysite_1` in another's. A user-id field is `userId` in TypeScript and `user_id` in the database. A feature flag is `enable_x` in one service and `enableX` in another. Each artifact is internally consistent; the cross-artifact mismatch creates a silent bug class where calls "succeed" but reference different actual things.

The structural defense: declare canonical IDs once, enforce them across artifacts. The check is mechanical when the canonical source exists; the bug it prevents is "I thought I was talking about the same thing" - which is among the hardest to debug because both sides claim to be working.
