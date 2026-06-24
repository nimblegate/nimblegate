---
id: yaml-tabs
description: Tab characters in YAML indentation, which the YAML spec disallows and most parsers reject with cryptic errors.
anticipated-siblings: []
---

# Pattern: yaml-tabs

YAML's indentation is defined to use spaces only - tabs are
forbidden in leading whitespace. Most parsers reject the first tab
with `expected <block end>, but found '<tab>'` or a similarly opaque
error. Editors that don't recognize the file as YAML default to tab
indentation; AI agents writing YAML from scratch sometimes use tabs
because Python style "feels close enough."

The defense is structural: lint the leading whitespace of every
.yaml / .yml line and refuse any line whose indent contains a tab.
Tabs inside string scalars (quoted values) are fine and not flagged.
