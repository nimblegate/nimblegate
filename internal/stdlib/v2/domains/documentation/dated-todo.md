---
name: dated-todo
category: documentation
subcategory: todo-discipline
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.go"
    - "**/*.js"
    - "**/*.ts"
    - "**/*.py"
    - "**/*.rs"
    - "**/*.md"
pattern: undated-promise
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 3/3
  last-run: 2026-11-08T14:33:57Z
---

# Naked TODOs must carry a date, owner, or issue link

Scans staged files for `TODO`, `FIXME`, `HACK`, and `XXX` markers that
don't include at least one of:

- **ISO date with optional condition:** `TODO(2026-06-15)` or
  `TODO(2026-06-15: ship V0.5)`
- **Owner handle:** `TODO(@user)`, `FIXME(acme)`
- **Issue link:** `TODO(#42)`, `FIXME(ACME-123)`, `HACK(JIRA-1234)`

Naked `TODO: rewrite this` markers are rejected. The intent is to make
every TODO addressable - either retire by date, owned by someone, or
linked to a tracking issue. Nameless TODOs accumulate forever; tagged
ones can be found and retired.

## When it fires

On `pre-commit` (scanning staged files) and on `nimblegate check`
(scanning all project files). Files matching `applies-to.files` are
read line-by-line; lines matching the TODO regex are tested against
the date/owner/issue regexes.

## Detection

The marker regex is roughly:

```
\b(TODO|FIXME|HACK|XXX)\b
```

Only a marker that is *used* as one counts, not one that is talked
about:

- In source code the marker must be the first word of a comment
  (`// TODO`, `# FIXME`, ` * HACK`, `<!-- XXX`), outside a string
  literal. A pattern literal, a label string, or "this is a TODO
  checklist" mid-comment is not a marker.
- In Markdown the marker must open a line (after any quote, bullet,
  checkbox, heading or emphasis) or an HTML comment. Fenced code blocks
  and inline code spans are examples and are not scanned; "No TBD/TODO"
  mid-sentence is prose.
- A marker glued to `/`, `|`, `-`, `_`, `.` or a quote (`TODO/FIXME`,
  `~$XXX/month`) is a mention.

A line containing a marker is acceptable when ANY of these match the
same line:

- date: `\b\d{4}-\d{2}-\d{2}\b`
- owner: `@[a-zA-Z0-9_-]+|\(\s*[a-zA-Z][a-zA-Z0-9_-]*\s*\)`
- issue: `#\d+|[A-Z]{2,6}-\d+`

## Failure message

```
⚠️  convention/dated-todo (convention)
   src/checkout.go:142  bare TODO: refactor this once we know more
   Suggested fix: add a date `TODO(2026-06-15)`, owner `TODO(@user)`,
   or issue `TODO(#42)`.
```

## Override

Per-line: `// appframes:disable-next-line documentation/dated-todo`
above the offending line.
Per-file: `// appframes:disable documentation/dated-todo` near the top.

## Examples

Accepted:
```go
// TODO(2026-06-15): switch to streaming parser
// FIXME(@maintainer): not thread-safe - fix before release
// HACK(#142): workaround for upstream bug
// TODO(ACME-321: blocked on auth refactor): retry after that ships
```

Rejected:
```go
// TODO: come back to this
// FIXME this is broken
// XXX old code, delete?
```

## Limitations (V0.5)

- Doesn't validate that the date is in the future or the issue exists.
  ISO date + 4-digit year is the strongest cheap signal of intent.
- Doesn't try to retire expired TODOs - that's a separate frame
  candidate (`convention/expired-todo`) for V0.6+.
- Markdown comments (`<!-- TODO ... -->`) and line-opening markers
  inside `.md` files are scanned; code examples in them are not.
- String-literal detection counts quotes before the comment leader; a
  line with an odd number of quotes in code before `//` (rare) can hide
  a marker.
