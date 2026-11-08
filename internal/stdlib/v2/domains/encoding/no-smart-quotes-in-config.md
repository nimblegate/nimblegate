---
name: no-smart-quotes-in-config
category: encoding
subcategory: smart-quotes
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
    - "**/*.toml"
    - "**/*.yaml"
    - "**/*.yml"
    - "**/*.json"
    - "**/*.env"
    - "**/*.ini"
    - "**/compose.yaml"
    - "**/compose.yml"
    - "**/docker-compose.yaml"
    - "**/docker-compose.yml"
pattern: smart-quotes
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-11-08T14:37:12Z
---

# No smart / curly quotes in config

Reject config files containing typographic ("smart") quotes:
U+2018-U+201F. These render like ASCII `"` and `'` but parsers reject
them - silently in some YAML libraries, loudly in TOML / JSON.

Common cause: pasting from an LLM, word processor, or documentation
page that converted straight quotes to curly ones. Config files are
the high-blast-radius surface because they're parsed at startup;
mis-quoted YAML often presents as "the app won't read this key" without
a clear error.

## What this catches

| Codepoint | Char |
|---|---|
| U+2018 | ' (left single curly) |
| U+2019 | ' (right single curly) |
| U+201A | ‚ (single low-9) |
| U+201B | ‛ (single high-reversed-9) |
| U+201C | " (left double curly) |
| U+201D | " (right double curly) |
| U+201E | „ (double low-9) |
| U+201F | ‟ (double high-reversed-9) |

## Fix

Replace with ASCII `'` or `"`. On the command line:

```
sed -i 's/[\xE2\x80\x98\xE2\x80\x99]/'\''/g; s/[\xE2\x80\x9C\xE2\x80\x9D]/"/g' <file>
```

## Never fires

- **JSON that parses.** In valid JSON every curly quote sits inside a string
  value (a search index, translated copy) where it is content; a curly quote
  used as a delimiter breaks the parse and fires. YAML, TOML, `.env` and
  `.ini` are always scanned: a curly-quoted YAML value parses but keeps the
  curly characters in the value.
