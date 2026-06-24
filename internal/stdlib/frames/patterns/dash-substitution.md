---
id: dash-substitution
description: En-dash (U+2013) or em-dash (U+2014) substituted for ASCII `-`, typically from auto-replacement when pasting commands.
anticipated-siblings: []
---

# Pattern: dash-substitution

LLM chat UIs, word processors, and documentation renderers commonly
auto-convert `--` to `–` (en-dash) or `-` (em-dash). Pasted into a
shell script, Dockerfile, Makefile recipe, or CI workflow, the result
is a flag that looks right but the shell parses as an unknown literal.

Failure mode is silent-corrupt: the surrounding command often
succeeds with the broken flag embedded, especially in Docker layers
where cached steps may have already baked the typo into a built
image. The deploy ships and the broken flag only surfaces on the next
clean build.

The defense is heuristic: flag en/em-dashes adjacent to alphabetic
characters (likely-flag shape) in command-bearing files, while
ignoring prose en-dashes surrounded by whitespace (comments,
documentation strings).
