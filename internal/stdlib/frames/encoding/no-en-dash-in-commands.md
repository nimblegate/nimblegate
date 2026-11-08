---
name: no-en-dash-in-commands
category: encoding
subcategory: dash-substitution
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
    - "**/*.sh"
    - "**/*.bash"
    - "**/*.zsh"
    - "**/Dockerfile"
    - "**/Dockerfile.*"
    - "**/compose.yaml"
    - "**/compose.yml"
    - "**/docker-compose.yaml"
    - "**/docker-compose.yml"
    - "**/Makefile"
    - "**/makefile"
    - "**/*.mk"
    - ".github/workflows/**/*.yml"
    - ".github/workflows/**/*.yaml"
pattern: dash-substitution
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 5/5
  negatives: 6/6
  last-run: 2026-11-08T14:37:12Z
---

# No en/em-dash adjacent to command flags

Block en-dash (U+2013, `–`) and em-dash (U+2014) when they appear
immediately before an alphabetic character. This catches the
classic AI-paste / word-processor-paste failure: `--verbose` rendered
as `–verbose`. The shell sees an unknown flag and errors, but only on
the first invocation; cached layers may have already baked the typo
into a built image.

The "followed by a letter" heuristic skips prose dashes in comments -
`# this – that – the other` doesn't trigger - and ranges such as
`0.2s–0.3s` or `2020–2024`.

## Fix

Replace both with `--`. On the command line (GNU sed):

```
sed -i 's/\xe2\x80\x93/--/g; s/\xe2\x80\x94/--/g' <file>
```

If your script genuinely needs an en/em-dash in a string literal
adjacent to text (multilingual prose), suppress at the line level
with `# appframes:disable-next-line encoding/no-en-dash-in-commands`.

## Scope

- **Markdown** (`.md`, `.markdown`) is scanned for **commands only**: fenced
  blocks (``` and ~~~, tagged or not) and inline code spans. A command a
  reader copies out of a README is the highest-impact instance of this paste
  bug.

## Deliberately not flagged

- **Prose in markdown.** A letter-adjacent en dash is correct there - "the
  Berlin-Paris route", "a Bose-Einstein condensate" - so only code regions
  are read. Indented (4-space) code blocks are not recognised: without a real
  markdown parser they are indistinguishable from wrapped list content, and
  guessing is how false positives start.
- **Inline code in a paragraph about dashes.** A markdown paragraph that
  names the character (en dash, em-dash, U+2013, U+2014) is explaining it,
  so its inline code spans are examples. Fenced blocks in the same document
  are still scanned.
