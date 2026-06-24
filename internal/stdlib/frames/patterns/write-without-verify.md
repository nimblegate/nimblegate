---
id: write-without-verify
description: State change applied but never confirmed it took effect or hit the right target.
anticipated-siblings: []
---

# Pattern: write-without-verify

A script applies a DDL migration, returns exit 0, and exits. The migration may have succeeded, may have hit the wrong database, may have applied to a stale schema. The script doesn't know - it just issued the write. Same shape: deploying without smoke-testing the deployed thing, pushing config without reading it back, sending a webhook without confirming delivery.

The structural defense is a confirm-after-write step: after the mutation, query the target to verify the change is visible. The query is cheap; the cost of a "succeeded" status that didn't actually succeed is enormous when the failure surfaces hours or days later.
