---
name: yaml-no-tabs
category: encoding
subcategory: yaml
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.yaml"
    - "**/*.yml"
pattern: yaml-tabs
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No tabs in YAML indentation

YAML does not permit tab characters in indentation. Most parsers fail
on the first tab with `expected <block end>, but found '<tab>'` or a
similarly cryptic error - and the file often ships through editor
auto-conversion before the engineer notices.

Editors and AI agents that don't know the file is YAML will use tab
indentation by default, especially in mixed YAML/Python repos where
Python style "feels close enough."

## Fix

Replace leading tabs with spaces (typically 2). On the command line:

```
expand -t 2 -i <file> | sponge <file>
```

If a YAML value's *content* legitimately contains a tab (rare), put
the tab inside a quoted scalar - this frame only flags tabs in leading
whitespace.
