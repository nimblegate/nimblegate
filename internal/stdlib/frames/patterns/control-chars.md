---
id: control-chars
description: Non-printable C0 / C1 control bytes in text files, typically leaked from terminal paste, screen capture, or copy-from-PDF.
anticipated-siblings: []
---

# Pattern: control-chars

C0 controls (U+0000-U+001F, excluding `\t` `\n` `\r`) and C1 controls
(U+0080-U+009F) have no legitimate use in source / config / docs text
files. They render invisibly but break:

- `diff`: shows mysterious whitespace differences.
- `grep`: misses the matched text.
- JSON / YAML encoding: control bytes are encoded as `\u00XX`, often
  producing surprising downstream output.
- Editor cursor movement: skip over or get stuck on the byte.

The most common single source is ESC (U+001B) leaked from ANSI color
codes when terminal output is pasted into source. Less common but
seen: U+00A0 (non-breaking space) confused with regular space; the
DEL byte (U+007F) from old terminal quirks.

Severity is WARN, not BLOCK, because rare binary fixtures legitimately
contain control bytes; common binary file extensions (images / fonts /
archives) are pre-filtered.
