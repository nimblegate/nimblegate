---
name: html-placeholder-content
category: web
subcategory: markup-validity
platform: []
framework: []
severity: WARN
tier: 6
tags: [web, html, content, regex-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.html"
    - "**/*.htm"
    - "**/+page.svelte"
    - "**/+layout.svelte"
    - "**/*.astro"
    - "**/*.md"
    - "**/*.mdx"
    - "**/*.mdoc"
dedup-key: "file:line"
pattern: placeholder-shipped-as-real
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-11-08T14:40:13Z
---

# No placeholder text in shipped pages

Surface a WARN when shipping HTML / Svelte / Astro / Markdown content contains placeholder patterns that escape into production embarrassingly often:

| Pattern | Why it's a problem |
|---------|--------------------|
| `lorem ipsum...` | Should have been replaced with real copy; usually a never-finished draft |
| `INSERT TEXT HERE` / `INSERT CONTENT HERE` | Template scaffold that didn't get filled |
| `<<PLACEHOLDER>>` / `{{TEMPLATE_VAR}}` | Template marker that wasn't substituted |
| `http://localhost` / `http://127.0.0.1` | Dev URL that didn't get swapped for prod |
| `https://example.com` | Documentation example URL that didn't get personalized |
| `FIXME` / `XXX` / `HACK` | Markers explicitly tagged as "not done" |
| `TODO: ship` / `TODO: prod` / `TODO: before launch` | Tasks the author flagged as required-before-shipping |

Tier 3 WARN - these aren't catastrophic but they consistently show up in production sites and damage credibility. The frame is opinionated about what counts as a "ship-blocker placeholder"; suppress per-line when you legitimately need one.

## Repository docs

README, CHANGELOG, CONTRIBUTING, SECURITY, CODE_OF_CONDUCT and LICENSE files
are skipped: a site build doesn't serve them, so a local-preview URL in a
README is not shipping content.

Markdown is checked only when it is site content that gets rendered into
pages: Hugo `content/` (next to a Hugo config), Jekyll `_posts/` (next to
`_config.yml`), and `src/content/`, `src/pages/`, `src/routes/` (Astro,
SvelteKit with mdsvex and similar). Plans, specs, notes and other
developer documents elsewhere in the repository are not shipped.

## What is page copy

Only text a visitor reads as the page's own copy is scanned. Skipped:

- code samples - fenced blocks and `inline code` in Markdown; `<pre>`,
  `<code>`, `<kbd>`, `<samp>` in HTML. A documented
  `curl https://example.com/install | sh` is an example, not a leftover;
- `<script>` and `<style>` blocks (a JS fallback URL is not copy);
- form-field hint text in `placeholder="..."` attributes - showing
  `https://www.example.com` there is the attribute's purpose;
- files whose name contains `template` (their `{{SLOTS}}` are filled by
  whatever renders them);
- test-fixture, test-suite and hidden folders (`testdata/`, `tests/`,
  `.superpowers/`, ...).

## Fix

Replace placeholder with real content, OR move the file outside the shipping path:

```
content/posts/      # ships - placeholders fire
content/_drafts/    # ignored by build - add to [scan] exclude-paths
examples/           # documentation, not shipping - suppress with appframes-ignore
```

For documentation that legitimately uses these patterns:

```markdown
<!-- appframes:disable-next-line web/html-placeholder-content -->
Run with: `curl http://localhost:8080/`
```

Or for whole-file suppression (typical for `examples/` directories):

```markdown
<!-- appframes:disable web/html-placeholder-content -->
```

## Suggestion: pair with build steps

The frame catches placeholders at commit time. A complementary pre-deploy check should also grep your built `dist/` / `.svelte-kit/output/` to catch placeholders that survived templating. The frame doesn't do that (it scans source, not build output) - wire it as a separate CI step.
