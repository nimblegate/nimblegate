---
id: invisible-payload
description: Invisible Unicode characters that carry payload - render as zero pixels yet are interpreted (by parsers, compilers, or LLM agents) as instructions or different identifiers.
anticipated-siblings: []
---

# Pattern: invisible-payload

A class of characters that humans cannot see - bidirectional override
controls (U+202A-U+202E, U+2066-U+2069), tag block (U+E0000-U+E007F),
zero-width spaces / joiners (U+200B-U+200D, U+FEFF) - but that
parsers, compilers, and LLM readers act on. The human reviewer sees
one shape; the machine sees a different one.

Three sub-classes:

1. **Visual reversal** - bidi override reverses the apparent
   control-flow of code (Trojan Source, CVE-2021-42574).
2. **Prompt-injection channel** - tag-block runes smuggle directives
   into otherwise innocuous text that agentic LLM readers will obey.
3. **Identifier forgery** - zero-width runes inside an identifier
   make `varname` and `var‌name` two different symbols pointing at
   different code.

Source files copied from LLM output are the typical delivery vector,
because the offending characters survive clipboard copy without
showing in most editors.
