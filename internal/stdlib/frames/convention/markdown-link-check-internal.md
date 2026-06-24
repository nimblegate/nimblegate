---
name: markdown-link-check-internal
category: documentation
subcategory: markdown
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*.md"
canonical-refs:
  - markdown-link-ignore.toml
pattern: broken-reference
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 1/1
  negatives: 3/3
  last-run: 2026-05-24T00:00:00Z
---

# Markdown internal link check

Scans Markdown files for inline `[text](path)` and `![alt](path)` links
that look like project-relative paths and verifies each target exists.
External links (`http://`, `https://`, `mailto:`, `tel:`, `data:`,
`javascript:`, `vbscript:`) are explicitly NOT checked - flakiness +
slowness make external link verification a job for a scheduled CI task,
not pre-commit. Pure-anchor links (`#heading`) are also skipped.

Links inside `inline code` spans and ```fenced code blocks``` are NOT
checked - per CommonMark they aren't links at all, just syntax/payload
examples (e.g. documenting `[text](url)` or an XSS test case). For links
that resolve at runtime rather than on disk (app routes, cross-branch
references), add their prefix to `markdown-link-ignore.toml`.

The intent: catch internal link rot at commit time so docs stay
navigable as the project evolves.

## When it fires

`pre-commit` and `cli` triggers. For each scanned `.md` file:

1. Extract every inline `[text](url)` and `![alt](url)`.
2. Filter out external schemes + pure anchors.
3. Strip `?query` and `#anchor` fragments before resolution.
4. Resolve the path relative to the markdown file's directory.
5. Apply ignore prefixes from
   `.appframes/_canonical/markdown-link-ignore.toml` (optional).
6. If the resolved file doesn't exist → hit.

Hits are reported as `<file>:<line> → <link>` so the user can jump
straight to the broken link.

## Configuration: ignore prefixes

The default behaviour is strict: every internal link must resolve to
a real file in the project's working tree. This breaks down for
projects using the orphan-branch monorepo pattern, where one branch's
docs reference paths that live in OTHER branches' working trees.

To suppress these known cross-branch references, drop a
`markdown-link-ignore.toml` into `.appframes/_canonical/`:

```toml
# .appframes/_canonical/markdown-link-ignore.toml
# Path prefixes that the markdown link checker should treat as
# intentionally absent (typically cross-branch references in
# orphan-branch monorepos). Resolved paths beginning with any of
# these prefixes are skipped.
[ignored-prefixes]
"marketing/"      = "orphan-branch cross-reference"
"infra/"          = "orphan-branch cross-reference"
"studio/"         = "orphan-branch cross-reference"
"images/"         = "orphan-branch cross-reference"
"landing/"        = "orphan-branch cross-reference"
"utm-builder/"    = "orphan-branch cross-reference"
"domain-checker/" = "orphan-branch cross-reference"
```

Keys are path prefixes (resolved relative to project root, slash-form).
Values are free-form reasons for documentation. Matching is by literal
prefix - `marketing/foo/bar.md` matches `marketing/`.

## Failure message

```
⚠️  convention/markdown-link-check-internal (convention)
   INDEX.md:42 → ./missing-doc.md (target not found)
   CLAUDE.md:18 → ../old-rename.md (target not found)
   fix: update the link or add the path's prefix to
   .appframes/_canonical/markdown-link-ignore.toml
```

## Override

Per-file: `<!-- appframes:disable convention/markdown-link-check-internal -->`
anywhere in the markdown file. Suppresses every link in that file.

For systematic suppression across many files, use the ignore-prefix
canonical table above - it's the project-author-controlled escape hatch
and is preferable to littering files with disable markers.

## Limitations (V0.5)

- Inline links only. Markdown reference links
  (`[text][ref]` + `[ref]: path` on another line) are not detected.
- External links are not validated. A `[404 page](https://example.com/404)`
  always passes this frame.
- Image alt-text quality is not checked (that's a separate frame's job
  - see V0.5 Tier 2 `app-correctness/img-alt-required` candidate).
- Cross-repo references (e.g., a link to another local repo at
  `../other-repo/README.md`) cannot be verified without that repo being
  present; add such prefixes to the ignore table.

## Why this design

Two alternatives considered:

1. **Try-fetch external links** - too slow + flaky for pre-commit. Belongs
   in a scheduled job.
2. **Smart orphan-branch detection** (introspect `.git/refs/heads/*` and
   accept any link prefix that matches a branch name) - clever but
   brittle: branch names change, links to specific paths within a branch
   need a finer-grained allow list anyway, and the canonical-table
   approach is more explicit + reviewable.

The chosen design - canonical-table prefix ignore + per-file disable
fallback - gives projects an obvious, code-reviewable escape hatch that
doesn't require runtime introspection.
