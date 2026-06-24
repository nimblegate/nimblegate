---
id: malformed-document-structure
description: Document (HTML, JSON, YAML, etc.) not following the spec - unclosed tags, duplicate keys, invalid encoding.
anticipated-siblings: []
---

# Pattern: malformed-document-structure

Browsers and parsers tolerate a lot. They auto-close tags, accept duplicate IDs, normalize whitespace silently. The result: the page "works" but the structural problems leak into downstream consumers - screen readers crash on duplicate IDs, SEO crawlers see different content than humans, JSON parsers in other languages choke where one was permissive.

The structural defense: validate against the strict spec at write-time, not at the most-permissive consumer. HTML5 tokenizer, JSON schema validator, YAML strict mode. The cost is reporting issues that don't break the visible page; the savings is consistent behavior across all consumers, including the ones you haven't tested yet.
