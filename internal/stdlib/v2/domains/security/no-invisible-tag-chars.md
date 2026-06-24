---
name: no-invisible-tag-chars
category: security
subcategory: invisible-payload
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
    - "**/*"
pattern: invisible-payload
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 1/1
---

# No invisible Unicode tag characters

Reject files containing characters in the Unicode tag block
(U+E0000–U+E007F). These render as zero pixels in every editor and
terminal yet carry full payload - the canonical channel for hiding
prompt-injection instructions inside otherwise innocuous text.

Typical failure shape: a "harmless" markdown snippet, README, or
comment includes a tag-encoded instruction stream that an LLM agent
reading the file will interpret as a directive. The human reviewer
sees nothing unusual; the agent obeys what's hidden.

## What this catches

Any rune in the range U+E0000 – U+E007F (the Unicode "Tags" block,
including the language tag U+E0001 and the tag-cancel U+E007F). These
codepoints have no legitimate use in modern source code.

## Fix

Remove the offending character. On the command line:

```
LC_ALL=C grep -P '[\x{E0000}-\x{E007F}]' <file>
```

Tag-block characters are essentially never benign in code or
documentation. There is no recommended suppression - if you have a
real need for tag-encoded text (rare research / forensic fixtures),
suppress at the file level with `# appframes:disable security/no-invisible-tag-chars`
and document why in the same file.

## Reference

- Unicode Standard, Chapter 23 - Tags
- "Invisible prompt injection via Unicode tags" (multiple 2024–2025
  AI-safety writeups)
