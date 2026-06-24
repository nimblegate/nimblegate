---
name: html-markup-valid
category: web
subcategory: markup-validity
platform: []
framework: []
severity: WARN
tier: 6
tags: [web, html, content-scan]
triggers: [pre-commit, cli]
applies-to:
  files:
    - "**/*.html"
    - "**/*.htm"
    - "**/+page.svelte"
    - "**/+layout.svelte"
    - "**/*.astro"
dedup-key: "file:line"
pattern: malformed-document-structure
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-05-20T14:37:33Z
---

# convention/html-markup-valid

HTML markup validation pass - the class of problems Vite, the HTML5 spec, and downstream tooling (RSS readers, AMP validators, social-card scrapers) complain about:

- **Unclosed tags** - `<div>` with no matching `</div>` before EOF
- **Mismatched closing tags** - `<div><span></div></span>` (the `</div>` closes the `<span>` implicitly, then `</span>` closes nothing)
- **Duplicate IDs** - two elements with the same `id=` attribute on the same page

Browsers tolerate most of these via the HTML5 error-recovery algorithm; downstream parsers don't.

## How it works

Uses the `golang.org/x/net/html` tokenizer to walk the file. Void elements (`<br>`, `<img>`, `<input>`, etc.) are recognized as auto-closed. Foreign content (`<svg>`, `<math>`, `<script>`, `<style>`, `<template>`) is skipped for balance checking because those subtrees have different parsing rules.

For Svelte / Astro files: the framework wrapper (script blocks, frontmatter) is stripped before tokenization, so the report is about the user-visible HTML body.

## Common cases this catches

```html
<!-- Unclosed tag -->
<section>
  <p>Content
</section>

<!-- Mismatched close -->
<div>
  <span>Text</div>
</span>

<!-- Duplicate ID -->
<input id="email" />
<label for="email">Email</label>
<input id="email" />   <!-- the second `email` is the bug -->
```

Each surfaces as a separate Hit with the line and a description.

## What it does NOT catch

- **Semantic HTML**. `<button>` vs `<a>` vs `<div onclick>` is a separate frame's job.
- **Invalid attribute combinations** (e.g. `<input type="checkbox" maxlength="10">`).
- **Spec compliance.** This isn't a substitute for [Nu Html Checker](https://validator.w3.org/nu/) on production builds - call that from CI for a stricter pass.

## Suppressing intentional cases

Per-file for templates that deliberately leave tags open (rare but legitimate - e.g. partial fragments composed at runtime):

```html
<!-- appframes:disable convention/html-markup-valid -->
```

Per-line is supported via `appframes:disable-next-line convention/html-markup-valid` but rarely useful - markup issues usually span multiple lines.
