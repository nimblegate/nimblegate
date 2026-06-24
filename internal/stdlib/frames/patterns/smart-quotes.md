---
id: smart-quotes
description: Typographic ("curly") Unicode quotes that look like ASCII straight quotes but are rejected by config parsers.
anticipated-siblings: []
---

# Pattern: smart-quotes

The Unicode block U+2018-U+201F contains typographic ("curly") quote
glyphs that look effectively identical to ASCII `'` and `"` in most
fonts but are different codepoints. Word processors, LLM chat UIs,
and many documentation renderers auto-convert straight quotes to
curly ones - and the resulting text, when pasted into config files,
breaks parsers in different ways:

- TOML / JSON: hard error, "expected `'` or `"`, found `'`".
- YAML: some libraries silently swallow the curly quote and end up
  with a string that doesn't match the expected key.
- ENV / INI: the curly quote becomes part of the string value,
  causing comparison mismatches at runtime.

Config files are high blast radius because they're parsed at
process startup. A failed parse often presents as "the app won't
read this key" without a clear error pointing at the quote glyph.
