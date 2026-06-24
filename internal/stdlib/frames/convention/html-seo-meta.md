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
  last-run: 2026-05-20T14:37:33Z
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

Per-route layouts can override with their own `<svelte:head>` block - SvelteKit merges them. (This frame doesn't follow that chain; it warns on the file in isolation. False positives on per-route files that DO override via layout can be suppressed with `appframes:disable`.)

## Suppressing intentional cases

```html
<!-- appframes:disable convention/html-seo-meta -->
<!-- SEO meta lives in +layout.svelte; this route inherits. -->
```

For partial templates, error pages, and other artifacts where SEO meta doesn't apply, the file-level disable is the right tool.
