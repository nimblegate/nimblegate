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
  last-run: 2026-05-20T11:45:47Z
---

# convention/html-required-meta

Every shipping HTML page (plain `*.html`, SvelteKit `+page.svelte` / `+layout.svelte`, Astro `*.astro`) must declare:

1. **`<meta charset>`** - character encoding. Browsers tolerate the omission but parsers can fall back to legacy decoders.
2. **`<meta name="viewport">`** - mobile rendering. Without this, mobile devices render at desktop width and zoom out.
3. **`<title>`** - required by every browser tab, every share, every search result.

In SvelteKit projects, the title is usually injected via `<svelte:head>` in a layout. This frame treats the presence of `<svelte:head>` as satisfying the title requirement on the assumption the layout (or this component) is filling it in.

## Fix

For a SvelteKit project, put the static meta in `src/routes/+layout.svelte`:

```svelte
<svelte:head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
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
<!-- appframes:disable convention/html-required-meta -->
<!-- This file is included as a partial; meta lives in the parent template. -->
```
