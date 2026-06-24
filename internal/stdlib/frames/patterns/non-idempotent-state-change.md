---
id: non-idempotent-state-change
description: Operation that can't be safely re-run - first invocation succeeds, second invocation breaks.
anticipated-siblings: []
---

# Pattern: non-idempotent-state-change

`ALTER TABLE users ADD COLUMN email TEXT` works the first time. The second time it errors because the column already exists. Same shape: `INSERT` without `ON CONFLICT`, `mkdir` without `-p`, sending a non-idempotent webhook on retry. The op is fine in fresh state; replay is the failure mode.

The structural defense: wrap state changes in idempotent envelopes - "create if not exists," conditional execution by checking current state first, or maintain a migration ledger so each change runs exactly once. The cost is a wrapper script or a stamp table; the savings is "this deployment retry actually retries safely."
