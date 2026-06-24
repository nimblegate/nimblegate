# {{title}}

## Incident

What broke. How long the debug took. Cross-references (AGENTS_LEARNING, ticket
links, commit hashes).

## Detection signal

What would have flagged this before damage. Concrete signals - a file pattern,
a log line, a state mismatch - not a vibe.

## Frame proposal

Candidate frame. Be specific:

- **ID** - `<category>/<kebab-name>` (e.g. `command-safety/wrangler-explicit-env`)
- **Severity** - BLOCK / WARN / INFO
- **Tier** - 1 (catastrophic) to 6 (cosmetic)
- **Triggers** - pre-commit / cli / git-wrap / watcher
- **Check** - what the check actually does in 1-3 sentences. Mechanically
  verifiable: a regex against a file, an exec of a known command, a state
  query. No judgment calls.

## Where the check belongs

pre-commit / pre-deploy / inside the migration script / runbook / CI gate.

## Generalizes to

Broader pattern this incident points to. Other tools, frameworks, or
environments where the same shape applies. Helps the eventual frame cover
the class, not the one-off.

## Notes

Anything else. Stack traces, related incidents, why the obvious fix isn't
the right fix.
