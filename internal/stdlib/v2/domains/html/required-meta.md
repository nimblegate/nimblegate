---
name: html-required-meta
category: web
subcategory: html
platform: []
framework: []
severity: WARN
tier: 6
tags: [web, html, seo, a11y, regex-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.html"
    - "**/*.htm"
    - "**/+page.svelte"
    - "**/+layout.svelte"
    - "**/*.astro"
pattern: missing-standard-protection
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-11-08T14:40:13Z
---

# convention/html-required-meta

Every shipping HTML page (plain `*.html`, SvelteKit `+page.svelte` / `+layout.svelte`, Astro `*.astro`) must declare:

1. **`<meta charset>`** - character encoding. Browsers tolerate the omission but parsers can fall back to legacy decoders.
2. **`<meta name="viewport">`** - mobile rendering. Without this, mobile devices render at desktop width and zoom out.
3. **`<title>`** - required by every browser tab, every share, every search result.

In SvelteKit projects, the title is usually injected via `<svelte:head>` in a layout. This frame treats the presence of `<svelte:head>` as satisfying the title requirement on the assumption the layout (or this component) is filling it in. Charset and viewport belong in `src/app.html`, so they are not required of `.svelte` files, and `app.html` itself needs no `<title>` (each route fills `%sveltekit.head%`).

## What counts as a page

Only whole documents are checked: an `.html` / `.htm` / `.astro` file
with an `<html>` or `<head>` tag, or a `.svelte` route with its own
`<svelte:head>`. Fragments (HTMX partials, report snippets, a route with
no head of its own) get their meta from the page they are loaded into.

Files under test-fixture and test-suite folders (`testdata/`,
`fixtures/`, `test/`, `tests/`, `__tests__/`, `spec/`) and under hidden
folders (`.superpowers/`, `.private/`; `.well-known/` excepted) are not
shipped pages and are skipped.

## Site generator templates

Template fragments of a static site generator are skipped: files under
Hugo's `layouts/` (next to `hugo.toml`, `config.toml` or another Hugo config)
and Jekyll's `_layouts/` and `_includes/` (next to `_config.yml`). They are
pieces of a page, not pages; the built output (for example `public/`) is
checked instead. A `layouts/` folder without the generator's config beside it
is checked as usual.

## Fix

For a SvelteKit project, charset and viewport go in `src/app.html`; the
title goes in `src/routes/+layout.svelte` (or per route):

```svelte
<svelte:head>
  <title>Your Site</title>
</svelte:head>
```

For Astro: in the root layout's `<head>`:

```astro
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{title}</title>
</head>
```

For plain HTML: just add the tags in `<head>`.

## Suppressing intentional cases

For email templates, partial HTML fragments, or non-page artifacts:

```html
<!-- appframes:disable web/html-required-meta -->
<!-- This file is included as a partial; meta lives in the parent template. -->
```
