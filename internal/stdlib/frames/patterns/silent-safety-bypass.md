---
id: silent-safety-bypass
description: Flag or mechanism that disables a configured protective layer without explicit acknowledgment or audit trail.
anticipated-siblings: []
---

# Pattern: silent-safety-bypass

Tools accumulate safety mechanisms over time - pre-commit hooks, validation passes, dry-run defaults. They also accumulate escape hatches: `--no-verify`, `--force`, `--skip-tests`, `--validate=false`. Each escape was added for a legitimate edge case; each becomes a silent footgun when used routinely.

The structural defense is to treat any "disable the safety" flag as itself a gated action: record the bypass, surface it in audit, and ideally require a second factor (interactive confirmation, explicit `--force-yes`) before honoring it. Don't ban the escape hatches - make them loud.
