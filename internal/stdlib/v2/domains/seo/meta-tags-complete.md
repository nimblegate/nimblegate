---
name: html-seo-meta
category: web
subcategory: seo
platform: []
framework: []
severity: WARN
tier: 6
tags: [web, html, seo, social, regex-scan]
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

# convention/html-seo-meta

Surface a WARN when HTML pages are missing the standard SEO + social meta set:

- `<meta name="description">` - search-result snippet
- `<link rel="canonical">` - duplicate-content / pagination signal
- `<meta property="og:title">` - link preview title
- `<meta property="og:description">` - link preview description
- `<meta property="og:image">` - link preview image

These are not technically required, but every one missing degrades how the page appears in search results, Slack/Discord/Teams unfurls, and link cards.

Companion to `convention/html-required-meta`. That frame catches the indisputably-required tags (charset / viewport / title); this one catches the should-have tags.

## What counts as a page

Only whole documents that search engines index are checked:

- an `.html` / `.htm` / `.astro` file with an `<html>` or `<head>` tag
  (fragments get their meta from the page they are loaded into);
- not one that opts out with `<meta name="robots" content="noindex">`;
- not an error page (`404.html`, `500.html`, `50x.html`, `+error.svelte`);
- not SvelteKit's `app.html` shell (`%sveltekit.head%` is filled per route);
- a `.svelte` route only when its own `<svelte:head>` sets at least one
  of these tags - one that sets none leaves SEO to its layout, one that
  sets some must set all.

Files under test-fixture and test-suite folders and under hidden folders
(`.superpowers/`, `.private/`; `.well-known/` excepted) are skipped.

## Site generator templates

Template fragments of a static site generator are skipped: files under
Hugo's `layouts/` (next to `hugo.toml`, `config.toml` or another Hugo config)
and Jekyll's `_layouts/` and `_includes/` (next to `_config.yml`). They are
pieces of a page, not pages; the built output (for example `public/`) is
checked instead. A `layouts/` folder without the generator's config beside it
is checked as usual.

## Fix

In SvelteKit, template the SEO meta in your root `+layout.svelte`:

```svelte
<script lang="ts">
  export let data: { title: string; description: string; image?: string };
</script>

<svelte:head>
  <title>{data.title}</title>
  <meta name="description" content={data.description} />
  <link rel="canonical" href={`https://example.com${$page.url.pathname}`} />
  <meta property="og:title" content={data.title} />
  <meta property="og:description" content={data.description} />
  <meta property="og:image" content={data.image ?? 'https://example.com/default-og.png'} />
</svelte:head>
```

Per-route layouts can override with their own `<svelte:head>` block - SvelteKit merges them. (This frame doesn't follow that chain; it judges each file in isolation. A route that overrides only some of the tags, relying on the layout for the rest, can be suppressed with `appframes:disable`.)

## Suppressing intentional cases

```html
<!-- appframes:disable web/html-seo-meta -->
<!-- SEO meta lives in +layout.svelte; this route inherits. -->
```

For partial templates, error pages, and other artifacts where SEO meta doesn't apply, the file-level disable is the right tool.
