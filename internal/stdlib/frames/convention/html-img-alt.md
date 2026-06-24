---
name: html-img-alt
category: web
subcategory: a11y
platform: []
framework: []
severity: WARN
tier: 6
tags: [web, html, a11y, seo, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.html"
    - "**/*.htm"
    - "**/+page.svelte"
    - "**/+layout.svelte"
    - "**/*.astro"
dedup-key: "file:line"
pattern: missing-standard-protection
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 2/2
  negatives: 2/2
  last-run: 2026-05-20T11:45:47Z
---

# convention/html-img-alt

Every `<img>` tag must have an `alt` attribute. The empty form `alt=""` is intentional in HTML5 and explicitly satisfies this check - it signals "decorative image, screen readers should skip."

## Why

| Audience | What missing alt does |
|----------|----------------------|
| Screen reader users | Reader announces filename or skips silently - neither is useful |
| Search engines | Image search can't understand the image; surrounding text bears the full burden |
| Slow/broken connections | Browser shows nothing instead of describing the image |
| Compliance (WCAG 2.1, AODA, EAA) | Required Level A criterion |

## Fix

```html
<!-- Informative image -->
<img src="/photo.jpg" alt="Aerial view of the construction site, May 2026" />

<!-- Decorative image (explicit empty alt - intentional) -->
<img src="/divider.svg" alt="" />

<!-- Icon with adjacent text - also decorative -->
<button>
  <img src="/cart.svg" alt="" /> Cart
</button>
```

## Suppressing intentional cases

Per-line:

```html
<!-- appframes:disable-next-line convention/html-img-alt -->
<img src={dynamicAttrs} />
```

File-level when the file is a template fragment that always gets wrapped with alt by the caller:

```html
<!-- appframes:disable convention/html-img-alt -->
```

## Implementation note

Uses the `golang.org/x/net/html` tokenizer to extract every `<img>` start tag (or self-closing form). Detection is robust to attribute order, quoting style, and multi-line tags. Dynamic attribute spreads (`{...$$props}`, `{...attrs}`) are NOT followed - if your component spreads alt from props, mark the file `appframes:disable convention/html-img-alt`.
