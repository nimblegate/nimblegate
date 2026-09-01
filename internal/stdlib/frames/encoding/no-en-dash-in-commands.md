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
  positives: 1/1
  negatives: 1/1
---

# No en/em-dash adjacent to command flags

Block en-dash (U+2013, `–`) and em-dash (U+2014, `-`) when they appear
immediately adjacent to an alphabetic character. This catches the
classic AI-paste / word-processor-paste failure: `--verbose` rendered
as `–verbose`. The shell sees an unknown flag and errors, but only on
the first invocation; cached layers may have already baked the typo
into a built image.

The "adjacent to a letter" heuristic skips prose en-dashes in
comments - `# this - that - the other` doesn't trigger.

## Fix

Replace `–` and `-` with `--`. On the command line:

```
sed -i 's/–/--/g; s/-/--/g' <file>
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
