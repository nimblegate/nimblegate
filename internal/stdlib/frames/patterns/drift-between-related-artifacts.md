---
id: drift-between-related-artifacts
description: Code, docs, config, or tests that should move together don't - one changes and the others go stale.
anticipated-siblings: []
---

# Pattern: drift-between-related-artifacts

A function signature changes; the docstring still references the old shape. A migration adds a column; the example query in the README still shows the old one. A config option is renamed; the .env.example still names the old key. Each is a small drift; cumulatively they make the documentation unreliable, the examples wrong, the onboarding painful.

The structural defense: detect related artifacts (code+docs in the same module, config+example pairs) and warn when one is touched without the other. Not a hard block - sometimes you really do mean to only change one - but a visible nudge so the drift is at least a conscious choice rather than an accident.
