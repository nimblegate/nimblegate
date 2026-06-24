---
name: no-mixed-content-urls
category: security
subcategory: transport
platform: []
framework: []
severity: WARN
tier: 2
tags: [web, html, https, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.html"
    - "**/*.htm"
    - "**/+page.svelte"
    - "**/+layout.svelte"
    - "**/*.astro"
dedup-key: "file:line"
pattern: protocol-downgrade-on-secure-context
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 3/3
  last-run: 2026-05-20T14:37:33Z
---

# security/no-mixed-content-urls

Reject HTML / Svelte / Astro pages that reference `http://` resources via `src=` or `href=` attributes. On HTTPS pages, browsers block these as mixed content - images don't load, scripts don't execute, analytics doesn't fire, and the console errors are too vague for non-devtools users to make sense of.

## What this catches

Every `src="http://..."` and `href="http://..."` value, with these exemptions:

- XML namespaces - `xmlns` URLs are identifiers, not fetch targets (e.g. `http://www.w3.org/2000/svg`)
- Schemas - `schema.org`, `schemas.example.com`
- Localhost / RFC1918 ranges (`127.0.0.1`, `192.168.x.x`, `10.x.x.x`, `172.16-31.x.x`)
- `example.com` / `example.org` / `example.net` - documentation examples
- IETF / purl reference URLs

For everything else: BLOCK.

## Fix

```html
<!-- WRONG - fails on https pages -->
<img src="http://cdn.example.com/logo.png" />
<link rel="stylesheet" href="http://fonts.googleapis.com/css?family=Roboto" />

<!-- RIGHT - explicit https -->
<img src="https://cdn.example.com/logo.png" />
<link rel="stylesheet" href="https://fonts.googleapis.com/css?family=Roboto" />

<!-- ALSO OK - protocol-relative (matches the page's protocol) -->
<img src="//cdn.example.com/logo.png" />
```

If the resource is HTTPS-capable, the swap is mechanical. If it's HTTP-only, you have three options:

1. Host a copy yourself behind your own HTTPS endpoint
2. Proxy through a CF Worker / similar reverse proxy
3. Reach out to the upstream maintainer - most "HTTP-only" claims are out of date

## Suppressing intentional cases

Per-line for explicit demos / docs:

```html
<!-- appframes:disable-next-line security/no-mixed-content-urls -->
<a href="http://internal-only.lan/admin">Admin (LAN only)</a>
```

Per-file for entire pages that legitimately serve HTTP-internal contexts:

```html
<!-- appframes:disable security/no-mixed-content-urls -->
```

## Why BLOCK (tier 2)

The failure mode is silent: the page loads, but resources are missing. Users see broken images, missing fonts, layout shift, and unexplained errors in features that depend on the blocked resources. Catching it at commit time costs nothing; catching it via "the site looks weird in prod" costs hours of debugging.
