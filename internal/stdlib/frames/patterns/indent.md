---
id: indent
description: Mixed tab and space indentation on the same line, where the two render identically but bind differently to interpreters / parsers / formatters.
anticipated-siblings: []
---

# Pattern: indent

Lines whose leading whitespace contains BOTH tabs and spaces render
identically in editors set to render tabs as 4 / 8 spaces, but bind
differently to the various consumers of the file:

- Python: `TabError: inconsistent use of tabs and spaces` at import.
- `make`: recipe lines must start with a literal tab. A line that
  starts with a space-then-tab is silently dropped from the rule.
- Go: `gofmt` rewrites the indent to its canonical form, producing
  huge unrelated diffs in every change to the file.
- ESLint / Prettier: the formatters disagree about which to keep,
  causing CI flap between runs.

The structural defense is to pick one indent style per project and
refuse the mixed case. The check inspects only the leading whitespace
on each line; tabs or spaces inside code or string literals are not
flagged.
